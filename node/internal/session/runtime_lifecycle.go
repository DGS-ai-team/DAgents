package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
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

// withTerminalHistoryLocked is the terminal durability fence: it prepares the
// final history/checkpoint delta and runtime Ack state, applies all terminal
// lifecycle commands tentatively, and commits them in one SQLite transaction.
// No observer, hook, or SSE notification runs before that transaction commits.
func (r *runtime) withTerminalHistoryLocked(history []llm.Message, commands []turn.TurnCommand, snapshotStatus string) error {
	r.lifecycleMu.Lock()
	r.mu.Lock()
	if r.turnEpochCurrentLocked() && r.commitStepHistory(&history) {
		r.historyCommitDirty = true
	}
	r.mu.Unlock()
	for i := range commands {
		if commands[i].CommandID == "" {
			commands[i].CommandID = r.lifecycleNextCommandID()
		}
	}
	var persist func([]turn.TurnCommand, []turn.CoordinatorSnapshot) error
	if r.store != nil {
		persist = func(applied []turn.TurnCommand, projections []turn.CoordinatorSnapshot) error {
			return r.persistTerminalBatchLocked(history, applied, projections, snapshotStatus)
		}
	}
	snapshots, err := r.turnCoordinator.DispatchBatchDurable(commands, persist)
	if err != nil {
		r.markResultCommitFailed()
		state := r.turnCoordinator.Snapshot()
		r.lifecycleMu.Unlock()
		r.publishTurnState(state, "")
		return fmt.Errorf("commit terminal lifecycle boundary: %w", err)
	}
	r.lifecycleMu.Unlock()
	for i, state := range snapshots {
		if r.onLifecycle != nil {
			if err := r.onLifecycle(r.session.ID, state); err != nil {
				return fmt.Errorf("turn lifecycle observer failed after terminal commit: %w", err)
			}
		}
		r.publishTurnState(state, commands[i].Type)
	}
	state := snapshots[len(snapshots)-1]
	if r.store == nil && terminalNeedsQueuePause(state.TurnStatus) && r.inputBox != nil {
		if _, _, err := r.inputBox.SetQueuePaused(true, 0); err != nil && r.logger != nil {
			r.logger.Warn("pause input queue after non-successful turn failed", "session_id", r.session.ID, "error", err)
		}
	}
	r.mu.Lock()
	revision := r.historyRevision
	r.mu.Unlock()
	if r.orch != nil && state.TurnStatus.Terminal() {
		additional := map[string]any{"turn_id": state.TurnID, "turn_generation": state.Generation, "history_revision": revision}
		if state.TurnStatus == turn.TurnStatusBudgetExhausted {
			additional["budget_reason"] = state.TurnEndReason
		}
		if state.TurnEndReason == "auto_idle" {
			r.orch.RunTurnDoneCommitted(r.session.ID, "stop")
			additional["no_work"] = true
		}
		r.orch.PublishCommittedTurnFinished(r.session.ID, terminalFinishReason(state), additional)
	}
	return nil
}

