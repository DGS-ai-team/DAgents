package session

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type pendingRecordMutation struct {
	Message  llm.Message
	Sequence uint64
	Owner    store.RecordOwnership
}

type pendingCheckpointMutation struct {
	Operation store.CheckpointOperation
	Sequence  uint64
}

func (r *runtime) trackHistoryMutation(kind turn.HistoryMutationKind, index int, message llm.Message) llm.Message {
	if strings.EqualFold(strings.TrimSpace(message.Role), "tool") && strings.TrimSpace(message.ToolCallID) != "" && r.turnCoordinator != nil {
		lifecycle := r.turnCoordinator.Snapshot()
		if lifecycle.TurnID != "" && lifecycle.StepID != "" {
			message.RecordID = llm.ToolResultRecordIDForExecution(message.ToolCallID, lifecycle.TurnID+"/"+lifecycle.StepID)
			if strings.TrimSpace(message.ToolCallSourceID) == "" {
				message.ToolCallSourceID = llm.AssistantMessageIDForStep(lifecycle.StepID)
			}
		}
	}
	r.deltaMu.Lock()
	defer r.deltaMu.Unlock()
	if r.historyMutationTurnEpoch != r.historyMutationSessionEpoch {
		// The in-flight model/tool step crossed a clear boundary. Its local
		// history is discarded by the caller's epoch fence; do not let its
		// late sink callback repopulate the durable transcript delta.
		return message
	}
	id := llm.StoredRecordID(message)
	if id == "" {
		var err error
		id, err = llm.NewOpaqueRecordID()
		if err != nil {
			// Keep the message unidentifiable; the explicit delta commit will fail
			// closed instead of silently deriving identity from content or position.
			r.historyMutationErr = err
			return message
		}
		message.RecordID = id
	}
	if r.recordDeltaUpserts == nil {
		r.recordDeltaUpserts = make(map[string]pendingRecordMutation)
	}
	if r.recordDeltaDeletes == nil {
		r.recordDeltaDeletes = make(map[string]uint64)
	}
	r.recordMutationSequence++
	delete(r.recordDeltaDeletes, id)
	r.recordDeltaUpserts[id] = pendingRecordMutation{Message: message, Sequence: r.recordMutationSequence, Owner: store.RecordOwnership{RoundID: r.activeRoundID, RevisionID: r.activeRevisionID}}
	r.checkpointDirty = true
	r.checkpointMutationSequence++
	mutation := pendingCheckpointMutation{
		Operation: store.CheckpointOperation{Kind: string(kind), Index: index, Message: message},
		Sequence:  r.checkpointMutationSequence,
	}
	r.checkpointMutations = append(r.checkpointMutations, mutation)
	r.historyMutations = append(r.historyMutations, mutation)
	return message
}

func (r *runtime) noteRecordUpsertLocked(message llm.Message) {
	id := llm.StoredRecordID(message)
	if id == "" {
		r.deltaMu.Lock()
		if r.historyMutationErr == nil {
			r.historyMutationErr = fmt.Errorf("history mutation lacks source identity for role %s", message.Role)
		}
		r.deltaMu.Unlock()
		return
	}
	r.deltaMu.Lock()
	defer r.deltaMu.Unlock()
	if r.recordDeltaUpserts == nil {
		r.recordDeltaUpserts = make(map[string]pendingRecordMutation)
	}
	if r.recordDeltaDeletes == nil {
		r.recordDeltaDeletes = make(map[string]uint64)
	}
	r.recordMutationSequence++
	delete(r.recordDeltaDeletes, id)
	r.recordDeltaUpserts[id] = pendingRecordMutation{Message: message, Sequence: r.recordMutationSequence, Owner: store.RecordOwnership{RoundID: r.activeRoundID, RevisionID: r.activeRevisionID}}
}

func (r *runtime) noteCheckpointDirtyLocked() {
	r.deltaMu.Lock()
	defer r.deltaMu.Unlock()
	r.checkpointMutationSequence++
	r.checkpointFullReplaceSequence = r.checkpointMutationSequence
	r.historyFullReplaceSequence = r.checkpointMutationSequence
	r.checkpointDirty = true
}

