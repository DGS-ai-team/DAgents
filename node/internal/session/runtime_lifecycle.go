package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// lifecycleDispatch is retained for legacy test/recovery fixtures that build
// projections directly. Production runtime boundaries use lifecycleDispatchErr
// so a durable lifecycle failure cannot be silently ignored.
func (r *runtime) lifecycleDispatch(command turn.TurnCommand) turn.CoordinatorSnapshot {
	snapshot, _ := r.lifecycleDispatchErr(command)
	return snapshot
}

// lifecycleDispatchErr is the strict adapter used by runtime boundaries and
// external reconciliation. Invalid transitions or durable-write failures are
// returned to the caller instead of allowing execution to continue with a
// stale projection.
func (r *runtime) lifecycleDispatchErr(command turn.TurnCommand) (turn.CoordinatorSnapshot, error) {
	if r == nil || r.turnCoordinator == nil {
		return turn.CoordinatorSnapshot{}, fmt.Errorf("turn coordinator is unavailable")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.lifecycleDispatchLockedErr(command)
}

// lifecycleDispatchLockedErr is the non-locking form used by compound
// lifecycle operations. The caller must hold lifecycleMu for the complete
// operation, not only for each individual command, so an external cancel or
// reconciliation cannot observe a half-applied transition sequence.
func (r *runtime) lifecycleDispatchLockedErr(command turn.TurnCommand) (turn.CoordinatorSnapshot, error) {
	if r == nil || r.turnCoordinator == nil {
		return turn.CoordinatorSnapshot{}, fmt.Errorf("turn coordinator is unavailable")
	}
	if command.CommandID == "" {
		command.CommandID = r.lifecycleNextCommandID()
	}
	var persist func(turn.TurnCommand, turn.CoordinatorSnapshot) error
	if r.store != nil {
		persist = r.lifecyclePersistEvent
	}
	snapshot, err := r.turnCoordinator.DispatchDurable(command, persist)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("turn lifecycle transition rejected",
				"session_id", r.session.ID,
				"command", command.Type,
				"turn_id", command.TurnID,
				"step_id", command.StepID,
				"error", err,
			)
		}
		return snapshot, err
	}
	if command.Type == turn.CommandAssistantReceived && command.HasTools && snapshot.ToolBatchID != "" {
		// AssistantReceived creates the batch atomically in the projection;
		// persist the explicit batch fact as a separate replay/audit marker.
		batchCommand := command
		batchCommand.Type = turn.CommandToolBatchCreated
		batchCommand.ToolBatchID = snapshot.ToolBatchID
		batchCommand.CommandID = command.CommandID + "-batch"
		if err := r.lifecyclePersistEvent(batchCommand, snapshot); err != nil && r.logger != nil {
			r.logger.Warn("persist tool batch audit marker failed", "session_id", r.session.ID, "error", err)
		}
	}
	r.publishTurnState(snapshot, command.Type)
	return snapshot, nil
}

func (r *runtime) lifecycleNextCommandID() string {
	r.mu.Lock()
	r.lifecycleCommandSeq++
	seq := r.lifecycleCommandSeq
	r.mu.Unlock()
	return fmt.Sprintf("%s-lifecycle-%d", r.session.ID, seq)
}

// restoreLifecycleEvents rebuilds the in-memory Turn/Step projection before
// the runtime starts consuming new queue commands. Legacy message snapshots
// are used only to reconcile unproven tool results or bootstrap old pending
// interactions; lifecycle events remain authoritative for execution position.
func (r *runtime) restoreLifecycleEvents() {
	if r == nil || r.store == nil || r.turnCoordinator == nil {
		return
	}
	var events []turn.TurnEventEnvelope
	var afterSeq uint64
	for {
		page, err := r.store.ListTurnEvents(context.Background(), r.session.ID, afterSeq, 1000)
		if err != nil {
			if r.logger != nil {
				r.logger.Warn("load turn lifecycle events failed", "session_id", r.session.ID, "error", err)
			}
			return
		}
		events = append(events, page...)
		if len(page) < 1000 {
			break
		}
		afterSeq = page[len(page)-1].SessionSeq
	}
	if len(events) == 0 {
		return
	}
	r.setLifecycleEventSequence(events[len(events)-1].SessionSeq)
	if err := r.turnCoordinator.Restore(events); err != nil {
		if r.logger != nil {
			r.logger.Warn("restore turn lifecycle projection failed", "session_id", r.session.ID, "error", err)
		}
		return
	}
	r.lifecycleEventsLoaded = true
	snapshot := r.turnCoordinator.Snapshot()
	r.mu.Lock()
	for _, event := range events {
		if sequence := lifecycleCommandSequence(event.CommandID); sequence > r.lifecycleCommandSeq {
			r.lifecycleCommandSeq = sequence
		}
	}
	if !snapshot.HasActiveTurn {
		r.mu.Unlock()
		return
	}
	if snapshot.Budget != (turn.TurnBudget{}) {
		r.turnBudget = snapshot.Budget
	}
	r.mu.Unlock()
	if snapshot.ContextSnapshot != nil && r.orch != nil {
		r.orch.RestoreModelContextSnapshot(r.session.ID, snapshot.ContextSnapshot)
	}
	if snapshot.StepStatus == turn.StepStatusExecutingTools {
		// A process restart cannot prove whether an in-flight side effect
		// committed. Convert every non-terminal execution into an explicit
		// unknown fact and require reconciliation; never replay the tool blindly.
		for _, executionID := range r.turnCoordinator.InFlightToolExecutionIDs() {
			if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
				Type:            turn.CommandToolExecutionFailed,
				SessionID:       r.session.ID,
				TurnID:          snapshot.TurnID,
				StepID:          snapshot.StepID,
				Generation:      snapshot.Generation,
				ToolExecutionID: executionID,
				ExecutionStatus: turn.ToolExecutionStatusUnknown,
				ErrorKind:       "node_restart_unknown",
				Reason:          "tool execution result cannot be proven after node restart",
				At:              time.Now().UTC(),
			}); err != nil {
				if r.logger != nil {
					r.logger.Warn("mark restarted tool execution unknown failed", "session_id", r.session.ID, "execution_id", executionID, "error", err)
				}
				return
			}
		}
		history := r.lifecycleHistorySnapshot()
		if assistant, ok := lastAssistantMessage(history, 0); ok && lifecycleToolResultsPresent(history, assistant.ToolCalls) {
			// A durable tool result in the model history is sufficient evidence
			// that the side effect boundary already committed. Close the missing
			// lifecycle facts from that projection; do not execute the tool again.
			for _, call := range assistant.ToolCalls {
				executionID := r.turnCoordinator.ToolExecutionID(call.ID)
				status, known := r.turnCoordinator.ToolExecutionStatusForCall(call.ID)
				if !known || status != turn.ToolExecutionStatusUnknown {
					continue
				}
				result, resultOK := lifecycleToolResultForCall(history, call.ID)
				if !resultOK {
					continue
				}
				executionStatus := turn.ToolExecutionStatusSucceeded
				errorKind := "recovered_from_history"
				if strings.HasPrefix(strings.TrimSpace(result.Content), "ERROR:") {
					executionStatus = turn.ToolExecutionStatusFailed
					errorKind = "recovered_tool_result_error"
				}
				if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
					Type: turn.CommandToolExecutionReconciled, SessionID: r.session.ID,
					TurnID: snapshot.TurnID, StepID: snapshot.StepID, Generation: snapshot.Generation,
					ToolCallID: call.ID, ToolExecutionID: executionID, ExecutionStatus: executionStatus,
					ResultContent: result.Content, ErrorKind: errorKind, Reason: "recovered_tool_result_history",
					At: time.Now().UTC(),
				}); err != nil {
					if r.logger != nil {
						r.logger.Warn("reconcile recovered tool execution failed", "session_id", r.session.ID, "execution_id", executionID, "error", err)
					}
					return
				}
			}
			if err := r.lifecycleRecordToolFacts(history, 0, assistant.ToolCalls); err != nil {
				if r.logger != nil {
					r.logger.Warn("recover tool facts failed", "session_id", r.session.ID, "error", err)
				}
				return
			}
		}
		snapshot = r.turnCoordinator.Snapshot()
		if snapshot.RecoveryRequired {
			if r.logger != nil {
				r.logger.Warn("turn recovery requires tool execution reconciliation", "session_id", r.session.ID, "turn_id", snapshot.TurnID, "step_id", snapshot.StepID)
			}
			return
		}
		if assistant, ok := lastAssistantMessage(history, 0); ok && lifecycleToolResultsPresent(history, assistant.ToolCalls) {
			if started, err := r.lifecycleBeginContinuationStep(turn.TurnSourceHuman); err != nil {
				if r.logger != nil {
					r.logger.Warn("start recovered continuation failed", "session_id", r.session.ID, "error", err)
				}
				return
			} else if !started {
				return
			}
			if err := r.enqueueToolResult(context.Background(), r.session.ID); err != nil && r.logger != nil {
				r.logger.Warn("schedule recovered tool result failed", "session_id", r.session.ID, "error", err)
			}
		} else if r.logger != nil {
			r.logger.Warn("turn recovery requires external tool execution decision", "session_id", r.session.ID, "turn_id", snapshot.TurnID, "step_id", snapshot.StepID)
		}
	}
}

