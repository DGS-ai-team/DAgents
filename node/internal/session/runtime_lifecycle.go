package session

import (
	"bytes"
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

// restoreLifecycleEvents rebuilds the in-memory Turn/Step projection from the
// durable event log. It is the fallback for runtimes constructed directly by
// embedded callers or focused tests; Manager-created runtimes pass the events
// already loaded during session restoration through restoreLifecycleEventsFrom.
func (r *runtime) restoreLifecycleEvents() {
	r.restoreLifecycleEventsFrom(nil, false)
}

// restoreLifecycleEventsFrom rebuilds the in-memory Turn/Step projection
// before the runtime starts consuming new queue commands. A loaded=true
// snapshot, including an empty event list, is authoritative for this restore
// and avoids a second database scan. The durable transcript is used only to
// reconcile an execution whose result was committed before its lifecycle fact;
// lifecycle events remain authoritative for execution position.
func (r *runtime) restoreLifecycleEventsFrom(initial []turn.TurnEventEnvelope, loaded bool) {
	if r == nil || r.store == nil || r.turnCoordinator == nil {
		return
	}
	var events []turn.TurnEventEnvelope
	if loaded {
		events = append(events, initial...)
	} else {
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
	// Lifecycle facts are committed before a tool side effect starts, while the
	// durable transcript snapshot is persisted after the step returns. A
	// process can therefore restart with the assistant message missing even
	// though its ToolCallRecorded facts are durable. Rebuild the provider-facing
	// assistant envelope from that authoritative projection before any result is
	// appended; otherwise recovery would create an orphan tool message.
	if calls := r.activeToolCallsFromLifecycle(); len(calls) > 0 {
		r.restoreActiveToolCallMessage(calls)
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
			if err := r.enqueueTurnContinuation(context.Background()); err != nil && r.logger != nil {
				r.logger.Warn("schedule recovered turn continuation failed", "session_id", r.session.ID, "error", err)
			}
		} else if r.logger != nil {
			r.logger.Warn("turn recovery requires external tool execution decision", "session_id", r.session.ID, "turn_id", snapshot.TurnID, "step_id", snapshot.StepID)
		}
	}
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
	case turn.CommandModelContextChanged:
		return turn.EventModelContextChanged, true
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

// recordSideEffectFact is the lifecycle boundary for async tool results that
// arrive after the original model/tool request. The callback bridge may write
// a model-readable result, but it never registers a new ToolCall. This fact is
// the durable evidence that the result was accepted exactly once.
func (r *runtime) recordSideEffectFact(entry readySideEffect) {
	if r == nil || r.turnCoordinator == nil {
		return
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.StepID == "" {
		if r.logger != nil {
			r.logger.Debug("skip async side-effect fact without active step", "session_id", r.session.ID, "seq", entry.seq)
		}
		return
	}
	factID := fmt.Sprintf("async:%s", strings.TrimSpace(entry.async.JobID))
	if entry.async.JobID == "" {
		factID = fmt.Sprintf("async:seq:%d", entry.seq)
	}
	payload, err := json.Marshal(map[string]any{
		"seq":          entry.seq,
		"kind":         "async",
		"job_id":       entry.async.JobID,
		"tool_call_id": entry.async.ToolCallID,
		"tool_name":    entry.async.ToolName,
		"status":       entry.async.Status,
		"result_text":  entry.async.ResultText,
		"error_text":   entry.async.ErrorText,
	})
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("marshal async side-effect fact failed", "session_id", r.session.ID, "seq", entry.seq, "error", err)
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
		ExternalFactKind: "async",
		ToolCallID:       entry.async.ToolCallID,
		ToolName:         entry.async.ToolName,
		ResultContent:    entry.built.ForClientContent,
		Payload:          payload,
		At:               time.Now().UTC(),
		Reason:           "side_effect_applied",
	}); err != nil && r.logger != nil {
		r.logger.Warn("record async side-effect fact failed", "session_id", r.session.ID, "seq", entry.seq, "error", err)
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

// pendingSnapshot is the API projection of the Coordinator's durable
// Interaction payload.
func (r *runtime) pendingSnapshot() *turn.PendingHITL {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	state := r.turnCoordinator.Snapshot()
	return pendingFromLifecycleSnapshot(state)
}

func pendingFromLifecycleSnapshot(state turn.CoordinatorSnapshot) *turn.PendingHITL {
	if state.StepStatus != turn.StepStatusWaitingInteraction {
		return nil
	}
	if len(state.InteractionPayload) > 0 {
		var pending turn.PendingHITL
		if err := json.Unmarshal(state.InteractionPayload, &pending); err == nil && len(pending.Items) > 0 {
			return &pending
		}
	}
	return nil
}

func pendingInteractionKind(pending *turn.PendingHITL) string {
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

func lifecycleToolResultsPresent(history []llm.Message, calls []llm.ToolCall) bool {
	if len(calls) == 0 {
		return false
	}
	results := make(map[string]struct{}, len(calls))
	for _, message := range history {
		if message.Role == "tool" && strings.TrimSpace(message.ToolCallID) != "" && !llm.IsRecoveryPlaceholderToolResult(message) {
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
		if history[i].Role == "tool" && strings.TrimSpace(history[i].ToolCallID) == toolCallID && !llm.IsRecoveryPlaceholderToolResult(history[i]) {
			return history[i], true
		}
	}
	return llm.Message{}, false
}

func lifecycleStepID(turnID string, index int) string {
	return fmt.Sprintf("%s-step-%d", turnID, index)
}

// lifecycleBeginHumanTurn creates a fresh Turn and its first Step.  A live
// Turn is not implicitly interrupted by a new human input; callers must use
// the explicit cancel control path first.  Queue/InputBox fencing is derived
// from the Coordinator projection.
func (r *runtime) lifecycleBeginHumanTurn() error {
	return r.lifecycleBeginInputTurn(turn.TurnSourceHuman)
}

func (r *runtime) lifecycleBeginInputTurn(source turn.TurnSource) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.lifecycleBeginInputTurnLocked(source)
}

func (r *runtime) lifecycleBeginInputTurnLocked(source turn.TurnSource) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	old := r.turnCoordinator.Snapshot()
	if old.HasActiveTurn && !old.TurnStatus.Terminal() {
		return fmt.Errorf("active turn must be cancelled before accepting a new human input")
	}

	turnID := newContinuationID()

	now := time.Now().UTC()
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type:      turn.CommandStartTurn,
		SessionID: r.session.ID,
		TurnID:    turnID,
		Source:    source,
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
// and passive side-effect continuations. Only a side-effect callback may open
// a new Turn when the previous Turn is already terminal; model/tool
// continuations must always remain inside an active Turn.
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
		if source != turn.TurnSourceSideEffect {
			return false, fmt.Errorf("cannot continue without an active turn")
		}
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
			Reason:     "side_effect_continuation",
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

// lifecyclePrepareResume moves the active Step back into tool execution before
// the resume implementation writes tool results. Pending HITL is durable
// lifecycle state, so a resume without an active Turn is invalid.
func (r *runtime) lifecyclePrepareResume(resumeValue map[string]any) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	state := r.turnCoordinator.Snapshot()
	resolution, _ := json.Marshal(resumeValue)
	if !state.HasActiveTurn {
		return fmt.Errorf("cannot resume without an active turn")
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

func assistantCoversToolCalls(message llm.Message, calls []llm.ToolCall) bool {
	if message.Role != "assistant" || len(calls) == 0 || len(message.ToolCalls) != len(calls) {
		return false
	}
	if err := llm.ValidateAssistantMessage(message); err != nil {
		return false
	}
	known := make(map[string]llm.ToolCall, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		known[strings.TrimSpace(call.ID)] = call
	}
	for _, call := range calls {
		knownCall, ok := known[strings.TrimSpace(call.ID)]
		if !ok || knownCall.Function.Name != call.Function.Name || !sameJSON(knownCall.Function.Arguments, call.Function.Arguments) {
			return false
		}
	}
	return true
}

func sameJSON(left, right string) bool {
	var leftCompact, rightCompact bytes.Buffer
	if err := json.Compact(&leftCompact, []byte(left)); err != nil {
		return false
	}
	if err := json.Compact(&rightCompact, []byte(right)); err != nil {
		return false
	}
	return leftCompact.String() == rightCompact.String()
}

// restoreActiveToolCallMessage places the active lifecycle batch immediately
// after the latest user message. Older snapshots could contain the assistant
// envelope before a recovered in-flight user input, which is still invalid
// even if the call and result IDs match.
func (r *runtime) restoreActiveToolCallMessage(calls []llm.ToolCall) {
	if r == nil || len(calls) == 0 {
		return
	}
	history := r.lifecycleHistorySnapshot()
	assistantIndex := -1
	callIDs := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		callIDs[strings.TrimSpace(call.ID)] = struct{}{}
	}
	activeAssistantIndices := make(map[int]struct{})
	activeResultIndices := make(map[int]struct{})
	resultByCallID := make(map[string]llm.Message, len(calls))
	for index, message := range history {
		if assistantCoversToolCalls(message, calls) {
			if assistantIndex < 0 {
				assistantIndex = index
			}
		}
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				if _, ok := callIDs[strings.TrimSpace(call.ID)]; ok {
					activeAssistantIndices[index] = struct{}{}
					break
				}
			}
		}
		if message.Role == "tool" {
			if _, ok := callIDs[strings.TrimSpace(message.ToolCallID)]; ok {
				activeResultIndices[index] = struct{}{}
				resultByCallID[strings.TrimSpace(message.ToolCallID)] = message
			}
		}
	}
	lastUserIndex := -1
	for index, message := range history {
		if message.Role == "user" {
			lastUserIndex = index
		}
	}
	needsMove := assistantIndex < 0 || assistantIndex <= lastUserIndex
	if !needsMove {
		for index := range activeResultIndices {
			if index < assistantIndex {
				needsMove = true
				break
			}
		}
	}
	for _, call := range calls {
		if _, ok := resultByCallID[strings.TrimSpace(call.ID)]; !ok {
			needsMove = true
			break
		}
	}
	if !needsMove {
		return
	}
	assistant := llm.Message{Role: "assistant", ToolCalls: calls}
	if assistantIndex >= 0 {
		assistant = history[assistantIndex]
	} else {
		for index := range activeAssistantIndices {
			assistant = history[index]
			break
		}
	}
	// Lifecycle facts are authoritative for the call envelope. Keep any
	// existing assistant text/reasoning, but replace a stale or partial tool
	// list with the durable calls so the reconstructed message validates.
	assistant.ToolCalls = append([]llm.ToolCall(nil), calls...)
	results := make([]llm.Message, 0, len(calls))
	for _, call := range calls {
		id := strings.TrimSpace(call.ID)
		if result, ok := resultByCallID[id]; ok {
			results = append(results, result)
			continue
		}
		results = append(results, llm.RecoveryPlaceholderToolResult(call))
	}
	kept := make([]llm.Message, 0, len(history))
	for index, message := range history {
		if _, ok := activeAssistantIndices[index]; ok {
			continue
		}
		if _, ok := activeResultIndices[index]; ok {
			continue
		}
		kept = append(kept, message)
	}
	history = kept
	lastUserIndex = -1
	for index, message := range history {
		if message.Role == "user" {
			lastUserIndex = index
		}
	}
	insertAt := lastUserIndex + 1
	if insertAt < 0 {
		insertAt = 0
	}
	if insertAt > len(history) {
		insertAt = len(history)
	}
	reordered := make([]llm.Message, 0, len(history)+1)
	reordered = append(reordered, history[:insertAt]...)
	reordered = append(reordered, assistant)
	reordered = append(reordered, results...)
	reordered = append(reordered, history[insertAt:]...)
	if err := llm.ValidateToolProtocol(reordered); err != nil {
		if r.logger != nil {
			r.logger.Warn("reconstructed active tool history is invalid", "session_id", r.session.ID, "error", err)
		}
		return
	}
	r.mu.Lock()
	r.messages = reordered
	r.historyRevision++
	r.mu.Unlock()
	r.persist(context.Background())
}

func (r *runtime) activeToolCallsFromLifecycle() []llm.ToolCall {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	projections := r.turnCoordinator.ActiveToolCalls()
	if len(projections) == 0 {
		return nil
	}
	calls := make([]llm.ToolCall, 0, len(projections))
	for _, projection := range projections {
		calls = append(calls, llm.ToolCall{
			ID:   projection.ID,
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      projection.ToolName,
				Arguments: string(projection.Arguments),
			},
		})
	}
	return calls
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
		if message.Role == "tool" && strings.TrimSpace(message.ToolCallID) != "" && !llm.IsRecoveryPlaceholderToolResult(message) {
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
	if !r.turnEpochCurrentLocked() {
		r.mu.Unlock()
		return
	}
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
		err := r.withCommittedHistoryLocked(history, func() error {
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
		if err != nil {
			return err
		}
		r.orch.PublishPendingHITL(r.session.ID, outcome.Pending)
		return nil
	}
	if hasAssistant && len(assistant.ToolCalls) > 0 {
		// Tool calls have only been proposed/accepted at this point. The Step
		// remains executing_tools until the inline continuation or a completed HITL
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
	// The resume command has consumed the waiting interaction. Normalize the
	// coordinator before applying the same post-resume settlement path as a
	// live state.
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
		err = r.withCommittedHistoryLocked(history, func() error {
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
				InteractionKind: pendingInteractionKind(outcome.Pending),
				ToolExecutionID: toolExecutionID,
				Payload:         pendingPayload,
				At:              now,
				Reason:          "pending_interaction",
			}); err != nil {
				return fmt.Errorf("record resumed pending interaction lifecycle: %w", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
		r.orch.PublishPendingHITL(r.session.ID, outcome.Pending)
		return nil
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
	// CommandCancelTurn is the cancellation fence. The coordinator closes the
	// active Step, executions, interaction and batch in the same durable
	// projection before publishing one terminal turn_state event; emitting a
	// separate Step cancellation first would expose an impossible intermediate
	// state to SSE clients and to a concurrent restore.
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{Type: turn.CommandCancelTurn, SessionID: r.session.ID, TurnID: state.TurnID, Generation: state.Generation, At: now, Reason: "cancelled_by_user"}); err != nil {
		return fmt.Errorf("cancel lifecycle turn: %w", err)
	}
	return nil
}

// lifecycleContextCompacted records the durable history compaction boundary.
// The following ModelContextChanged fact advances ContextEpoch once the next
// model request has rebuilt its model-visible context segment.
func (r *runtime) lifecycleContextCompacted(reason, beforeDigest, afterDigest string, beforeCount, afterCount int) error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	identity, generation := r.lifecycleIdentity()
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.StepID == "" ||
		(state.StepStatus != turn.StepStatusRequesting && state.StepStatus != turn.StepStatusWaitingInteraction) {
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
	reconciledMetadata := tools.ResultMetadata{Status: tools.ResultStatus(status)}
	if status != turn.ToolExecutionStatusSucceeded {
		reconciledMetadata.Error = &tools.ResultError{
			Code:      "reconciled_" + string(status),
			Message:   "tool execution was reconciled after restart",
			Retryable: false,
		}
	}
	reconciledMessage := llm.ToolResultMessageWithMetadata(toolCallID, toolName, content, reconciledMetadata)
	r.mu.Lock()
	resultExists := false
	for index, message := range r.messages {
		if message.Role != "tool" || message.ToolCallID != toolCallID {
			continue
		}
		resultExists = true
		if llm.IsRecoveryPlaceholderToolResult(message) {
			r.messages[index] = reconciledMessage
			r.historyRevision++
		}
		break
	}
	if !resultExists {
		r.messages = append(r.messages, reconciledMessage)
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
	if err := r.enqueueTurnContinuation(ctx); err != nil {
		r.persist(ctx)
		return err
	}
	r.persist(ctx)
	return nil
}