func (r *runtime) persistTerminalBatchLocked(history []llm.Message, commands []turn.TurnCommand, projections []turn.CoordinatorSnapshot, snapshotStatus string) error {
	if len(commands) == 0 || len(commands) != len(projections) {
		return fmt.Errorf("invalid terminal lifecycle batch")
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	r.deltaMu.Lock()
	if r.historyMutationErr != nil {
		err := r.historyMutationErr
		r.deltaMu.Unlock()
		r.mu.Unlock()
		return fmt.Errorf("allocate persisted history identity: %w", err)
	}
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	notifySeq, ackSeq := r.notifySeq, r.ackSeq
	historyRevision, conversationRevision, checkpointRevision := r.historyRevision, r.conversationRevision, r.contextCheckpointRevision
	upsertsByID := make(map[string]pendingRecordMutation, len(r.recordDeltaUpserts))
	for id, p := range r.recordDeltaUpserts {
		upsertsByID[id] = p
	}
	deletes := make([]string, 0, len(r.recordDeltaDeletes))
	deleteSeq := make(map[string]uint64, len(r.recordDeltaDeletes))
	for id, seq := range r.recordDeltaDeletes {
		deletes = append(deletes, id)
		deleteSeq[id] = seq
	}
	checkpointDirty, checkpointSeq := r.checkpointDirty, r.checkpointMutationSequence
	checkpointFullReplaceSeq := r.checkpointFullReplaceSequence
	checkpointMutations := append([]pendingCheckpointMutation(nil), r.checkpointMutations...)
	lastHumanInteraction := r.lastHumanInteraction
	terminalProjection := projections[len(projections)-1]
	if terminalProjection.TurnStatus.Terminal() && terminalProjection.Source == turn.TurnSourceHuman && terminalProjection.TurnFinishedAt != nil && terminalProjection.TurnFinishedAt.After(lastHumanInteraction) {
		lastHumanInteraction = terminalProjection.TurnFinishedAt.UTC()
	}
	type terminalUpsert struct {
		id      string
		pending pendingRecordMutation
	}
	orderedUpserts := make([]terminalUpsert, 0, len(upsertsByID))
	for id, p := range upsertsByID {
		orderedUpserts = append(orderedUpserts, terminalUpsert{id: id, pending: p})
	}
	sort.Slice(orderedUpserts, func(i, j int) bool { return orderedUpserts[i].pending.Sequence < orderedUpserts[j].pending.Sequence })
	upserts := make([]llm.Message, 0, len(upsertsByID))
	recordOwners := make(map[string]store.RecordOwnership, len(upsertsByID))
	upSeq := make(map[string]uint64, len(upsertsByID))
	for _, entry := range orderedUpserts {
		upserts = append(upserts, entry.pending.Message)
		upSeq[entry.id] = entry.pending.Sequence
		if entry.pending.Owner.RoundID != "" {
			recordOwners[entry.id] = entry.pending.Owner
		}
	}
	activeRoundID, activeRevisionID := r.activeRoundID, r.activeRevisionID
	var checkpoint *store.CheckpointCommit
	if checkpointDirty {
		start := r.activeContextStart
		lineage := "live_append"
		if start > 0 || r.lastContextResetID != "" || r.dreamingAttempt != nil {
			lineage = "derived_checkpoint"
		}
		checkpoint = buildCheckpointCommit(r.messages, start, lineage, checkpointRevision, checkpointSeq, checkpointFullReplaceSeq, checkpointMutations)
	}
	state := store.RuntimeState{InputBoxState: nil, HistoryRevision: historyRevision, ConversationRevision: conversationRevision, ContextCheckpointRevision: checkpointRevision, ToolCallProjectionHiddenThroughGeneration: r.toolCallProjectionHiddenThroughGeneration, ActiveContextStart: r.activeContextStart, LastContextResetID: r.lastContextResetID, LastHumanInteraction: lastHumanInteraction, NotifySeq: notifySeq, AckSeq: ackSeq}
	if r.dreamingAttempt != nil {
		state.DreamingAttempt, _ = json.Marshal(*r.dreamingAttempt)
	}
	firstUser := ""
	for _, m := range upserts {
		if m.Role == "user" {
			firstUser = llm.MessageTextSummary(m)
			if firstUser == "" && llm.MessageHasImages(m) {
				firstUser = "[image]"
			}
			if firstUser != "" {
				break
			}
		}
	}
	r.deltaMu.Unlock()
	r.mu.Unlock()
	if r.orch != nil {
		state.HookStore = hooks.CloneSessionStore(r.orch.HookStoreSnapshot())
	}
	var terminalInputSeq, terminalInputRevision uint64
	var inputQueueRows []store.InputQueueRow
	if r.inputBox != nil {
		// The terminal transaction needs a completed marker in its durable
		// runtime snapshot, without mutating the live mailbox before COMMIT.
		// Otherwise a failed terminal write followed by an ordinary runtime Save
		// could persist that marker without the terminal history/lifecycle facts.
		mailboxSnapshot, rows, seq, revision, err := completedInputBoxSnapshot(r.inputBox, terminalNeedsQueuePause(terminalProjection.TurnStatus))
		if err != nil {
			return fmt.Errorf("snapshot completed input ownership: %w", err)
		}
		terminalInputSeq = seq
		terminalInputRevision = revision
		state.InputBoxState = mailboxSnapshot
		inputQueueRows = rows
	}
	rec := store.Record{AgentID: r.session.ID, NodeID: r.session.AgentID, FirstUserMessage: firstUser, LoadedSkills: loaded, RuntimeState: state, InputQueueRows: inputQueueRows}
	var delta *store.ConversationDelta
	var roundRevision *store.RoundRevision
	if activeRoundID != "" && activeRevisionID != "" {
		roundRevision = &store.RoundRevision{ConversationID: r.session.ID, RoundID: activeRoundID, RevisionID: activeRevisionID, TurnID: terminalProjection.TurnID, Source: string(terminalProjection.Source), Generation: terminalProjection.Generation, Status: "active"}
	}
	if len(upserts) > 0 || len(deletes) > 0 || checkpoint != nil {
		delta = &store.ConversationDelta{ConversationID: r.session.ID, NodeID: r.session.AgentID, ExpectedRevision: conversationRevision, HistoryRevision: historyRevision, Upserts: upserts, RecordOwners: recordOwners, RoundRevision: roundRevision, DeleteIDs: deletes, Checkpoint: checkpoint}
	}
	events := make([]turn.TurnEventEnvelope, 0, len(commands))
	for i, command := range commands {
		e, ok, err := r.lifecycleEventEnvelope(command, projections[i])
		if err != nil {
			return err
		}
		if ok {
			events = append(events, e)
		}
	}
	if len(events) == 0 {
		return fmt.Errorf("terminal lifecycle batch has no durable facts")
	}
	var messages []llm.Message
	if snapshotStatus == "readable" {
		messages, _ = r.store.RebuildTurnMessages(context.Background(), r.session.ID, projections[len(projections)-1].TurnID)
		if len(messages) == 0 {
			snapshotStatus = "unreadable"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stored, result, err := r.store.CommitTerminalBoundary(ctx, store.TerminalBoundary{Record: rec, Delta: delta, RoundRevision: roundRevision, Events: events, SnapshotMessages: messages, SnapshotStatus: snapshotStatus, CompletedInputSeq: terminalInputSeq, CompletedInputRevision: terminalInputRevision})
	if err != nil {
		return err
	}
	// Keep the in-memory mailbox aligned with the just-committed durable marker
	// before releasing persistMu. Otherwise a concurrent ordinary Save could
	// snapshot the stale uncompleted live record and overwrite the terminal Ack.
	if terminalInputSeq != 0 && r.inputBox != nil {
		r.inputBox.MarkCompleted(terminalInputSeq)
	}
	if r.inputBox != nil {
		r.inputBox.MarkPersistenceCommitted(state.InputBoxState)
		if paused, revision, err := QueueControlFromSnapshot(state.InputBoxState); err == nil {
			r.inputBox.RestoreQueueControl(paused, revision)
		}
	}
	r.mu.Lock()
	r.deltaMu.Lock()
	r.conversationRevision = result.ConversationRevision
	r.contextCheckpointRevision = result.CheckpointRevision
	r.lastHumanInteraction = lastHumanInteraction
	r.activeRoundID, r.activeRevisionID = "", ""
	for id, seq := range upSeq {
		if current, ok := r.recordDeltaUpserts[id]; ok && current.Sequence == seq {
			delete(r.recordDeltaUpserts, id)
		}
	}
	for id, seq := range deleteSeq {
		if current, ok := r.recordDeltaDeletes[id]; ok && current == seq {
			delete(r.recordDeltaDeletes, id)
		}
	}
	if checkpoint != nil {
		remaining := r.checkpointMutations[:0]
		for _, mutation := range r.checkpointMutations {
			if mutation.Sequence > checkpointSeq {
				remaining = append(remaining, mutation)
			}
		}
		r.checkpointMutations = remaining
		if r.checkpointFullReplaceSequence != 0 && r.checkpointFullReplaceSequence <= checkpointSeq {
			r.checkpointFullReplaceSequence = 0
		}
		if r.historyFullReplaceSequence != 0 && r.historyFullReplaceSequence <= checkpointSeq {
			r.historyFullReplaceSequence = 0
		}
		r.checkpointDirty = r.checkpointFullReplaceSequence != 0 || len(r.checkpointMutations) > 0
	}
	r.historyCommitDirty = false
	r.resultCommitStatus = "committed"
	r.deltaMu.Unlock()
	r.mu.Unlock()
	if len(stored) > 0 {
		r.setLifecycleEventSequence(stored[len(stored)-1].SessionSeq)
	}
	r.publishConversationPatch(result, historyRevision, "committed", &terminalProjection, activeRoundID, activeRevisionID, state.InputBoxState, inputQueueRows)
	return nil
}

func completedInputBoxSnapshot(inputBox *InputBox, pauseQueue bool) (json.RawMessage, []store.InputQueueRow, uint64, uint64, error) {
	if inputBox == nil {
		return nil, nil, 0, 0, nil
	}
	copyBox := NewInputBox()
	if err := copyBox.Restore(inputBox.Snapshot()); err != nil {
		return nil, nil, 0, 0, err
	}
	var completedSeq, completedRevision uint64
	if inFlight, ok := copyBox.InFlight(); ok {
		completedSeq = inFlight.Seq
		completedRevision = inFlight.Revision
		copyBox.MarkCompleted(inFlight.Seq)
	}
	if pauseQueue {
		if _, _, err := copyBox.SetQueuePaused(true, 0); err != nil {
			return nil, nil, 0, 0, err
		}
	}
	snapshot, rows, _, _ := copyBox.PersistenceState()
	if snapshot == nil {
		return nil, nil, 0, 0, fmt.Errorf("encode completed input queue snapshot")
	}
	return snapshot, rows, completedSeq, completedRevision, nil
}

func terminalNeedsQueuePause(status turn.TurnStatus) bool {
	return status == turn.TurnStatusFailed || status == turn.TurnStatusInterrupted || status == turn.TurnStatusBudgetExhausted
}

// commitTerminalBatchAlreadyLocked is used where the caller already owns
// lifecycleMu (continuation budget fences and setup rollback cancellation).
// Its caller owns any specialized post-commit hook behavior.
func (r *runtime) commitTerminalBatchAlreadyLocked(commands []turn.TurnCommand) error {
	r.mu.Lock()
	history := append([]llm.Message(nil), r.messages...)
	if r.turnEpochCurrentLocked() && r.commitStepHistory(&history) {
		r.historyCommitDirty = true
	}
	r.mu.Unlock()
	for i := range commands {
		if commands[i].CommandID == "" {
			commands[i].CommandID = r.lifecycleNextCommandID()
		}
	}
	var persist func([]turn.TurnCommand, []turn.CoordinatorSnapshot) error
	if r.store != nil {
		persist = func(applied []turn.TurnCommand, projections []turn.CoordinatorSnapshot) error {
			return r.persistTerminalBatchLocked(history, applied, projections, "unreadable")
		}
	}
	snapshots, err := r.turnCoordinator.DispatchBatchDurable(commands, persist)
	if err != nil {
		r.markResultCommitFailed()
		return fmt.Errorf("commit terminal lifecycle boundary: %w", err)
	}
	for i, state := range snapshots {
		if r.onLifecycle != nil {
			if err := r.onLifecycle(r.session.ID, state); err != nil {
				return fmt.Errorf("turn lifecycle observer failed after budget commit: %w", err)
			}
		}
		r.publishTurnState(state, commands[i].Type)
	}
	return nil
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
	if r.onLifecycle != nil {
		if err := r.onLifecycle(r.session.ID, snapshot); err != nil {
			if r.logger != nil {
				r.logger.Warn("turn lifecycle observer failed", "session_id", r.session.ID, "turn_id", snapshot.TurnID, "error", err)
			}
			// The observer is the Goal runtime's durable Run binding and
			// settlement boundary. Continuing execution after it fails can
			// spend model/tool budget without a persisted Run projection.
			return snapshot, fmt.Errorf("turn lifecycle observer failed: %w", err)
		}
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
	scope := strings.TrimSpace(r.lifecycleCommandScope)
	r.mu.Unlock()
	if scope == "" {
		// Runtimes assembled directly by embedded callers and tests may bypass
		// newRuntimeWithPublisher. Keep their generated IDs collision-resistant
		// too, rather than falling back to the legacy session-local sequence.
		scope = newLifecycleCommandScope()
		r.mu.Lock()
		if r.lifecycleCommandScope == "" {
			r.lifecycleCommandScope = scope
		} else {
			scope = r.lifecycleCommandScope
		}
		r.mu.Unlock()
	}
	// A replacement runtime can briefly coexist with the prior instance while
	// it is hydrated and swapped in. Scope each counter so two instances that
	// resume from the same durable maximum cannot emit the same command ID.
	return fmt.Sprintf("%s-lifecycle-%s-%d", r.session.ID, scope, seq)
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
	event, ok, err := r.lifecycleEventEnvelope(command, snapshot)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var stored turn.TurnEventEnvelope
	if command.Type == turn.CommandStartTurn {
		r.deltaMu.Lock()
		roundID, revisionID := r.activeRoundID, r.activeRevisionID
		r.deltaMu.Unlock()
		if roundID != "" && revisionID != "" {
			revision := store.RoundRevision{ConversationID: r.session.ID, RoundID: roundID, RevisionID: revisionID, TurnID: snapshot.TurnID, Source: string(snapshot.Source), Generation: snapshot.Generation, Status: "active"}
			var storedEvents []turn.TurnEventEnvelope
			r.persistMu.Lock()
			runtimeState, queueRows := r.roundStartRuntimeState()
			storedEvents, err = r.store.AppendTurnStartBoundary(context.Background(), []turn.TurnEventEnvelope{event}, revision, runtimeState, queueRows)
			if err == nil && r.inputBox != nil {
				r.inputBox.MarkPersistenceCommitted(runtimeState.InputBoxState)
			}
			r.persistMu.Unlock()
			if err == nil && len(storedEvents) > 0 {
				stored = storedEvents[len(storedEvents)-1]
			}
		} else {
			stored, err = r.store.AppendTurnEvent(context.Background(), event)
		}
	} else if event.EventType == turn.EventTurnCompleted {
		if rebuilt, rebuildErr := r.store.RebuildTurnMessages(context.Background(), event.SessionID, event.TurnID); rebuildErr == nil && len(rebuilt) > 0 {
			stored, err = r.store.AppendTurnEventWithSnapshot(context.Background(), event, rebuilt)
		} else {
			stored, err = r.store.AppendTurnEventWithSnapshot(context.Background(), event, nil)
		}
	} else {
		stored, err = r.store.AppendTurnEvent(context.Background(), event)
	}
	if err != nil {
		return err
	}
	r.setLifecycleEventSequence(stored.SessionSeq)
	return nil
}

func (r *runtime) roundStartRuntimeState() (store.RuntimeState, []store.InputQueueRow) {
	r.mu.Lock()
	r.deltaMu.Lock()
	state := store.RuntimeState{
		HistoryRevision: r.historyRevision, ConversationRevision: r.conversationRevision,
		ContextCheckpointRevision: r.contextCheckpointRevision,
		ActiveRoundID:             r.activeRoundID, ActiveRevisionID: r.activeRevisionID,
		ToolCallProjectionHiddenThroughGeneration: r.toolCallProjectionHiddenThroughGeneration,
		ActiveContextStart:                        r.activeContextStart, LastContextResetID: r.lastContextResetID,
		LastHumanInteraction: r.lastHumanInteraction, NotifySeq: r.notifySeq, AckSeq: r.ackSeq,
	}
	if r.dreamingAttempt != nil {
		state.DreamingAttempt, _ = json.Marshal(*r.dreamingAttempt)
	}
	r.deltaMu.Unlock()
	r.mu.Unlock()
	if r.orch != nil {
		state.HookStore = hooks.CloneSessionStore(r.orch.HookStoreSnapshot())
	}
	if r.inputBox != nil {
		inputState, inputQueueRows, _, _ := r.inputBox.PersistenceState()
		state.InputBoxState = inputState
		return state, inputQueueRows
	}
	return state, nil
}

func (r *runtime) lifecycleEventEnvelope(command turn.TurnCommand, snapshot turn.CoordinatorSnapshot) (turn.TurnEventEnvelope, bool, error) {
	eventType, ok := lifecycleEventType(command.Type)
	if !ok || snapshot.TurnID == "" {
		return turn.TurnEventEnvelope{}, false, nil
	}
	r.deltaMu.Lock()
	roundID, revisionID := r.activeRoundID, r.activeRevisionID
	r.deltaMu.Unlock()
	payload, err := json.Marshal(map[string]any{
		"command_type":           command.Type,
		"generation":             snapshot.Generation,
		"round_id":               roundID,
		"revision_id":            revisionID,
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
		"attempt_id":             command.AttemptID,
		"assistant_message_id":   command.AssistantMessageID,
		"assistant_message":      command.AssistantMessage,
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
		"retryable":              command.Retryable,
		"max_attempts":           command.MaxAttempts,
		"retry_after_ms":         command.RetryAfterMS,
		"execution_status":       command.ExecutionStatus,
		"usage":                  command.Usage,
		"turn_status":            snapshot.TurnStatus,
		"turn_end_reason":        snapshot.TurnEndReason,
		"step_status":            snapshot.StepStatus,
		"step_end_reason":        snapshot.StepEndReason,
		"step_index":             snapshot.StepIndex,
		"context_epoch":          snapshot.ContextEpoch,
		"recovery_required":      snapshot.RecoveryRequired,
		"input_message":          r.lifecycleInputMessage(eventType),
	})
	if err != nil {
		return turn.TurnEventEnvelope{}, false, err
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
	if event.Source == "" {
		event.Source = string(snapshot.Source)
	}
	event.CommandID = command.CommandID
	event.Payload = payload
	event.PayloadRef = command.PayloadRef
	return event, true, nil
}

func (r *runtime) lifecycleInputMessage(eventType turn.EventType) any {
	if eventType != turn.EventTurnStarted || r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pendingInputMessage == nil {
		return nil
	}
	return r.pendingInputMessage
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
	start := r.activeContextStart
	if start < 0 || start > len(r.messages) {
		start = len(r.messages)
	}
	return len(r.messages) - start
}

func (r *runtime) lifecycleHistorySnapshot() []llm.Message {
	if r == nil {
		return nil
	}
	return r.activeMessagesSnapshot()
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

// ConditionApprovalRequest describes a durable, no-model condition approval.
// The trigger owns delivery/revision validation; the runtime owns the normal
// HITL lifecycle and Agent-bound tool execution.
type ConditionApprovalRequest struct {
	Metadata turn.ConditionApprovalMetadata
	Command  string
}

func (r *runtime) requestConditionApproval(req ConditionApprovalRequest) error {
	if r == nil || r.orch == nil {
		return fmt.Errorf("condition approval runtime unavailable")
	}
	if req.Metadata.AgentID != "" && req.Metadata.AgentID != r.agentID {
		return fmt.Errorf("condition approval agent mismatch")
	}
	if req.Metadata.SessionID != "" && req.Metadata.SessionID != r.session.ID {
		return fmt.Errorf("condition approval session mismatch")
	}
	pending := turn.BuildConditionApprovalPending(req.Metadata, req.Command)
	// Bind the durable approval to the exact tool arguments that were shown to
	// the user. ExecuteConditionApproval rechecks this digest before invoking
	// the shell, so a restored or tampered pending item cannot change the
	// command while retaining the original trigger identity.
	if len(pending.Items) == 1 && pending.Items[0].ConditionApproval != nil {
		pending.Items[0].ConditionApproval.ArgsDigest = turn.Digest(pending.Items[0].ToolCall.Function.Arguments)
	}
	payload, err := json.Marshal(pending)
	if err != nil {
		return fmt.Errorf("marshal condition approval: %w", err)
	}
	r.lifecycleMu.Lock()
	if err := r.lifecycleBeginInputTurnLocked(turn.TurnSourceSideEffect, newLogicalRoundID(), newRoundRevisionID()); err != nil {
		r.lifecycleMu.Unlock()
		return err
	}
	state := r.turnCoordinator.Snapshot()
	item := pending.Items[0]
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type: turn.CommandAssistantReceived, SessionID: r.session.ID, TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, HasTools: true,
		ToolBatchID: state.StepID + "-condition-batch", AssistantMessageID: item.ToolCall.ID + "-assistant",
		At: time.Now().UTC(), Reason: "condition_approval",
	}); err != nil {
		r.lifecycleCancelLocked()
		r.lifecycleMu.Unlock()
		return err
	}
	state = r.turnCoordinator.Snapshot()
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type: turn.CommandToolCallRecorded, SessionID: r.session.ID, TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, ToolCallID: item.ToolCall.ID,
		ToolExecutionID: item.ToolCall.ID + "-execution", ToolName: item.ToolCall.Function.Name,
		Arguments: json.RawMessage(item.ToolCall.Function.Arguments), At: time.Now().UTC(),
	}); err != nil {
		r.lifecycleCancelLocked()
		r.lifecycleMu.Unlock()
		return err
	}
	state = r.turnCoordinator.Snapshot()
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
		Type: turn.CommandInteractionRequested, SessionID: r.session.ID, TurnID: state.TurnID,
		StepID: state.StepID, Generation: state.Generation, InteractionID: state.InteractionID,
		InteractionKind: "approval", ToolExecutionID: item.ToolCall.ID + "-execution", Payload: payload,
		At: time.Now().UTC(), Reason: "condition_approval",
	}); err != nil {
		r.lifecycleCancelLocked()
		r.lifecycleMu.Unlock()
		return err
	}
	r.lifecycleMu.Unlock()
	r.orch.PublishPendingHITL(r.session.ID, pending)
	return nil
}

