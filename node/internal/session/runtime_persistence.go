package session

import (
	"context"
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// persist writes the compatibility runtime snapshot. The lifecycle event log
// remains the turn authority; this snapshot is the durable transcript and
// mailbox checkpoint used by hydrate and crash recovery.
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
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	idleMarked := r.idleAutoCompressApplied
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	historyRevision := r.historyRevision
	r.mu.Unlock()
	var inputBoxState json.RawMessage
	if r.inputBox != nil {
		inputBoxState = r.inputBox.Snapshot()
	}
	var hookStore map[string]json.RawMessage
	if r.orch != nil {
		hookStore = hooks.CloneSessionStore(r.orch.HookStoreSnapshot())
	}
	pending := r.pendingSnapshot()
	stepCount := r.stepIndexSnapshot()
	err := r.store.Save(ctx, store.Record{
		AgentID:      r.session.ID,
		NodeID:       r.session.AgentID,
		Messages:     msgs,
		LoadedSkills: loaded,
		RuntimeState: store.RuntimeState{
			Pending:                 pending,
			ToolLoopCount:           stepCount,
			InputBoxState:           inputBoxState,
			HistoryRevision:         historyRevision,
			HookStore:               hookStore,
			IdleAutoCompressApplied: idleMarked,
			NotifySeq:               notifySeq,
			AckSeq:                  ackSeq,
		},
	})
	if err != nil && r.logger != nil {
		r.logger.Warn("persist runtime snapshot failed", "session_id", r.session.ID, "error", err)
	}
	return err
}

func (r *runtime) restoreInputBoxState(raw json.RawMessage) {
	if r == nil || r.inputBox == nil || len(raw) == 0 {
		return
	}
	if err := r.inputBox.Restore(raw); err != nil && r.logger != nil {
		r.logger.Warn("restore input box state failed", "session_id", r.session.ID, "error", err)
	}
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
	state := r.turnCoordinator.Snapshot()
	if state.HasActiveTurn && !state.TurnStatus.Terminal() {
		if userMsg, err := r.buildInputUserMessage(record.Env); err == nil {
			r.mu.Lock()
			if !r.historyHasUserMessageLocked(userMsg) {
				r.messages = append(r.messages, userMsg)
				r.historyRevision++
			}
			r.mu.Unlock()
		} else if r.logger != nil {
			r.logger.Warn("restore in-flight input message failed", "session_id", r.session.ID, "seq", record.Seq, "error", err)
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

func (r *runtime) historyHasUserMessageLocked(target llm.Message) bool {
	for index := len(r.messages) - 1; index >= 0; index-- {
		message := r.messages[index]
		if message.Role != "user" {
			continue
		}
		return llm.MessageTextSummary(message) == llm.MessageTextSummary(target) &&
			llm.NormalizeUserMessageName(message.Name) == llm.NormalizeUserMessageName(target.Name)
	}
	return false
}

// replacementData returns the in-memory state needed when a manager without
// a persistence store replaces a runtime. Production managers normally load
// this state from SQLite after persist; keeping this fallback prevents tests
// and embedded callers from losing history during a swap.
func (r *runtime) replacementData() ([]llm.Message, []skills.LoadedSkill, *turn.PendingHITL, int, map[string]json.RawMessage, bool, int, int, uint64, json.RawMessage) {
	if r == nil {
		return nil, nil, nil, 0, nil, false, 0, 0, 0, nil
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	idleMarked := r.idleAutoCompressApplied
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	historyRevision := r.historyRevision
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	stepCount := r.stepIndexSnapshot()
	var hookStore map[string]json.RawMessage
	if r.orch != nil {
		hookStore = r.orch.HookStoreSnapshot()
	}
	var inputBoxState json.RawMessage
	if r.inputBox != nil {
		inputBoxState = r.inputBox.Snapshot()
	}
	return msgs, loaded, pending, stepCount, hookStore, idleMarked, notifySeq, ackSeq, historyRevision, inputBoxState
}