// restoreLegacyPending upgrades the old RuntimeState.Pending representation
// into the durable Coordinator interaction projection. It is intentionally a
// one-time compatibility bridge: all subsequent reads come from the
// Coordinator snapshot and the legacy runtime field no longer exists.
func (r *runtime) restoreLegacyPending(initial *turn.PendingHITL, legacyStepCounts ...int) {
	if r == nil || r.turnCoordinator == nil {
		return
	}
	snapshot := r.turnCoordinator.Snapshot()
	pending := initial
	if pending == nil && !r.lifecycleEventsLoaded && snapshot.StepStatus == turn.StepStatusWaitingInteraction && len(snapshot.InteractionPayload) == 0 {
		pending = r.lifecycleRecoverPendingFromProjection()
	}
	if pending == nil || len(pending.Items) == 0 {
		return
	}

	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	snapshot = r.turnCoordinator.Snapshot()
	if snapshot.HasActiveTurn {
		if snapshot.StepStatus == turn.StepStatusWaitingInteraction && len(snapshot.InteractionPayload) > 0 {
			return
		}
		if snapshot.StepStatus == turn.StepStatusWaitingInteraction {
			payload, err := json.Marshal(pending)
			if err != nil {
				return
			}
			if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
				Type: turn.CommandInteractionRequested, SessionID: r.session.ID,
				TurnID: snapshot.TurnID, StepID: snapshot.StepID, Generation: snapshot.Generation,
				InteractionID: snapshot.InteractionID, InteractionKind: legacyPendingInteractionKind(pending),
				Payload: payload, At: time.Now().UTC(), Reason: "legacy_pending_projection_recovered",
			}); err != nil && r.logger != nil {
				r.logger.Warn("restore legacy pending payload failed", "session_id", r.session.ID, "error", err)
			}
			return
		}
		if snapshot.StepStatus == turn.StepStatusRequesting {
			if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
				Type: turn.CommandAssistantReceived, SessionID: r.session.ID,
				TurnID: snapshot.TurnID, StepID: snapshot.StepID, Generation: snapshot.Generation,
				HasTools: true, At: time.Now().UTC(), Reason: "legacy_pending_projection_recovered",
			}); err != nil {
				if r.logger != nil {
					r.logger.Warn("restore legacy pending assistant failed", "session_id", r.session.ID, "error", err)
				}
				return
			}
			snapshot = r.turnCoordinator.Snapshot()
		}
		if snapshot.StepStatus == turn.StepStatusAssistantReceived && snapshot.ToolBatchID == "" {
			if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
				Type: turn.CommandToolBatchCreated, SessionID: r.session.ID,
				TurnID: snapshot.TurnID, StepID: snapshot.StepID, Generation: snapshot.Generation,
				At: time.Now().UTC(), Reason: "legacy_pending_projection_recovered",
			}); err != nil {
				if r.logger != nil {
					r.logger.Warn("restore legacy pending tool batch failed", "session_id", r.session.ID, "error", err)
				}
				return
			}
			snapshot = r.turnCoordinator.Snapshot()
		}
		if snapshot.StepStatus != turn.StepStatusExecutingTools && snapshot.StepStatus != turn.StepStatusWaitingInteraction {
			return
		}
		for _, item := range pending.Items {
			if strings.TrimSpace(item.ToolCall.ID) == "" || r.turnCoordinator.HasToolCall(item.ToolCall.ID) {
				continue
			}
			if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
				Type: turn.CommandToolCallRecorded, SessionID: r.session.ID,
				TurnID: snapshot.TurnID, StepID: snapshot.StepID, Generation: snapshot.Generation,
				ToolCallID: item.ToolCall.ID, ToolName: item.ToolCall.Function.Name,
				Arguments: []byte(item.ToolCall.Function.Arguments), At: time.Now().UTC(),
				Reason: "legacy_pending_projection_recovered",
			}); err != nil {
				if r.logger != nil {
					r.logger.Warn("restore legacy pending tool call failed", "session_id", r.session.ID, "tool_call_id", item.ToolCall.ID, "error", err)
				}
				return
			}
			snapshot = r.turnCoordinator.Snapshot()
		}
		payload, err := json.Marshal(pending)
		if err != nil {
			return
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:            turn.CommandInteractionRequested,
			SessionID:       r.session.ID,
			TurnID:          snapshot.TurnID,
			StepID:          snapshot.StepID,
			Generation:      snapshot.Generation,
			InteractionID:   snapshot.InteractionID,
			InteractionKind: legacyPendingInteractionKind(pending),
			Payload:         payload,
			At:              time.Now().UTC(),
			Reason:          "legacy_pending_projection_recovered",
		}); err != nil && r.logger != nil {
			r.logger.Warn("restore legacy pending interaction failed", "session_id", r.session.ID, "error", err)
		}
		return
	}

	now := time.Now().UTC()
	turnID := newContinuationID()
	legacyStepCount := 1
	if len(legacyStepCounts) > 0 && legacyStepCounts[0] > 0 {
		legacyStepCount = legacyStepCounts[0]
	}
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:      turn.CommandStartTurn,
		SessionID: r.session.ID,
		TurnID:    turnID,
		Source:    turn.TurnSourceHuman,
		Budget:    r.turnBudget,
		At:        now,
		Reason:    "legacy_pending_projection_recovered",
	}); err != nil {
		if r.logger != nil {
			r.logger.Warn("start legacy pending turn failed", "session_id", r.session.ID, "error", err)
		}
		return
	}
	snapshot = r.turnCoordinator.Snapshot()
	turnID, generation := snapshot.TurnID, snapshot.Generation
	// A legacy RuntimeState carried only the number of completed model steps.
	// Materialize those steps as explicit migration facts so the new
	// Coordinator preserves the old soft tool-loop boundary without keeping a
	// second in-memory counter. These synthetic steps contain no model/tool
	// facts because the old snapshot cannot prove them.
	for index := 1; index < legacyStepCount; index++ {
		stepID := lifecycleStepID(turnID, index)
		for _, command := range []turn.TurnCommand{
			{Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: turnID, StepID: stepID, Generation: generation, At: now, Reason: "legacy_step_count_migrated"},
			{Type: turn.CommandAssistantReceived, SessionID: r.session.ID, TurnID: turnID, StepID: stepID, Generation: generation, At: now, Reason: "legacy_step_count_migrated"},
			{Type: turn.CommandCompleteStep, SessionID: r.session.ID, TurnID: turnID, StepID: stepID, Generation: generation, At: now, Reason: "legacy_step_count_migrated"},
		} {
			if _, err := r.lifecycleDispatchLockedErr(command); err != nil {
				if r.logger != nil {
					r.logger.Warn("migrate legacy step count failed", "session_id", r.session.ID, "step_index", index, "error", err)
				}
				return
			}
		}
	}
	stepID := lifecycleStepID(turnID, legacyStepCount)
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:       turn.CommandStartStep,
		SessionID:  r.session.ID,
		TurnID:     turnID,
		StepID:     stepID,
		Generation: generation,
		At:         now,
		Reason:     "legacy_pending_projection_recovered",
	}); err != nil {
		if r.logger != nil {
			r.logger.Warn("start legacy pending step failed", "session_id", r.session.ID, "error", err)
		}
		return
	}
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:       turn.CommandAssistantReceived,
		SessionID:  r.session.ID,
		TurnID:     turnID,
		StepID:     stepID,
		Generation: generation,
		HasTools:   true,
		At:         now,
		Reason:     "legacy_pending_projection_recovered",
	}); err != nil {
		if r.logger != nil {
			r.logger.Warn("record legacy pending assistant failed", "session_id", r.session.ID, "error", err)
		}
		return
	}
	for _, item := range pending.Items {
		if strings.TrimSpace(item.ToolCall.ID) == "" {
			continue
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandToolCallRecorded,
			SessionID:  r.session.ID,
			TurnID:     turnID,
			StepID:     stepID,
			Generation: generation,
			ToolCallID: item.ToolCall.ID,
			ToolName:   item.ToolCall.Function.Name,
			Arguments:  []byte(item.ToolCall.Function.Arguments),
			At:         now,
			Reason:     "legacy_pending_projection_recovered",
		}); err != nil {
			if r.logger != nil {
				r.logger.Warn("record legacy pending tool call failed", "session_id", r.session.ID, "tool_call_id", item.ToolCall.ID, "error", err)
			}
			return
		}
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return
	}
	toolExecutionID := ""
	if len(pending.Items) == 1 {
		toolExecutionID = r.turnCoordinator.ToolExecutionID(pending.Items[0].ToolCall.ID)
	}
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:            turn.CommandInteractionRequested,
		SessionID:       r.session.ID,
		TurnID:          turnID,
		StepID:          stepID,
		Generation:      generation,
		InteractionID:   stepID + "-interaction",
		InteractionKind: legacyPendingInteractionKind(pending),
		ToolExecutionID: toolExecutionID,
		Payload:         payload,
		At:              now,
		Reason:          "legacy_pending_projection_recovered",
	}); err != nil && r.logger != nil {
		r.logger.Warn("record legacy pending interaction failed", "session_id", r.session.ID, "error", err)
	}
}