func (r *runtime) lifecycleBeginInputTurn(source turn.TurnSource) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	return r.lifecycleBeginInputTurnIdentity(source, newLogicalRoundID(), newRoundRevisionID())
}

func (r *runtime) lifecycleBeginInputTurnIdentity(source turn.TurnSource, roundID, revisionID string) error {
	return r.lifecycleBeginInputTurnIdentityWithResubmit(source, roundID, revisionID, nil, nil)
}

func (r *runtime) lifecycleBeginInputTurnIdentityWithResubmit(source turn.TurnSource, roundID, revisionID string, resubmit *queue.ConversationResubmitRequest, checkpoint *store.CheckpointCommit) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.lifecycleBeginInputTurnLockedWithResubmit(source, roundID, revisionID, resubmit, checkpoint)
}

func (r *runtime) lifecycleBeginInputTurnLocked(source turn.TurnSource, roundID, revisionID string) error {
	return r.lifecycleBeginInputTurnLockedWithResubmit(source, roundID, revisionID, nil, nil)
}

func (r *runtime) lifecycleBeginInputTurnLockedWithResubmit(source turn.TurnSource, roundID, revisionID string, resubmit *queue.ConversationResubmitRequest, checkpoint *store.CheckpointCommit) error {
	if r == nil || r.turnCoordinator == nil {
		return fmt.Errorf("turn coordinator is unavailable")
	}
	old := r.turnCoordinator.Snapshot()
	if old.HasActiveTurn && !old.TurnStatus.Terminal() {
		return fmt.Errorf("active turn must be cancelled before accepting a new human input")
	}
	if strings.TrimSpace(roundID) == "" || strings.TrimSpace(revisionID) == "" {
		return fmt.Errorf("round and revision identities are required")
	}
	r.deltaMu.Lock()
	r.activeRoundID, r.activeRevisionID = roundID, revisionID
	r.deltaMu.Unlock()

	turnID := newContinuationID()
	if r.budgetResolver != nil {
		budget, err := r.budgetResolver()
		if err != nil {
			r.deltaMu.Lock()
			r.activeRoundID, r.activeRevisionID = "", ""
			r.deltaMu.Unlock()
			return fmt.Errorf("refresh turn budget: %w", err)
		}
		r.turnBudget = budget
	}
	now := time.Now().UTC()
	generation := old.Generation + 1
	if generation == 0 {
		generation = 1
	}
	commands := []turn.TurnCommand{{
		Type:       turn.CommandStartTurn,
		SessionID:  r.session.ID,
		TurnID:     turnID,
		Generation: generation,
		Source:     source,
		Budget:     r.turnBudget,
		At:         now,
	}, {
		Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: turnID,
		StepID: lifecycleStepID(turnID, 1), Generation: generation, At: now,
	}}
	for i := range commands {
		commands[i].CommandID = r.lifecycleNextCommandID()
	}
	if _, err := r.lifecycleStartTurnBatchLocked(commands, roundID, revisionID, resubmit, checkpoint); err != nil {
		state := r.turnCoordinator.Snapshot()
		r.deltaMu.Lock()
		if !state.HasActiveTurn || state.TurnID != turnID {
			r.activeRoundID, r.activeRevisionID = "", ""
		}
		r.deltaMu.Unlock()
		return fmt.Errorf("start turn lifecycle: %w", err)
	}
	return nil
}