// noteCheckpointMutationPendingLocked marks a checkpoint as dirty while the
// corresponding ordered history operations are still being produced by the
// orchestrator. It must not force a full replacement: the later history sink
// records the exact append/insert/replace operations for this turn.
func (r *runtime) noteCheckpointMutationPendingLocked() {
	r.deltaMu.Lock()
	defer r.deltaMu.Unlock()
	r.checkpointMutationSequence++
	r.checkpointDirty = true
}

func buildCheckpointCommit(messages []llm.Message, activeStart int, lineage string, checkpointRevision, checkpointSequence, fullReplaceSequence uint64, mutations []pendingCheckpointMutation) *store.CheckpointCommit {
	if activeStart < 0 || activeStart > len(messages) {
		activeStart = 0
	}
	commit := &store.CheckpointCommit{Lineage: lineage}
	forceSnapshot := fullReplaceSequence > 0 || checkpointRevision == 0 || (checkpointRevision+1)%64 == 0
	if !forceSnapshot {
		operations := make([]store.CheckpointOperation, 0, len(mutations))
		for _, mutation := range mutations {
			if mutation.Sequence <= checkpointSequence {
				operations = append(operations, mutation.Operation)
			}
		}
		if len(operations) > 0 {
			commit.Delta = &store.CheckpointDelta{BaseRevision: checkpointRevision, Operations: operations}
		}
	}
	if commit.Delta == nil {
		commit.Messages = append([]llm.Message(nil), messages[activeStart:]...)
	}
	return commit
}