func legacyPendingInteractionKind(pending *turn.PendingHITL) string {
	if pending == nil {
		return "approval"
	}
	for _, item := range pending.Items {
		if item.MemoryConflict != nil {
			return "memory_conflict"
		}
		if tools.IsAskUserInformation(item.ToolCall.Function.Name) {
			return "user_information"
		}
	}
	return "approval"
}

func lifecycleCommandSequence(commandID string) uint64 {
	commandID = strings.TrimSpace(commandID)
	index := strings.LastIndexByte(commandID, '-')
	if index < 0 || index+1 >= len(commandID) {
		return 0
	}
	sequence, err := strconv.ParseUint(commandID[index+1:], 10, 64)
	if err != nil {
		return 0
	}
	return sequence
}

func lifecycleEventType(command turn.CommandType) (turn.EventType, bool) {
	switch command {
	case turn.CommandStartTurn:
		return turn.EventTurnStarted, true
	case turn.CommandStartStep:
		return turn.EventStepStarted, true
	case turn.CommandTurnSnapshotCreated:
		return turn.EventTurnSnapshotCreated, true
	case turn.CommandModelRequestStarted:
		return turn.EventModelRequestStarted, true
	case turn.CommandModelResponseCompleted:
		return turn.EventModelRequestCompleted, true
	case turn.CommandModelRequestFailed:
		return turn.EventModelRequestFailed, true
	case turn.CommandModelRequestRetrying:
		return turn.EventModelRequestRetrying, true
	case turn.CommandModelUsageRecorded:
		return turn.EventModelUsageRecorded, true
	case turn.CommandAssistantReceived:
		return turn.EventAssistantMessageRecorded, true
	case turn.CommandToolBatchCreated:
		return turn.EventToolBatchCreated, true
	case turn.CommandToolBatchSettled:
		return turn.EventToolBatchSettled, true
	case turn.CommandToolCallRecorded:
		return turn.EventToolCallRecorded, true
	case turn.CommandToolExecutionStarted:
		return turn.EventToolExecutionStarted, true
	case turn.CommandToolExecutionRetrying:
		return turn.EventToolExecutionRetrying, true
	case turn.CommandToolExecutionCompleted:
		return turn.EventToolExecutionCompleted, true
	case turn.CommandToolExecutionFailed:
		return turn.EventToolExecutionFailed, true
	case turn.CommandToolExecutionReconciled:
		return turn.EventToolExecutionReconciled, true
	case turn.CommandToolResultRecorded:
		return turn.EventToolResultRecorded, true
	case turn.CommandInteractionRequested:
		return turn.EventInteractionRequested, true
	case turn.CommandInteractionResolved:
		return turn.EventInteractionResolved, true
	case turn.CommandCompleteStep:
		return turn.EventStepCompleted, true
	case turn.CommandFailStep:
		return turn.EventStepFailed, true
	case turn.CommandInterruptStep:
		return turn.EventStepInterrupted, true
	case turn.CommandCancelStep:
		return turn.EventStepCancelled, true
	case turn.CommandCompleteTurn:
		return turn.EventTurnCompleted, true
	case turn.CommandFailTurn:
		return turn.EventTurnFailed, true
	case turn.CommandInterruptTurn:
		return turn.EventTurnInterrupted, true
	case turn.CommandCancelTurn:
		return turn.EventTurnCancelled, true
	case turn.CommandBudgetExhausted:
		return turn.EventTurnBudgetExhausted, true
	case turn.CommandContextCompacted:
		return turn.EventContextCompacted, true
	case turn.CommandExternalFactRecorded:
		return turn.EventExternalFactRecorded, true
	default:
		return "", false
	}
}