func (r *runtime) lifecycleStartTurnBatchLocked(commands []turn.TurnCommand, roundID, revisionID string, resubmit *queue.ConversationResubmitRequest, checkpoint *store.CheckpointCommit) ([]turn.CoordinatorSnapshot, error) {
	persist := func(applied []turn.TurnCommand, snapshots []turn.CoordinatorSnapshot) error {
		if r.store == nil {
			return nil
		}
		events := make([]turn.TurnEventEnvelope, 0, len(applied))
		for i, command := range applied {
			event, ok, err := r.lifecycleEventEnvelope(command, snapshots[i])
			if err != nil {
				return err
			}
			if ok {
				events = append(events, event)
			}
		}
		if len(events) != 2 || events[0].EventType != turn.EventTurnStarted || events[1].EventType != turn.EventStepStarted {
			return fmt.Errorf("round start batch must contain turn.started followed by step.started")
		}
		revision := store.RoundRevision{ConversationID: r.session.ID, RoundID: roundID, RevisionID: revisionID, TurnID: snapshots[len(snapshots)-1].TurnID, Source: string(snapshots[len(snapshots)-1].Source), Generation: snapshots[len(snapshots)-1].Generation, Status: "active"}
		if resubmit != nil {
			revision.ResubmitFromRevisionID = resubmit.ExpectedSelectedRevisionID
			revision.ExpectedConversationRevision = resubmit.ExpectedConversationRevision
			revision.ExpectedPreCheckpointRevision = resubmit.PreCheckpointRevision
			revision.ResubmitCheckpoint = checkpoint
		}
		r.persistMu.Lock()
		runtimeState, queueRows := r.roundStartRuntimeState()
		stored, err := r.store.AppendTurnStartBoundary(context.Background(), events, revision, runtimeState, queueRows)
		if err == nil && r.inputBox != nil {
			r.inputBox.MarkPersistenceCommitted(runtimeState.InputBoxState)
		}
		r.persistMu.Unlock()
		if err != nil {
			return err
		}
		if resubmit != nil && checkpoint != nil {
			r.mu.Lock()
			r.deltaMu.Lock()
			r.conversationRevision++
			r.contextCheckpointRevision++
			r.checkpointDirty = false
			r.checkpointFullReplaceSequence = 0
			r.checkpointMutations = nil
			r.checkpointMutationSequence++
			r.checkpointSyncedMutationSequence = r.checkpointMutationSequence
			r.historyFullReplaceSequence = r.checkpointMutationSequence
			r.historyMutations = nil
			r.deltaMu.Unlock()
			r.mu.Unlock()
		}
		if len(stored) > 0 {
			r.setLifecycleEventSequence(stored[len(stored)-1].SessionSeq)
		}
		return nil
	}
	snapshots, err := r.turnCoordinator.DispatchBatchDurable(commands, persist)
	if err != nil {
		return nil, err
	}
	for i, snapshot := range snapshots {
		if r.onLifecycle != nil {
			if err := r.onLifecycle(r.session.ID, snapshot); err != nil {
				return snapshots, fmt.Errorf("turn lifecycle observer failed after start commit: %w", err)
			}
		}
		r.publishTurnState(snapshot, commands[i].Type)
	}
	return snapshots, nil
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
	before := r.turnCoordinator.Snapshot()
	started, err := r.lifecycleBeginContinuationStepLocked(source)
	state := r.turnCoordinator.Snapshot()
	r.mu.Lock()
	historyRevision := r.historyRevision
	r.mu.Unlock()
	r.lifecycleMu.Unlock()
	if err == nil && !started && before.TurnStatus != turn.TurnStatusBudgetExhausted && state.TurnStatus == turn.TurnStatusBudgetExhausted && r.orch != nil {
		// Publish only after releasing lifecycleMu: terminal turn_state is
		// already visible, and turn.done hooks cannot re-enter this lock.
		r.orch.PublishBudgetExhausted(r.session.ID, state.TurnID, state.Generation, state.TurnEndReason, historyRevision)
	}
	return started, err
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
		r.deltaMu.Lock()
		r.activeRoundID, r.activeRevisionID = newLogicalRoundID(), newRoundRevisionID()
		r.deltaMu.Unlock()
		identity, _ = r.lifecycleEnsureIdentity()
		generation = state.Generation + 1
		if generation == 0 {
			generation = 1
		}
		if r.budgetResolver != nil {
			budget, err := r.budgetResolver()
			if err != nil {
				r.deltaMu.Lock()
				r.activeRoundID, r.activeRevisionID = "", ""
				r.deltaMu.Unlock()
				return false, fmt.Errorf("refresh turn budget: %w", err)
			}
			r.turnBudget = budget
		}
		now := time.Now().UTC()
		r.deltaMu.Lock()
		roundID, revisionID := r.activeRoundID, r.activeRevisionID
		r.deltaMu.Unlock()
		commands := []turn.TurnCommand{{
			Type: turn.CommandStartTurn, SessionID: r.session.ID, TurnID: identity,
			Generation: generation, Source: source, Budget: r.turnBudget, At: now, Reason: "side_effect_continuation",
		}, {
			Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: identity,
			StepID: lifecycleStepID(identity, 1), Generation: generation, At: now,
		}}
		for i := range commands {
			commands[i].CommandID = r.lifecycleNextCommandID()
		}
		if _, err := r.lifecycleStartTurnBatchLocked(commands, roundID, revisionID, nil, nil); err != nil {
			state := r.turnCoordinator.Snapshot()
			r.deltaMu.Lock()
			if !state.HasActiveTurn || state.TurnID != identity {
				r.activeRoundID, r.activeRevisionID = "", ""
			}
			r.deltaMu.Unlock()
			return false, fmt.Errorf("recover turn lifecycle: %w", err)
		}
		return true, nil
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
		decision := r.turnCoordinator.BudgetDecisionFor(turn.CommandStartStep)
		if !decision.Allowed {
			summaryAllowed := false
			if decision.Reason == "max_tool_calls" {
				summary := turn.TurnCommand{Type: turn.CommandStartStep, SessionID: r.session.ID, TurnID: identity, StepID: lifecycleStepID(identity, state.StepIndex+1), Generation: generation, FinalSummary: true}
				summaryAllowed = r.turnCoordinator.BudgetDecisionForCommand(summary).Allowed
			}
			if !summaryAllowed {
				now := time.Now().UTC()
				if err := r.commitTerminalBatchAlreadyLocked([]turn.TurnCommand{
					{Type: turn.CommandToolBatchSettled, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now},
					{Type: turn.CommandCompleteStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: "tool_batch_settled"},
					{Type: turn.CommandBudgetExhausted, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: decision.Reason},
				}); err != nil {
					return false, fmt.Errorf("record budget exhaustion lifecycle: %w", err)
				}
				return false, nil
			}
		}
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
		if decision.Reason == "max_tool_calls" {
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
		if err := r.withTerminalHistoryLocked(r.lifecycleHistorySnapshot(), []turn.TurnCommand{{
			Type: turn.CommandBudgetExhausted, SessionID: r.session.ID, TurnID: identity,
			Generation: generation, At: time.Now().UTC(), Reason: decision.Reason,
		}}, "unreadable"); err != nil {
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
	if strings.TrimSpace(assistant.RecordID) == "" && r.turnCoordinator != nil {
		assistant.RecordID = llm.AssistantMessageIDForStep(r.turnCoordinator.Snapshot().StepID)
	}
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
	currentStart := r.activeContextStart
	if currentStart < 0 || currentStart > len(r.messages) {
		currentStart = len(r.messages)
	}
	r.messages = append(append([]llm.Message(nil), r.messages[:currentStart]...), reordered...)
	r.noteRecordUpsertLocked(assistant)
	for _, message := range results {
		r.noteRecordUpsertLocked(message)
	}
	r.historyRevision++
	r.noteCheckpointDirtyLocked()
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
		if tools.ClassifyResult(call.Function.Name, result.Content, false).Denied() {
			status = turn.ToolExecutionStatusDenied
			errorKind = "tool_denied"
			if strings.Contains(strings.ToLower(result.Content), "user_rejected") {
				errorKind = "user_rejected"
			} else if strings.Contains(strings.ToLower(result.Content), "policy_denied") {
				errorKind = "policy_denied"
			}
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
				ResultContent:   result.Content,
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
			ResultContent:   result.Content,
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
	if err := r.commitHistoryForLifecycleLocked(history); err != nil {
		r.markResultCommitFailed()
		state := r.turnCoordinator.Snapshot()
		r.lifecycleMu.Unlock()
		r.publishTurnState(state, "")
		return fmt.Errorf("persist committed history: %w", err)
	}
	if err := callback(); err != nil {
		r.markResultCommitFailed()
		state := r.turnCoordinator.Snapshot()
		r.lifecycleMu.Unlock()
		r.publishTurnState(state, "")
		return err
	}
	state := r.turnCoordinator.Snapshot()
	r.mu.Lock()
	historyRevision := r.historyRevision
	r.mu.Unlock()
	r.lifecycleMu.Unlock()
	if state.TurnStatus.Terminal() && r.orch != nil {
		additional := map[string]any{
			"turn_id":          state.TurnID,
			"turn_generation":  state.Generation,
			"history_revision": historyRevision,
		}
		if state.TurnStatus == turn.TurnStatusBudgetExhausted {
			additional["budget_reason"] = state.TurnEndReason
		}
		if state.TurnEndReason == "auto_idle" {
			// Auto no-work completion historically runs turn.done after the
			// lifecycle commit. Keep that Hook boundary and count while the
			// result event itself remains strictly post-commit.
			r.orch.RunTurnDoneCommitted(r.session.ID, "stop")
			additional["no_work"] = true
		}
		r.orch.PublishCommittedTurnFinished(r.session.ID, terminalFinishReason(state), additional)
	}
	return nil
}

func (r *runtime) markResultCommitFailed() bool {
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	r.mu.Lock()
	changed := !r.executionStopped || r.resultCommitStatus != "failed"
	r.resultCommitStatus = "failed"
	r.executionStopped = true
	r.mu.Unlock()
	return changed
}

func (r *runtime) commitHistoryForLifecycleLocked(history []llm.Message) error {
	r.mu.Lock()
	if !r.turnEpochCurrentLocked() {
		r.mu.Unlock()
		return nil
	}
	changed := r.commitStepHistory(&history)
	if changed {
		r.historyCommitDirty = true
	}
	needsSave := r.historyCommitDirty
	r.mu.Unlock()
	if needsSave {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.persist(ctx); err != nil {
			return err
		}
		r.mu.Lock()
		r.historyCommitDirty = false
		r.resultCommitStatus = "committed"
		r.mu.Unlock()
	}
	return nil
}

func terminalFinishReason(state turn.CoordinatorSnapshot) string {
	if state.TurnStatus == turn.TurnStatusBudgetExhausted {
		return "budget_exhausted"
	}
	switch state.TurnStatus {
	case turn.TurnStatusCompleted:
		return "stop"
	case turn.TurnStatusCancelled, turn.TurnStatusInterrupted:
		return "cancelled"
	default:
		return "error"
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
		state := r.turnCoordinator.Snapshot()
		reason := outcome.Err.Error()
		if strings.TrimSpace(state.ModelErrorKind) != "" && strings.TrimSpace(state.StepEndReason) != "" {
			reason = state.StepEndReason
		}
		commands := make([]turn.TurnCommand, 0, 2)
		if errors.Is(outcome.Err, turn.ErrBudgetExhausted) {
			if !state.StepStatus.Terminal() {
				commands = append(commands, turn.TurnCommand{Type: turn.CommandFailStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: outcome.Err.Error()})
			}
			if !state.TurnStatus.Terminal() {
				commands = append(commands, turn.TurnCommand{Type: turn.CommandBudgetExhausted, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: outcome.Err.Error()})
			}
			return r.withTerminalHistoryLocked(history, commands, "unreadable")
		}
		stepType, turnType := turn.CommandFailStep, turn.CommandFailTurn
		if errors.Is(outcome.Err, context.Canceled) {
			stepType, turnType = turn.CommandInterruptStep, turn.CommandInterruptTurn
		}
		if !state.StepStatus.Terminal() {
			commands = append(commands, turn.TurnCommand{Type: stepType, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: reason})
		}
		if !state.TurnStatus.Terminal() {
			commands = append(commands, turn.TurnCommand{Type: turnType, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: reason})
		}
		return r.withTerminalHistoryLocked(history, commands, "unreadable")
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
	if outcome.NoWork {
		commands := make([]turn.TurnCommand, 0, 3)
		if state.StepStatus == turn.StepStatusExecutingTools {
			commands = append(commands, turn.TurnCommand{Type: turn.CommandToolBatchSettled, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: "auto_idle"})
		}
		if !state.StepStatus.Terminal() {
			commands = append(commands, turn.TurnCommand{Type: turn.CommandCompleteStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: "auto_idle"})
		}
		if !state.TurnStatus.Terminal() {
			commands = append(commands, turn.TurnCommand{Type: turn.CommandCompleteTurn, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: "auto_idle"})
		}
		return r.withTerminalHistoryLocked(history, commands, "no_work")
	}
	if hasAssistant && len(assistant.ToolCalls) > 0 {
		// Tool calls have only been proposed/accepted at this point. The Step
		// remains executing_tools until the inline continuation or a completed HITL
		// resume reaches lifecycleBeginContinuationStep/lifecycleAfterResume.
		return r.withCommittedHistoryLocked(history, func() error { return nil })
	}
	return r.withTerminalHistoryLocked(history, []turn.TurnCommand{
		{Type: turn.CommandCompleteStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now},
		{Type: turn.CommandCompleteTurn, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: "assistant_completed"},
	}, "readable")
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
	if outcome.ConditionHandled {
		return r.lifecycleAfterCondition(outcome, history, now)
	}
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
		state := r.turnCoordinator.Snapshot()
		reason := outcome.Err.Error()
		if strings.TrimSpace(state.ModelErrorKind) != "" && strings.TrimSpace(state.StepEndReason) != "" {
			reason = state.StepEndReason
		}
		commands := make([]turn.TurnCommand, 0, 2)
		if errors.Is(outcome.Err, turn.ErrBudgetExhausted) {
			if !state.StepStatus.Terminal() {
				commands = append(commands, turn.TurnCommand{Type: turn.CommandFailStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: outcome.Err.Error()})
			}
			if !state.TurnStatus.Terminal() {
				commands = append(commands, turn.TurnCommand{Type: turn.CommandBudgetExhausted, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: outcome.Err.Error()})
			}
			return r.withTerminalHistoryLocked(history, commands, "unreadable")
		}
		stepType, turnType := turn.CommandFailStep, turn.CommandFailTurn
		if errors.Is(outcome.Err, context.Canceled) {
			stepType, turnType = turn.CommandInterruptStep, turn.CommandInterruptTurn
		}
		if !state.StepStatus.Terminal() {
			commands = append(commands, turn.TurnCommand{Type: stepType, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: reason})
		}
		if !state.TurnStatus.Terminal() {
			commands = append(commands, turn.TurnCommand{Type: turnType, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: reason})
		}
		return r.withTerminalHistoryLocked(history, commands, "unreadable")
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
	return r.withTerminalHistoryLocked(history, []turn.TurnCommand{
		{Type: turn.CommandToolBatchSettled, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now},
		{Type: turn.CommandCompleteStep, SessionID: r.session.ID, TurnID: identity, StepID: state.StepID, Generation: generation, At: now, Reason: "interaction_resolved"},
		{Type: turn.CommandCompleteTurn, SessionID: r.session.ID, TurnID: identity, Generation: generation, At: now, Reason: "assistant_completed_after_interaction"},
	}, "readable")
}

func (r *runtime) lifecycleAfterCondition(outcome turn.StepOutcome, history []llm.Message, now time.Time) error {
	state := r.turnCoordinator.Snapshot()
	status := turn.ToolExecutionStatusSucceeded
	errorKind := ""
	if !outcome.ConditionMatched || outcome.Err != nil {
		status = turn.ToolExecutionStatusFailed
		if outcome.Err != nil {
			errorKind = "condition_execution_failed"
		} else {
			errorKind = "condition_rejected_or_false"
		}
	}
	commandType := turn.CommandToolExecutionCompleted
	if status != turn.ToolExecutionStatusSucceeded {
		commandType = turn.CommandToolExecutionFailed
	}
	return r.withTerminalHistoryLocked(history, []turn.TurnCommand{
		{Type: commandType, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, ToolCallID: outcome.ConditionToolCallID, ToolExecutionID: outcome.ConditionExecutionID, ExecutionStatus: status, ErrorKind: errorKind, ResultContent: outcome.ConditionResult, At: now},
		{Type: turn.CommandToolResultRecorded, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, ToolCallID: outcome.ConditionToolCallID, ToolExecutionID: outcome.ConditionExecutionID, ResultContent: outcome.ConditionResult, At: now},
		{Type: turn.CommandToolBatchSettled, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, At: now},
		{Type: turn.CommandCompleteStep, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, At: now, Reason: "condition_completed"},
		{Type: turn.CommandCompleteTurn, SessionID: r.session.ID, TurnID: state.TurnID, Generation: state.Generation, At: now, Reason: "condition_completed"},
	}, "readable")
}

func (r *runtime) lifecycleCancel() error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn {
		return nil
	}
	return r.commitTerminalBatchAlreadyLocked([]turn.TurnCommand{{Type: turn.CommandCancelTurn, SessionID: r.session.ID, TurnID: state.TurnID, Generation: state.Generation, At: time.Now().UTC(), Reason: "cancelled_by_user"}})
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
	if err := r.commitTerminalBatchAlreadyLocked([]turn.TurnCommand{{Type: turn.CommandCancelTurn, SessionID: r.session.ID, TurnID: state.TurnID, Generation: state.Generation, At: now, Reason: "cancelled_by_user"}}); err != nil {
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	return r.lifecycleContextCompactedLocked(reason, beforeDigest, afterDigest, beforeCount, afterCount)
}

// lifecycleContextCompactedLocked records the durable history compaction
// boundary while the caller already holds lifecycleMu. This lets manual
// compression serialize its checkpoint commit and lifecycle fact without
// exposing a committed lifecycle event for an uncommitted checkpoint.
func (r *runtime) lifecycleContextCompactedLocked(reason, beforeDigest, afterDigest string, beforeCount, afterCount int) error {
	if r == nil || r.turnCoordinator == nil {
		return nil
	}
	identity, generation := r.lifecycleIdentity()
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.StepID == "" ||
		(state.StepStatus != turn.StepStatusRequesting && state.StepStatus != turn.StepStatusWaitingInteraction) {
		return nil
	}
	if _, err := r.lifecycleDispatchLockedErr(turn.TurnCommand{
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
	if lifecycle := r.turnCoordinator.Snapshot(); lifecycle.TurnID != "" && lifecycle.StepID != "" {
		reconciledMessage.RecordID = llm.ToolResultRecordIDForExecution(toolCallID, lifecycle.TurnID+"/"+lifecycle.StepID)
		reconciledMessage.ToolCallSourceID = llm.AssistantMessageIDForStep(lifecycle.StepID)
	}
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
			r.noteRecordUpsertLocked(reconciledMessage)
			r.noteCheckpointDirtyLocked()
		}
		break
	}
	if !resultExists {
		r.messages = append(r.messages, reconciledMessage)
		r.historyRevision++
		r.noteRecordUpsertLocked(reconciledMessage)
		r.noteCheckpointDirtyLocked()
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