// persist writes runtime metadata plus explicit transcript and input-queue
// deltas. The lifecycle event log remains the turn authority; the runtime row
// carries only input queue watermarks, while queue payloads are indexed rows.
//
// Callers that cross a durability boundary should check the returned error.
// Background best-effort callers may intentionally ignore it, but making the
// failure observable keeps persistence errors from being silently discarded by
// the runtime layer.
func (r *runtime) persist(ctx context.Context) error {
	if r.store == nil || r.isChildSession() {
		return nil
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	return r.persistLocked(ctx)
}

// persistLocked writes one runtime snapshot while the caller owns persistMu.
// It is used by compound durability boundaries that must roll back memory
// before another snapshot writer can publish the tentative state.
func (r *runtime) persistLocked(ctx context.Context) error {
	return r.persistLockedWithInputReceipt(ctx, nil)
}

func (r *runtime) persistLockedWithInputReceipt(ctx context.Context, acceptedReceipt *store.InputReceipt) error {
	return r.persistLockedWithInputReceiptAndResubmit(ctx, acceptedReceipt, nil)
}

func (r *runtime) persistLockedWithInputReceiptAndResubmit(ctx context.Context, acceptedReceipt *store.InputReceipt, resubmitGuard *store.ConversationResubmitGuard) error {
	r.mu.Lock()
	r.deltaMu.Lock()
	if r.historyMutationErr != nil {
		err := r.historyMutationErr
		r.deltaMu.Unlock()
		r.mu.Unlock()
		return fmt.Errorf("allocate persisted history identity: %w", err)
	}
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	historyRevision := r.historyRevision
	conversationRevision := r.conversationRevision
	checkpointRevision := r.contextCheckpointRevision
	checkpointDirty := r.checkpointDirty
	checkpointSequence := r.checkpointMutationSequence
	checkpointFullReplaceSequence := r.checkpointFullReplaceSequence
	checkpointMutations := append([]pendingCheckpointMutation(nil), r.checkpointMutations...)
	upserts := make([]llm.Message, 0, len(r.recordDeltaUpserts))
	recordOwners := make(map[string]store.RecordOwnership, len(r.recordDeltaUpserts))
	upsertSequences := make(map[string]uint64, len(r.recordDeltaUpserts))
	type sequencedMessage struct {
		id      string
		pending pendingRecordMutation
	}
	orderedUpserts := make([]sequencedMessage, 0, len(r.recordDeltaUpserts))
	for id, pending := range r.recordDeltaUpserts {
		if strings.TrimSpace(pending.Message.RecordID) == "" {
			r.deltaMu.Unlock()
			r.mu.Unlock()
			return fmt.Errorf("record identity required for pending mutation %s", id)
		}
		orderedUpserts = append(orderedUpserts, sequencedMessage{id: id, pending: pending})
	}
	sort.Slice(orderedUpserts, func(i, j int) bool { return orderedUpserts[i].pending.Sequence < orderedUpserts[j].pending.Sequence })
	for _, entry := range orderedUpserts {
		upserts = append(upserts, entry.pending.Message)
		upsertSequences[entry.id] = entry.pending.Sequence
		if entry.pending.Owner.RoundID != "" {
			recordOwners[entry.id] = entry.pending.Owner
		}
	}
	deleteIDs := make([]string, 0, len(r.recordDeltaDeletes))
	deleteSequences := make(map[string]uint64, len(r.recordDeltaDeletes))
	for id, sequence := range r.recordDeltaDeletes {
		deleteIDs = append(deleteIDs, id)
		deleteSequences[id] = sequence
	}
	var checkpoint *store.CheckpointCommit
	if checkpointDirty {
		start := r.activeContextStart
		if start < 0 || start > len(r.messages) {
			start = 0
		}
		lineage := "live_append"
		if start > 0 || r.lastContextResetID != "" || r.dreamingAttempt != nil {
			lineage = "derived_checkpoint"
		}
		checkpoint = buildCheckpointCommit(r.messages, start, lineage, checkpointRevision, checkpointSequence, checkpointFullReplaceSequence, checkpointMutations)
	}
	activeRoundID, activeRevisionID := r.activeRoundID, r.activeRevisionID
	r.deltaMu.Unlock()
	toolCallProjectionHiddenThroughGeneration := r.toolCallProjectionHiddenThroughGeneration
	activeContextStart := r.activeContextStart
	lastContextResetID := r.lastContextResetID
	lastHumanInteraction := r.lastHumanInteraction
	var dreamingAttempt json.RawMessage
	if r.dreamingAttempt != nil {
		dreamingAttempt, _ = json.Marshal(*r.dreamingAttempt)
	}
	r.mu.Unlock()
	var inputBoxState json.RawMessage
	var inputQueueRows []store.InputQueueRow
	var completedInputSeq, completedInputRevision uint64
	if r.inputBox != nil {
		inputBoxState, inputQueueRows, completedInputSeq, completedInputRevision = r.inputBox.PersistenceState()
	}
	var hookStore map[string]json.RawMessage
	if r.orch != nil {
		hookStore = hooks.CloneSessionStore(r.orch.HookStoreSnapshot())
	}
	firstUser := ""
	for _, message := range upserts {
		if message.Role == "user" {
			firstUser = llm.MessageTextSummary(message)
			if firstUser == "" && llm.MessageHasImages(message) {
				firstUser = "[image]"
			}
			if firstUser != "" {
				break
			}
		}
	}
	rec := store.Record{
		AgentID:                r.session.ID,
		NodeID:                 r.session.AgentID,
		FirstUserMessage:       firstUser,
		LoadedSkills:           loaded,
		AcceptedInputReceipt:   acceptedReceipt,
		ResubmitGuard:          resubmitGuard,
		InputQueueRows:         inputQueueRows,
		CompletedInputSeq:      completedInputSeq,
		CompletedInputRevision: completedInputRevision,
		RuntimeState: store.RuntimeState{
			InputBoxState:                             inputBoxState,
			HistoryRevision:                           historyRevision,
			ConversationRevision:                      conversationRevision,
			ActiveRoundID:                             activeRoundID,
			ActiveRevisionID:                          activeRevisionID,
			ContextCheckpointRevision:                 checkpointRevision,
			ToolCallProjectionHiddenThroughGeneration: toolCallProjectionHiddenThroughGeneration,
			ActiveContextStart:                        activeContextStart,
			LastContextResetID:                        lastContextResetID,
			LastHumanInteraction:                      lastHumanInteraction,
			DreamingAttempt:                           dreamingAttempt,
			HookStore:                                 hookStore,
			NotifySeq:                                 notifySeq,
			AckSeq:                                    ackSeq,
		},
	}
	var delta *store.ConversationDelta
	if len(upserts) > 0 || len(deleteIDs) > 0 || checkpoint != nil {
		delta = &store.ConversationDelta{ConversationID: r.session.ID, NodeID: r.session.AgentID, ExpectedRevision: conversationRevision, HistoryRevision: historyRevision, Upserts: upserts, RecordOwners: recordOwners, DeleteIDs: deleteIDs, Checkpoint: checkpoint}
		// The active revision is created atomically with AppendTurnStartBoundary
		// and finalized by the terminal transaction. Ordinary persistence must
		// not acquire the coordinator lock while holding persistMu: terminal
		// persistence takes those locks in the opposite direction.
	}
	result, err := r.store.PersistRuntimeDelta(ctx, rec, delta)
	if err != nil && r.logger != nil {
		r.logger.Warn("persist runtime snapshot failed", "session_id", r.session.ID, "error", err)
	}
	if err == nil {
		if r.inputBox != nil {
			r.inputBox.MarkPersistenceCommitted(inputBoxState)
		}
		r.mu.Lock()
		r.deltaMu.Lock()
		r.conversationRevision = result.ConversationRevision
		r.contextCheckpointRevision = result.CheckpointRevision
		for id, sequence := range upsertSequences {
			if current, ok := r.recordDeltaUpserts[id]; ok && current.Sequence == sequence {
				delete(r.recordDeltaUpserts, id)
			}
		}
		for id, sequence := range deleteSequences {
			if current, ok := r.recordDeltaDeletes[id]; ok && current == sequence {
				delete(r.recordDeltaDeletes, id)
			}
		}
		if r.historyRevision == historyRevision && len(r.recordDeltaUpserts) == 0 && len(r.recordDeltaDeletes) == 0 {
			// A non-terminal snapshot may durably commit a step's history before
			// the lifecycle terminal batch. Clear the fallback dirty bit only when
			// no newer transcript mutation arrived during that transaction.
			r.historyCommitDirty = false
		}
		if checkpoint != nil {
			remaining := r.checkpointMutations[:0]
			for _, mutation := range r.checkpointMutations {
				if mutation.Sequence > checkpointSequence {
					remaining = append(remaining, mutation)
				}
			}
			r.checkpointMutations = remaining
			if r.checkpointFullReplaceSequence != 0 && r.checkpointFullReplaceSequence <= checkpointSequence {
				r.checkpointFullReplaceSequence = 0
			}
			if r.historyFullReplaceSequence != 0 && r.historyFullReplaceSequence <= checkpointSequence {
				r.historyFullReplaceSequence = 0
			}
			r.checkpointDirty = r.checkpointFullReplaceSequence != 0 || len(r.checkpointMutations) > 0
		}
		r.deltaMu.Unlock()
		r.mu.Unlock()
		if delta != nil && (result.ConversationRevision > conversationRevision || result.CheckpointRevision > checkpointRevision) {
			r.publishConversationPatch(result, historyRevision, "committed", nil, "", "", inputBoxState, inputQueueRows)
		}
	}
	return err
}

func (r *runtime) restoreInputBoxState(raw json.RawMessage) error {
	if r == nil || r.inputBox == nil {
		return fmt.Errorf("input box unavailable")
	}
	var header inputBoxState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &header); err != nil {
			return fmt.Errorf("decode input box state: %w", err)
		}
	}
	if r.store != nil && header.QueueStorageVersion == 1 {
		rows, err := r.store.LoadInputQueue(context.Background(), r.session.ID)
		if err != nil {
			return fmt.Errorf("load persisted input queue: %w", err)
		}
		if err := r.inputBox.RestoreQueueRows(raw, rows); err != nil {
			return fmt.Errorf("restore persisted input queue: %w", err)
		}
	} else {
		if len(raw) > 0 {
			if err := r.inputBox.Restore(raw); err != nil {
				return fmt.Errorf("restore input box state: %w", err)
			}
		}
	}
	if r.store != nil && header.QueueStorageVersion != 1 && len(raw) > 0 {
		legacyReceipts, err := InputReceiptsFromSnapshot(raw)
		if err != nil {
			return err
		}
		inputState, rows, completedSeq, completedRevision := r.inputBox.PersistenceState()
		if inputState == nil {
			return fmt.Errorf("encode migrated input queue state")
		}
		if err := r.store.MigrateLegacyInputState(context.Background(), r.session.ID, inputState, rows, legacyReceipts, completedSeq, completedRevision); err != nil {
			return fmt.Errorf("migrate legacy input queue: %w", err)
		}
		r.inputBox.MarkPersistenceCommitted(inputState)
	}
	// SQLite is authoritative for durable receipts. Avoid loading the whole
	// receipt history into every runtime; lookups use the indexed table.
	r.inputBox.ClearReceiptCache()
	// The mailbox snapshot is the durable conversation-generation boundary.
	// Restore it before starting the consumer so late POST retries from before a
	// clear cannot be mistaken for inputs to the new context.
	epoch := r.inputBox.ConversationEpoch()
	if epoch > 0 {
		r.mu.Lock()
		r.sessionEpoch = epoch
		r.mu.Unlock()
		r.deltaMu.Lock()
		r.historyMutationSessionEpoch = epoch
		r.historyMutationTurnEpoch = epoch
		r.deltaMu.Unlock()
	}
	return nil
}