func (r *runtime) lifecyclePersistEvent(command turn.TurnCommand, snapshot turn.CoordinatorSnapshot) error {
	if r == nil || r.store == nil {
		return nil
	}
	eventType, ok := lifecycleEventType(command.Type)
	if !ok || snapshot.TurnID == "" {
		return nil
	}
	payload, err := json.Marshal(map[string]any{
		"command_type":           command.Type,
		"generation":             command.Generation,
		"reason":                 command.Reason,
		"has_tools":              command.HasTools,
		"final_summary":          command.FinalSummary,
		"tool_batch_id":          command.ToolBatchID,
		"external_fact_id":       command.ExternalFactID,
		"external_fact_kind":     command.ExternalFactKind,
		"tool_name":              command.ToolName,
		"interaction_id":         command.InteractionID,
		"interaction_kind":       command.InteractionKind,
		"interaction_revision":   command.InteractionRevision,
		"request_digest":         command.RequestDigest,
		"assistant_message_id":   command.AssistantMessageID,
		"arguments_json":         lifecycleArgumentsJSON(command.Arguments),
		"runtime_revision":       command.RuntimeRevision,
		"runtime_digest":         command.RuntimeDigest,
		"prompt_digest":          command.PromptDigest,
		"tool_digest":            command.ToolDigest,
		"budget":                 command.Budget,
		"context_snapshot":       command.ContextSnapshot,
		"interaction_payload":    command.Payload,
		"result_content":         command.ResultContent,
		"context_before_digest":  command.ContextBeforeDigest,
		"context_after_digest":   command.ContextAfterDigest,
		"compacted_message_from": command.CompactedMessageFrom,
		"compacted_message_to":   command.CompactedMessageTo,
		"error_kind":             command.ErrorKind,
		"execution_status":       command.ExecutionStatus,
		"usage":                  command.Usage,
		"turn_status":            snapshot.TurnStatus,
		"turn_end_reason":        snapshot.TurnEndReason,
		"step_status":            snapshot.StepStatus,
		"step_end_reason":        snapshot.StepEndReason,
		"step_index":             snapshot.StepIndex,
		"context_epoch":          snapshot.ContextEpoch,
		"recovery_required":      snapshot.RecoveryRequired,
	})
	if err != nil {
		return err
	}
	event := turn.NewTurnEventEnvelope(r.session.ID, eventType, command.At)
	event.AgentID = r.session.AgentID
	event.TurnID = snapshot.TurnID
	event.StepID = snapshot.StepID
	event.ToolBatchID = snapshot.ToolBatchID
	if event.ToolBatchID == "" {
		event.ToolBatchID = command.ToolBatchID
	}
	event.ToolCallID = command.ToolCallID
	event.ToolExecutionID = command.ToolExecutionID
	event.InteractionID = command.InteractionID
	if event.InteractionID == "" {
		event.InteractionID = snapshot.InteractionID
	}
	event.Source = string(command.Source)
	event.CommandID = command.CommandID
	event.Payload = payload
	event.PayloadRef = command.PayloadRef
	stored, err := r.store.AppendTurnEvent(context.Background(), event)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("persist turn lifecycle event failed", "session_id", r.session.ID, "event_type", eventType, "command_id", command.CommandID, "error", err)
		}
		return err
	}
	r.setLifecycleEventSequence(stored.SessionSeq)
	return nil
}

// recordSideEffectFact is the lifecycle boundary for results that arrive from
// outside the current model/tool request. The history bridge may still write
// a model-readable callback message, but that message is never registered as
// a ToolCall. This fact is the durable evidence that the external result was
// accepted exactly once by the active Turn.
func (r *runtime) recordSideEffectFact(entry readySideEffect) {
	if r == nil || r.turnCoordinator == nil {
		return
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.StepID == "" {
		if r.logger != nil {
			r.logger.Debug("skip external side-effect fact without active step", "session_id", r.session.ID, "seq", entry.seq)
		}
		return
	}
	factID := fmt.Sprintf("%s:%d", entry.kind, entry.seq)
	payload, err := json.Marshal(map[string]any{
		"seq":             entry.seq,
		"kind":            entry.kind,
		"job_id":          entry.async.JobID,
		"tool_call_id":    entry.async.ToolCallID,
		"tool_name":       entry.async.ToolName,
		"status":          entry.async.Status,
		"result_text":     entry.async.ResultText,
		"error_text":      entry.async.ErrorText,
		"message_content": entry.messageContent,
		"user_name":       entry.userName,
		"trigger_id":      entry.triggerID,
	})
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("marshal external side-effect fact failed", "session_id", r.session.ID, "seq", entry.seq, "error", err)
		}
		return
	}
	if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
		Type:             turn.CommandExternalFactRecorded,
		SessionID:        r.session.ID,
		TurnID:           state.TurnID,
		StepID:           state.StepID,
		Generation:       state.Generation,
		ExternalFactID:   factID,
		ExternalFactKind: string(entry.kind),
		ToolCallID:       entry.async.ToolCallID,
		ToolName:         entry.async.ToolName,
		ResultContent:    entry.built.ForClientContent,
		Payload:          payload,
		At:               time.Now().UTC(),
		Reason:           "side_effect_applied",
	}); err != nil && r.logger != nil {
		r.logger.Warn("record external side-effect fact failed", "session_id", r.session.ID, "seq", entry.seq, "error", err)
	}
}

func lifecycleArgumentsJSON(arguments json.RawMessage) string {
	if len(arguments) == 0 || len(arguments) > 64*1024 || !json.Valid(arguments) {
		return ""
	}
	return string(arguments)
}

func (r *runtime) lifecycleIdentity() (string, uint64) {
	if r == nil || r.turnCoordinator == nil {
		return "", 0
	}
	snapshot := r.turnCoordinator.Snapshot()
	return snapshot.TurnID, snapshot.Generation
}

func (r *runtime) lifecycleEnsureIdentity() (string, uint64) {
	if r == nil || r.turnCoordinator == nil {
		return "", 0
	}
	snapshot := r.turnCoordinator.Snapshot()
	if snapshot.HasActiveTurn && snapshot.TurnID != "" {
		return snapshot.TurnID, snapshot.Generation
	}
	// Generation is allocated atomically by the Coordinator when a StartTurn
	// command omits it. The caller only needs a stable ID before dispatch.
	return newContinuationID(), 0
}

func (r *runtime) lifecycleHistoryLength() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}

func (r *runtime) lifecycleHistorySnapshot() []llm.Message {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]llm.Message(nil), r.messages...)
}

// pendingSnapshot is the compatibility wire projection of the Coordinator's
// durable Interaction payload. The runtime no longer owns a second pending
// HITL lifecycle state. Legacy history reconstruction is kept in
// lifecycleRecoverPendingFromProjection and is only used during migration.
func (r *runtime) pendingSnapshot() *turn.PendingHITL {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	state := r.turnCoordinator.Snapshot()
	return pendingFromLifecycleSnapshot(state, nil)
}

func pendingFromLifecycleSnapshot(state turn.CoordinatorSnapshot, history []llm.Message) *turn.PendingHITL {
	if state.StepStatus != turn.StepStatusWaitingInteraction {
		return nil
	}
	if len(state.InteractionPayload) > 0 {
		var pending turn.PendingHITL
		if err := json.Unmarshal(state.InteractionPayload, &pending); err == nil && len(pending.Items) > 0 {
			return &pending
		}
	}
	// History reconstruction is intentionally limited to the legacy migration
	// caller. Normal runtime and cold projections pass nil here so an arbitrary
	// assistant callback can never recreate an active interaction.
	if len(history) == 0 {
		return nil
	}
	assistant, ok := lastAssistantMessage(history, 0)
	if !ok || len(assistant.ToolCalls) == 0 {
		return nil
	}
	pending := &turn.PendingHITL{Items: make([]turn.PendingHITLItem, 0, len(assistant.ToolCalls))}
	for _, toolCall := range assistant.ToolCalls {
		if strings.TrimSpace(toolCall.ID) != "" && !isInternalSideEffectCallback(toolCall) {
			pending.Items = append(pending.Items, turn.PendingHITLItem{ToolCall: toolCall})
		}
	}
	if len(pending.Items) == 0 {
		return nil
	}
	return pending
}

func (r *runtime) lifecycleRecoverPendingFromProjection() *turn.PendingHITL {
	if pending := r.pendingSnapshot(); pending != nil {
		return pending
	}
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	state := r.turnCoordinator.Snapshot()
	history := r.lifecycleHistorySnapshot()
	return pendingFromLifecycleSnapshot(state, history)
}

func lifecycleToolResultsPresent(history []llm.Message, calls []llm.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	results := make(map[string]struct{}, len(calls))
	for _, message := range history {
		if message.Role == "tool" && strings.TrimSpace(message.ToolCallID) != "" {
			results[message.ToolCallID] = struct{}{}
		}
	}
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			return false
		}
		if _, ok := results[call.ID]; !ok {
			return false
		}
	}
	return true
}

func lifecycleToolResultForCall(history []llm.Message, toolCallID string) (llm.Message, bool) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return llm.Message{}, false
	}
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "tool" && strings.TrimSpace(history[i].ToolCallID) == toolCallID {
			return history[i], true
		}
	}
	return llm.Message{}, false
}

func lifecycleStepID(turnID string, index int) string {
	return fmt.Sprintf("%s-step-%d", turnID, index)
}

// lifecycleBeginHumanTurn closes the old logical Turn when a new human input
// interrupts it, then creates a fresh Turn and its first Step. Queue fencing
// is derived from the Coordinator projection.
func (r *runtime) lifecycleBeginHumanTurn() error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.lifecycleBeginHumanTurnLocked()
}

func (r *runtime) lifecycleBeginHumanTurnLocked() error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	old := r.turnCoordinator.Snapshot()
	if old.HasActiveTurn {
		if old.StepID != "" && !old.StepStatus.Terminal() {
			if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
				Type:       turn.CommandInterruptStep,
				SessionID:  r.session.ID,
				TurnID:     old.TurnID,
				StepID:     old.StepID,
				Generation: old.Generation,
				At:         time.Now().UTC(),
				Reason:     "interrupted_by_new_human_input",
			}); err != nil {
				return fmt.Errorf("interrupt previous step: %w", err)
			}
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandInterruptTurn,
			SessionID:  r.session.ID,
			TurnID:     old.TurnID,
			Generation: old.Generation,
			At:         time.Now().UTC(),
			Reason:     "interrupted_by_new_human_input",
		}); err != nil {
			return fmt.Errorf("interrupt previous turn: %w", err)
		}
	}

	turnID := newContinuationID()

	now := time.Now().UTC()
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:      turn.CommandStartTurn,
		SessionID: r.session.ID,
		TurnID:    turnID,
		Source:    turn.TurnSourceHuman,
		Budget:    r.turnBudget,
		At:        now,
	}); err != nil {
		return fmt.Errorf("start turn lifecycle: %w", err)
	}
	state := r.turnCoordinator.Snapshot()
	turnID, generation := state.TurnID, state.Generation
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:       turn.CommandStartStep,
		SessionID:  r.session.ID,
		TurnID:     turnID,
		StepID:     lifecycleStepID(turnID, 1),
		Generation: generation,
		At:         now,
	}); err != nil {
		return fmt.Errorf("start step lifecycle: %w", err)
	}
	return nil
}

// lifecycleBeginContinuationStep starts the next model Step for tool-result
// and passive side-effect continuations. A missing coordinator state is
// treated as a recovered legacy Turn until durable Turn metadata is added.
func (r *runtime) lifecycleBeginContinuationStep(source turn.TurnSource) (bool, error) {
	if r == nil || r.turnCoordinator == nil {
		return false, fmt.Errorf("turn coordinator is unavailable")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.lifecycleBeginContinuationStepLocked(source)
}

func (r *runtime) lifecycleBeginContinuationStepLocked(source turn.TurnSource) (bool, error) {
	if r == nil || r.turnCoordinator == nil {
		return false, fmt.Errorf("turn coordinator is unavailable")
	}
	identity, generation := r.lifecycleIdentity()
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn {
		identity, generation = r.lifecycleEnsureIdentity()
		now := time.Now().UTC()
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandStartTurn,
			SessionID:  r.session.ID,
			TurnID:     identity,
			Generation: generation,
			Source:     source,
			Budget:     r.turnBudget,
			At:         now,
			Reason:     "recovered_legacy_runtime_turn",
		}); err != nil {
			return false, fmt.Errorf("recover turn lifecycle: %w", err)
		}
		state = r.turnCoordinator.Snapshot()
		identity = state.TurnID
		generation = state.Generation
	}
	if identity == "" {
		identity = state.TurnID
	}
	if generation == 0 {
		generation = state.Generation
	}
	if identity == "" || !state.HasActiveTurn {
		return false, nil
	}
	if state.RecoveryRequired {
		if r.logger != nil {
			r.logger.Warn("continuation blocked by unresolved tool execution", "session_id", r.session.ID, "turn_id", state.TurnID, "step_id", state.StepID)
		}
		return false, nil
	}
	if state.StepStatus == turn.StepStatusExecutingTools {
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandToolBatchSettled,
			SessionID:  r.session.ID,
			TurnID:     identity,
			StepID:     state.StepID,
			Generation: generation,
			At:         time.Now().UTC(),
		}); err != nil {
			return false, fmt.Errorf("settle tool batch lifecycle: %w", err)
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandCompleteStep,
			SessionID:  r.session.ID,
			TurnID:     identity,
			StepID:     state.StepID,
			Generation: generation,
			At:         time.Now().UTC(),
			Reason:     "tool_batch_settled",
		}); err != nil {
			return false, fmt.Errorf("complete tool step lifecycle: %w", err)
		}
		state = r.turnCoordinator.Snapshot()
	}
	if state.StepStatus != "" && !state.StepStatus.Terminal() {
		return true, nil
	}
	decision := r.turnCoordinator.BudgetDecisionFor(turn.CommandStartStep)
	if !decision.Allowed {
		if decision.Reason == "max_steps" || decision.Reason == "max_tool_calls" {
			summaryCommand := turn.TurnCommand{
				Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: identity,
				StepID: lifecycleStepID(identity, state.StepIndex+1), Generation: generation,
				FinalSummary: true, At: time.Now().UTC(), Reason: "reserved_final_summary",
			}
			if summaryDecision := r.turnCoordinator.BudgetDecisionForCommand(summaryCommand); summaryDecision.Allowed {
				if _, err := r.lifecycleDispatchLockedErr(summaryCommand); err != nil {
					return false, fmt.Errorf("start summary step lifecycle: %w", err)
				}
				if r.orch != nil {
					r.orch.SetNextStepFinalSummary(r.session.ID)
				}
				return true, nil
			}
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandBudgetExhausted,
			SessionID:  r.session.ID,
			TurnID:     identity,
			Generation: generation,
			At:         time.Now().UTC(),
			Reason:     decision.Reason,
		}); err != nil {
			return false, fmt.Errorf("record budget exhaustion lifecycle: %w", err)
		}
		return false, nil
	}
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:       turn.CommandStartStep,
		SessionID:  r.session.ID,
		TurnID:     identity,
		StepID:     lifecycleStepID(identity, state.StepIndex+1),
		Generation: generation,
		At:         time.Now().UTC(),
	}); err != nil {
		return false, fmt.Errorf("start continuation step lifecycle: %w", err)
	}
	return true, nil
}