// reconcileRestoredInputBox closes the crash window around Pop. An input that
// was already driving a live Turn is represented by the lifecycle projection,
// so it must not be replayed after its recovered continuation. Its user
// message may not have reached the history snapshot before the process
// stopped, therefore restore it before dropping the in-flight marker.
func (r *runtime) reconcileRestoredInputBox() {
	if r == nil || r.inputBox == nil || r.turnCoordinator == nil {
		return
	}
	record, ok := r.inputBox.InFlight()
	if !ok {
		return
	}
	if (record.Kind == InputKindTrigger || record.Kind == InputKindSystemAuto) && (record.RecoveredLegacy || strings.TrimSpace(record.Env.DeliveryID) != "") {
		if record.RecoveredLegacy {
			if state := r.turnCoordinator.Snapshot(); state.HasActiveTurn && !state.TurnStatus.Terminal() {
				_ = r.lifecycleCancel()
			}
			r.inputBox.MarkCompleted(record.Seq)
			_ = r.persist(context.Background())
			r.inputBox.Ack(record.Seq)
			_ = r.persist(context.Background())
			return
		}
		if identity, ok := r.triggerDelivery.(triggers.DeliveryIdentityTracker); ok && !identity.IsPendingDelivery(strings.TrimSpace(record.Env.TriggerID), strings.TrimSpace(record.Env.DeliveryID)) {
			// An explicit recovery or a superseding claim fenced this mailbox
			// item. Do not restore its user message or continue its old Turn.
			if state := r.turnCoordinator.Snapshot(); state.HasActiveTurn && !state.TurnStatus.Terminal() {
				_ = r.lifecycleCancel()
			}
			r.inputBox.MarkCompleted(record.Seq)
			_ = r.persist(context.Background())
			r.inputBox.Ack(record.Seq)
			_ = r.persist(context.Background())
			return
		}
	}
	state := r.turnCoordinator.Snapshot()
	if state.HasActiveTurn && !state.TurnStatus.Terminal() {
		if userMsg, err := r.buildInputUserMessage(record.Env); err == nil {
			userMsg.RecordID = llm.InputRecordID(r.session.ID, record.Seq)
			userMsg.ClientRequestID = record.ClientRequestID
			r.mu.Lock()
			if !r.historyHasUserRecordLocked(userMsg) {
				r.messages = append(r.messages, userMsg)
				r.historyRevision++
				r.noteRecordUpsertLocked(userMsg)
				r.noteCheckpointDirtyLocked()
			}
			r.mu.Unlock()
		} else if r.logger != nil {
			r.logger.Warn("restore in-flight input message failed", "session_id", r.session.ID, "seq", record.Seq, "error", err)
		}
		// restoreLifecycleEvents runs during construction, before the InputBox
		// in-flight record is reconciled. Re-run the insertion after restoring the
		// user message so a recovered assistant batch cannot remain before it.
		if calls := r.activeToolCallsFromLifecycle(); len(calls) > 0 {
			r.restoreActiveToolCallMessage(calls)
		}
		// The active lifecycle Turn is authoritative. Mark this input complete
		// before acknowledging it so history and ownership are persisted in the
		// same snapshot boundary.
		r.inputBox.MarkCompleted(record.Seq)
		r.persist(context.Background())
		r.inputBox.Ack(record.Seq)
		r.persist(context.Background())
		return
	}
	if record.Completed {
		// A completed marker means the result history was persisted before the
		// final acknowledgement. Discard it instead of starting a duplicate
		// Turn after restart.
		r.inputBox.Ack(record.Seq)
		r.persist(context.Background())
		return
	}
	// The process stopped after Pop but before a Turn became durable. Replay
	// the input at the head of the FIFO, preserving its original sequence.
	r.inputBox.RequeueInFlight()
	r.persist(context.Background())
}