// lifecyclePrepareResume reconstructs the minimum lifecycle state needed for
// a legacy persisted PendingHITL, then moves the active Step back into tool
// execution before the existing resume implementation writes tool results.
func (r *runtime) lifecyclePrepareResume(resumeValue map[string]any) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	state := r.turnCoordinator.Snapshot()
	resolution, _ := json.Marshal(resumeValue)
	if !state.HasActiveTurn {
		identity, generation := r.lifecycleEnsureIdentity()
		now := time.Now().UTC()
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
			Type:       turn.CommandStartTurn,
			SessionID:  r.session.ID,
			TurnID:     identity,
			Generation: generation,
			Source:     turn.TurnSourceHuman,
			Budget:     r.turnBudget,
			At:         now,
			Reason:     "recovered_pending_interaction",
		}); err != nil {
			return fmt.Errorf("recover pending turn lifecycle: %w", err)
		}
		state = r.turnCoordinator.Snapshot()
		identity, generation = state.TurnID, state.Generation
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
			Type:       turn.CommandStartStep,
			SessionID:  r.session.ID,
			TurnID:     identity,
			StepID:     lifecycleStepID(identity, 1),
			Generation: generation,
			At:         now,
		}); err != nil {
			return fmt.Errorf("recover pending step lifecycle: %w", err)
		}
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
			Type:       turn.CommandAssistantReceived,
			SessionID:  r.session.ID,
			TurnID:     identity,
			StepID:     lifecycleStepID(identity, 1),
			Generation: generation,
			HasTools:   true,
			At:         now,
		}); err != nil {
			return fmt.Errorf("recover pending assistant lifecycle: %w", err)
		}
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
			Type:       turn.CommandInteractionRequested,
			SessionID:  r.session.ID,
			TurnID:     identity,
			StepID:     lifecycleStepID(identity, 1),
			Generation: generation,
			At:         now,
			Reason:     "recovered_pending_interaction",
		}); err != nil {
			return fmt.Errorf("recover pending interaction lifecycle: %w", err)
		}
		state = r.turnCoordinator.Snapshot()
	}
	if state.StepStatus == turn.StepStatusWaitingInteraction {
		history := r.lifecycleHistorySnapshot()
		if assistant, ok := lastAssistantMessage(history, 0); ok && len(assistant.ToolCalls) > 0 {
			if err := r.lifecycleRecordToolFacts(history, 0, assistant.ToolCalls); err != nil {
				return fmt.Errorf("recover pending tool facts: %w", err)
			}
		}
		identity, generation := r.lifecycleIdentity()
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
			Type:                turn.CommandInteractionResolved,
			SessionID:           r.session.ID,
			TurnID:              identity,
			StepID:              state.StepID,
			Generation:          generation,
			InteractionID:       state.InteractionID,
			InteractionRevision: 1,
			Payload:             resolution,
			At:                  time.Now().UTC(),
		}); err != nil {
			return fmt.Errorf("resolve interaction lifecycle: %w", err)
		}
	}
	return nil
}

func lastAssistantMessage(history []llm.Message, start int) (llm.Message, bool) {
	if start < 0 {
		start = 0
	}
	if start > len(history) {
		start = len(history)
	}
	for i := len(history) - 1; i >= start; i-- {
		if history[i].Role == "assistant" {
			return history[i], true
		}
	}
	return llm.Message{}, false
}

func (r *runtime) lifecycleRecordToolFacts(history []llm.Message, historyStart int, calls []llm.ToolCall) error {
	return r.lifecycleRecordToolFactsMode(history, historyStart, calls, false)
}

// lifecycleRecordModelToolFacts is called only at the assistant-received
// boundary. That is the one normal execution edge allowed to introduce new
// ToolCalls into the lifecycle. All later history observations must refer to
// the ToolBatch already owned by the Coordinator.
func (r *runtime) lifecycleRecordModelToolFacts(history []llm.Message, historyStart int, calls []llm.ToolCall) error {
	return r.lifecycleRecordToolFactsMode(history, historyStart, calls, true)
}

func (r *runtime) lifecycleRecordToolFactsMode(history []llm.Message, historyStart int, calls []llm.ToolCall, allowNewCalls bool) error {
	if r == nil || r.turnCoordinator == nil || len(calls) == 0 {
		return nil
	}
	results := make(map[string]llm.Message)
	if historyStart < 0 {
		historyStart = 0
	}
	for i := historyStart; i < len(history); i++ {
		message := history[i]
		if message.Role == "tool" && strings.TrimSpace(message.ToolCallID) != "" {
			results[message.ToolCallID] = message
		}
	}
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			continue
		}
		if !r.turnCoordinator.HasToolCall(call.ID) {
			if !allowNewCalls {
				// The Coordinator owns the current ToolBatch. Unknown calls in a
				// later history segment are external/context messages, not a new
				// executable request. This is what prevents async callback bridges
				// from becoming a second ToolExecution.
				continue
			}
			if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
				Type:       turn.CommandToolCallRecorded,
				SessionID:  r.session.ID,
				TurnID:     r.turnCoordinator.Snapshot().TurnID,
				StepID:     r.turnCoordinator.Snapshot().StepID,
				Generation: r.turnCoordinator.Snapshot().Generation,
				ToolCallID: call.ID,
				ToolName:   call.Function.Name,
				Arguments:  []byte(call.Function.Arguments),
				At:         time.Now().UTC(),
			}); err != nil {
				return fmt.Errorf("record tool call fact: %w", err)
			}
		}
		result, ok := results[call.ID]
		if !ok {
			continue
		}
		executionID := r.turnCoordinator.ToolExecutionID(call.ID)
		if executionID == "" {
			continue
		}
		now := time.Now().UTC()
		budgetDenied := strings.Contains(strings.ToLower(result.Content), "turn budget exhausted")
		if executionStatus, known := r.turnCoordinator.ToolExecutionStatusForCall(call.ID); !known || executionStatus != turn.ToolExecutionStatusRunning {
			if !budgetDenied && (!known || !executionStatus.Terminal()) {
				if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
					Type:            turn.CommandToolExecutionStarted,
					SessionID:       r.session.ID,
					TurnID:          r.turnCoordinator.Snapshot().TurnID,
					StepID:          r.turnCoordinator.Snapshot().StepID,
					Generation:      r.turnCoordinator.Snapshot().Generation,
					ToolCallID:      call.ID,
					ToolExecutionID: executionID,
					At:              now,
				}); err != nil {
					return fmt.Errorf("record tool execution start fact: %w", err)
				}
			}
		}
		executionStatus, executionKnown := r.turnCoordinator.ToolExecutionStatusForCall(call.ID)
		status := turn.ToolExecutionStatusSucceeded
		errorKind := ""
		if strings.HasPrefix(strings.TrimSpace(result.Content), "ERROR:") {
			status = turn.ToolExecutionStatusFailed
			errorKind = "tool_result_error"
		}
		if budgetDenied {
			status = turn.ToolExecutionStatusDenied
			errorKind = "turn_budget"
		}
		if !executionKnown || !executionStatus.Terminal() {
			executionType := turn.CommandToolExecutionCompleted
			if status != turn.ToolExecutionStatusSucceeded {
				executionType = turn.CommandToolExecutionFailed
			}
			if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
				Type:            executionType,
				SessionID:       r.session.ID,
				TurnID:          r.turnCoordinator.Snapshot().TurnID,
				StepID:          r.turnCoordinator.Snapshot().StepID,
				Generation:      r.turnCoordinator.Snapshot().Generation,
				ToolCallID:      call.ID,
				ToolExecutionID: executionID,
				ExecutionStatus: status,
				ErrorKind:       errorKind,
				At:              now,
			}); err != nil {
				return fmt.Errorf("record tool execution result fact: %w", err)
			}
		}
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
			Type:            turn.CommandToolResultRecorded,
			SessionID:       r.session.ID,
			TurnID:          r.turnCoordinator.Snapshot().TurnID,
			StepID:          r.turnCoordinator.Snapshot().StepID,
			Generation:      r.turnCoordinator.Snapshot().Generation,
			ToolCallID:      call.ID,
			ToolExecutionID: executionID,
			At:              now,
		}); err != nil {
			return fmt.Errorf("record tool result fact: %w", err)
		}
	}
	return nil
}

func isInternalSideEffectCallback(call llm.ToolCall) bool {
	name := strings.ToLower(strings.TrimSpace(call.Function.Name))
	return name == "tool_callback" || name == "get_callback"
}

// withCommittedHistoryLocked makes the message snapshot and the lifecycle
// projection visible as one transition. The caller callback runs while
// lifecycleMu is held and must use lifecycleDispatchLockedErr for dispatches.
func (r *runtime) withCommittedHistoryLocked(history []llm.Message, callback func() error) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.commitHistoryForLifecycleLocked(history)
	return callback()
}

func (r *runtime) commitHistoryForLifecycleLocked(history []llm.Message) {
	r.mu.Lock()
	changed := r.commitStepHistory(&history)
	r.mu.Unlock()
	if changed {
		r.persist(context.Background())
	}
}

// lifecycleAfterModelStep translates the existing StepOutcome/history result
// into the new lifecycle projection. It deliberately does not alter the
// existing execution result, so this adapter can be removed after the new
// Coordinator becomes authoritative.
func (r *runtime) lifecycleAfterModelStep(outcome turn.StepOutcome, history []llm.Message, historyStart int) error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	identity, generation := r.lifecycleIdentity()
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.StepID == "" {
		return nil
	}
	dispatch := func(command turn.TurnCommand) error {
		_, err := r.lifecycleDispatchErr(command)
		return err
	}
	now := time.Now().UTC()
	assistant, hasAssistant := lastAssistantMessage(history, historyStart)
	// On an error, history may still end with the previous Step's assistant
	// message (for example when a provider fails before appending a response).
	// Do not attribute that old message to the failed current Step.
	if outcome.Err == nil && state.StepStatus == turn.StepStatusRequesting {
		if err := dispatch(turn.TurnCommand{
			Type:       turn.CommandAssistantReceived,
			SessionID:  r.session.ID,
			TurnID:     identity,
			StepID:     state.StepID,
			Generation: generation,
			HasTools:   hasAssistant && len(assistant.ToolCalls) > 0,
			At:         now,
		}); err != nil {
			return fmt.Errorf("record assistant lifecycle after step: %w", err)
		}
		state = r.turnCoordinator.Snapshot()
	}
	if hasAssistant && len(assistant.ToolCalls) > 0 {
		recordFacts := r.lifecycleRecordToolFacts
		if outcome.Err == nil && state.StepStatus == turn.StepStatusAssistantReceived {
			recordFacts = r.lifecycleRecordModelToolFacts
		}
		if err := recordFacts(history, historyStart, assistant.ToolCalls); err != nil {
			return err
		}
		state = r.turnCoordinator.Snapshot()
	}
	if outcome.Err != nil {
		return r.withCommittedHistoryLocked(history, func() error {
			state := r.turnCoordinator.Snapshot()
			if errors.Is(outcome.Err, turn.ErrBudgetExhausted) {
				if !state.StepStatus.Terminal() {
					if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
						Type:       turn.CommandFailStep,
						SessionID:  r.session.ID,
						TurnID:     identity,
						StepID:     state.StepID,
						Generation: generation,
						At:         now,
						Reason:     outcome.Err.Error(),
					}); err != nil {
						return fmt.Errorf("fail budget-exhausted step lifecycle: %w", err)
					}
				}
				if !r.turnCoordinator.Snapshot().TurnStatus.Terminal() {
					if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
						Type:       turn.CommandBudgetExhausted,
						SessionID:  r.session.ID,
						TurnID:     identity,
						Generation: generation,
						At:         now,
						Reason:     outcome.Err.Error(),
					}); err != nil {
						return fmt.Errorf("record budget-exhausted turn lifecycle: %w", err)
					}
				}
				return nil
			}
			commandType := turn.CommandFailStep
			turnCommandType := turn.CommandFailTurn
			if errors.Is(outcome.Err, context.Canceled) {
				commandType = turn.CommandInterruptStep
				turnCommandType = turn.CommandInterruptTurn
			}
			if !state.StepStatus.Terminal() {
				if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
					Type:       commandType,
					SessionID:  r.session.ID,
					TurnID:     identity,
					StepID:     state.StepID,
					Generation: generation,
					At:         now,
					Reason:     outcome.Err.Error(),
				}); err != nil {
					return fmt.Errorf("finish failed step lifecycle: %w", err)
				}
			}
			if !r.turnCoordinator.Snapshot().TurnStatus.Terminal() {
				if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
					Type:       turnCommandType,
					SessionID:  r.session.ID,
					TurnID:     identity,
					Generation: generation,
					At:         now,
					Reason:     outcome.Err.Error(),
				}); err != nil {
					return fmt.Errorf("finish failed turn lifecycle: %w", err)
				}
			}
			return nil
		})
	}
	if outcome.Pending != nil {
		pendingPayload, _ := json.Marshal(outcome.Pending)
		return r.withCommittedHistoryLocked(history, func() error {
			state := r.turnCoordinator.Snapshot()
			toolExecutionID := ""
			if len(outcome.Pending.Items) > 0 {
				toolExecutionID = r.turnCoordinator.ToolExecutionID(outcome.Pending.Items[0].ToolCall.ID)
			}
			if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
				Type:            turn.CommandInteractionRequested,
				SessionID:       r.session.ID,
				TurnID:          identity,
				StepID:          state.StepID,
				Generation:      generation,
				InteractionID:   state.InteractionID,
				ToolExecutionID: toolExecutionID,
				Payload:         pendingPayload,
				At:              now,
				Reason:          "pending_interaction",
			}); err != nil {
				return fmt.Errorf("record pending interaction lifecycle: %w", err)
			}
			return nil
		})
	}
	if hasAssistant && len(assistant.ToolCalls) > 0 {
		// Tool calls have only been proposed/accepted at this point. The Step
		// remains executing_tools until a tool_result or a completed HITL
		// resume reaches lifecycleBeginContinuationStep/lifecycleAfterResume.
		return r.withCommittedHistoryLocked(history, func() error { return nil })
	}
	return r.withCommittedHistoryLocked(history, func() error {
		state := r.turnCoordinator.Snapshot()
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandCompleteStep,
			SessionID:  r.session.ID,
			TurnID:     identity,
			StepID:     state.StepID,
			Generation: generation,
			At:         now,
		}); err != nil {
			return fmt.Errorf("complete step lifecycle: %w", err)
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
			Type:       turn.CommandCompleteTurn,
			SessionID:  r.session.ID,
			TurnID:     identity,
			Generation: generation,
			At:         now,
			Reason:     "assistant_completed",
		}); err != nil {
			return fmt.Errorf("complete turn lifecycle: %w", err)
		}
		return nil
	})
}