func (r *runtime) historyHasUserRecordLocked(target llm.Message) bool {
	targetID := strings.TrimSpace(target.RecordID)
	if targetID == "" {
		return false
	}
	start := r.activeContextStart
	if start < 0 || start > len(r.messages) {
		start = len(r.messages)
	}
	for index := len(r.messages) - 1; index >= start; index-- {
		message := r.messages[index]
		if message.Role == "user" && strings.TrimSpace(message.RecordID) == targetID {
			return true
		}
	}
	return false
}

// replacementData returns the in-memory state needed when a manager without
// a persistence store replaces a runtime. Production managers normally load
// this state from SQLite after persist; keeping this fallback prevents tests
// and embedded callers from losing history during a swap.
type runtimeReplacementData struct {
	Messages             []llm.Message
	LoadedSkills         []skills.LoadedSkill
	HookStore            map[string]json.RawMessage
	NotifySeq            int
	AckSeq               int
	HistoryRevision      uint64
	ActiveContextStart   int
	LastContextResetID   string
	LastHumanInteraction time.Time
	DreamingAttempt      json.RawMessage
	InputBoxState        json.RawMessage
}

func (r *runtime) replacementData() runtimeReplacementData {
	if r == nil {
		return runtimeReplacementData{}
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	historyRevision := r.historyRevision
	activeContextStart := r.activeContextStart
	lastContextResetID := r.lastContextResetID
	lastHumanInteraction := r.lastHumanInteraction
	var dreamingAttempt json.RawMessage
	if r.dreamingAttempt != nil {
		dreamingAttempt, _ = json.Marshal(*r.dreamingAttempt)
	}
	r.mu.Unlock()
	var hookStore map[string]json.RawMessage
	if r.orch != nil {
		hookStore = r.orch.HookStoreSnapshot()
	}
	var inputBoxState json.RawMessage
	if r.inputBox != nil {
		inputBoxState = r.inputBox.SnapshotForPersistence()
	}
	return runtimeReplacementData{
		Messages: msgs, LoadedSkills: loaded, HookStore: hookStore,
		NotifySeq: notifySeq, AckSeq: ackSeq,
		HistoryRevision: historyRevision, ActiveContextStart: activeContextStart, LastContextResetID: lastContextResetID, InputBoxState: inputBoxState, DreamingAttempt: dreamingAttempt,
		LastHumanInteraction: lastHumanInteraction,
	}
}