func (r *runtime) lifecycleAfterResume(outcome turn.StepOutcome, history []llm.Message) error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	identity, generation := r.lifecycleIdentity()
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.StepID == "" {
		return nil
	}
	dispatch := func(command turn.TurnCommand) error {
		_, err := r.lifecycleDispatchErr(command)
		return err
	}
	now := time.Now().UTC()
	assistant, hasAssistant := lastAssistantMessage(history, 0)
	if hasAssistant && len(assistant.ToolCalls) > 0 {
		if err := r.lifecycleRecordToolFacts(history, 0, assistant.ToolCalls); err != nil {
			return err
		}
		state = r.turnCoordinator.Snapshot()
	}
	// A recovered legacy HITL state is still waiting in the coordinator. The
	// current runtime has already consumed the resume command, so normalize it
	// before applying the same post-resume settlement path as a live state.
	if state.StepStatus == turn.StepStatusWaitingInteraction {
		if err := dispatch(turn.TurnCommand{Type: turn.CommandInteractionResolved, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, InteractionID: state.InteractionID, InteractionRevision: 1, At: now}); err != nil {
			return fmt.Errorf("resolve resumed interaction lifecycle: %w", err)
		}
		state = r.turnCoordinator.Snapshot()
	}
	if outcome.Err != nil {
		return r.withCommittedHistoryLocked(history, func() error {
			state := r.turnCoordinator.Snapshot()
			if errors.Is(outcome.Err, turn.ErrBudgetExhausted) {
				if !state.StepStatus.Terminal() {
					if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandFailStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: outcome.Err.Error()}); err != nil {
						return fmt.Errorf("fail resumed budget step lifecycle: %w", err)
					}
				}
				if !r.turnCoordinator.Snapshot().TurnStatus.Terminal() {
					if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandBudgetExhausted, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: outcome.Err.Error()}); err != nil {
						return fmt.Errorf("record resumed budget turn lifecycle: %w", err)
					}
				}
				return nil
			}
			stepType := turn.CommandFailStep
			turnType := turn.CommandFailTurn
			if errors.Is(outcome.Err, context.Canceled) {
				stepType = turn.CommandInterruptStep
				turnType = turn.CommandInterruptTurn
			}
			if !state.StepStatus.Terminal() {
				if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: stepType, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: outcome.Err.Error()}); err != nil {
					return fmt.Errorf("finish resumed failed step lifecycle: %w", err)
				}
			}
			if !r.turnCoordinator.Snapshot().TurnStatus.Terminal() {
				if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turnType, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: outcome.Err.Error()}); err != nil {
					return fmt.Errorf("finish resumed failed turn lifecycle: %w", err)
				}
			}
			return nil
		})
	}
	if outcome.Pending != nil {
		pendingPayload, err := json.Marshal(outcome.Pending)
		if err != nil {
			return fmt.Errorf("marshal resumed pending interaction: %w", err)
		}
		return r.withCommittedHistoryLocked(history, func() error {
			state := r.turnCoordinator.Snapshot()
			toolExecutionID := ""
			if len(outcome.Pending.Items) == 1 {
				toolExecutionID = r.turnCoordinator.ToolExecutionID(outcome.Pending.Items[0].ToolCall.ID)
			}
			if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
				Type:            turn.CommandInteractionRequested,
				SessionID:       r.session.ID,
				TurnID:          identity,
				StepID:          state.StepID,
				Generation:      generation,
				InteractionID:   state.InteractionID,
				InteractionKind: legacyPendingInteractionKind(outcome.Pending),
				ToolExecutionID: toolExecutionID,
				Payload:         pendingPayload,
				At:              now,
				Reason:          "pending_interaction",
			}); err != nil {
				return fmt.Errorf("record resumed pending interaction lifecycle: %w", err)
			}
			return nil
		})
	}
	if outcome.ScheduleToolResult {
		// The model emitted another tool batch after the resumed execution. It
		// is a new continuation Step boundary, so leave this Step executing
		// until its results arrive instead of falsely settling the old batch.
		return r.withCommittedHistoryLocked(history, func() error { return nil })
	}
	return r.withCommittedHistoryLocked(history, func() error {
		state := r.turnCoordinator.Snapshot()
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandToolBatchSettled, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now}); err != nil {
			return fmt.Errorf("settle resumed tool batch lifecycle: %w", err)
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandCompleteStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: "interaction_resolved"}); err != nil {
			return fmt.Errorf("complete resumed step lifecycle: %w", err)
		}
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandCompleteTurn, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: "assistant_completed_after_interaction"}); err != nil {
			return fmt.Errorf("complete resumed turn lifecycle: %w", err)
		}
		return nil
	})
}

func (r *runtime) lifecycleCancel() error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.lifecycleCancelLocked()
}

func (r *runtime) lifecycleCancelLocked() error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn {
		return nil
	}
	now := time.Now().UTC()
	if state.StepID != "" && !state.StepStatus.Terminal() {
		if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandCancelStep, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, At: now, Reason: "cancelled_by_user"}); err != nil {
			return fmt.Errorf("cancel lifecycle step: %w", err)
		}
	}
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandCancelTurn, SessionID: r.session.ID, TurnID: state.TurnID, Generation: state.Generation, At: now, Reason: "cancelled_by_user"}); err != nil {
		return fmt.Errorf("cancel lifecycle turn: %w", err)
	}
	return nil
}

// lifecycleContextCompacted records a context epoch change at the Step
// boundary where compression actually happened. Keeping this as metadata
// avoids changing the model-visible prompt shape beyond the existing
// compaction behavior while making cache-invalidating boundaries observable.
func (r *runtime) lifecycleContextCompacted(reason, beforeDigest, afterDigest string, beforeCount, afterCount int) error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	identity, generation := r.lifecycleIdentity()
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.StepID == "" || state.StepStatus != turn.StepStatusRequesting {
		return nil
	}
	if _, err := r.lifecycleDispatchErr(turn.TurnCommand{
		Type:                 turn.CommandContextCompacted,
		SessionID:            r.session.ID,
		TurnID:               identity,
		StepID:               state.StepID,
		Generation:           generation,
		At:                   time.Now().UTC(),
		Reason:               reason,
		ContextBeforeDigest:  beforeDigest,
		ContextAfterDigest:   afterDigest,
		CompactedMessageFrom: afterCount,
		CompactedMessageTo:   beforeCount,
	}); err != nil {
		return fmt.Errorf("record context compaction lifecycle: %w", err)
	}
	return nil
}

// reconcileToolExecution applies an operator- or external-provider-confirmed
// result to an execution that was marked unknown after restart. It is the only
// path allowed to turn Unknown back into a known terminal result; after all
// unknown executions are reconciled, the normal Step continuation is queued.
func (r *runtime) reconcileToolExecution(ctx context.Context, turnID, stepID, executionID string, status turn.ToolExecutionStatus, content string) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if status == turn.ToolExecutionStatusUnknown || !status.Terminal() {
		return fmt.Errorf("reconciliation requires a known terminal status")
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.TurnID != strings.TrimSpace(turnID) || state.StepID != strings.TrimSpace(stepID) {
		return fmt.Errorf("turn or step is not active")
	}
	toolCallID, toolName, ok := r.turnCoordinator.ToolExecutionInfo(executionID)
	if !ok {
		return fmt.Errorf("unknown tool execution %s", executionID)
	}
	if !state.RecoveryRequired {
		return fmt.Errorf("tool execution does not require reconciliation")
	}
	if strings.TrimSpace(content) == "" {
		if status == turn.ToolExecutionStatusSucceeded {
			content = "tool execution reconciled as completed"
		} else {
			content = "ERROR: tool execution reconciled as " + string(status)
		}
	}

	_, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:            turn.CommandToolExecutionReconciled,
		SessionID:       r.session.ID,
		TurnID:          state.TurnID,
		StepID:          state.StepID,
		Generation:      state.Generation,
		ToolCallID:      toolCallID,
		ToolExecutionID: executionID,
		ExecutionStatus: status,
		ResultContent:   content,
		ErrorKind:       "reconciled",
		Reason:          "external tool execution reconciliation",
		At:              time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	r.mu.Lock()
	resultExists := false
	for _, message := range r.messages {
		if message.Role == "tool" && message.ToolCallID == toolCallID {
			resultExists = true
			break
		}
	}
	if !resultExists {
		r.messages = append(r.messages, llm.ToolResultMessage(toolCallID, toolName, content))
		r.historyRevision++
	}
	r.mu.Unlock()
	state, err = r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:            turn.CommandToolResultRecorded,
		SessionID:       r.session.ID,
		TurnID:          state.TurnID,
		StepID:          state.StepID,
		Generation:      state.Generation,
		ToolCallID:      toolCallID,
		ToolExecutionID: executionID,
		At:              time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	if state.RecoveryRequired {
		r.persist(ctx)
		return nil
	}
	started, err := r.lifecycleBeginContinuationStepLocked(turn.TurnSourceHuman)
	if err != nil {
		r.persist(ctx)
		return err
	}
	if !started {
		r.persist(ctx)
		return nil
	}
	if err := r.enqueueToolResult(ctx, r.session.ID); err != nil {
		r.persist(ctx)
		return err
	}
	r.persist(ctx)
	return nil
}
