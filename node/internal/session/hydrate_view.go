package session

import (
	"context"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// HydrateView 为 GET /v1/sessions/{id}/hydrate 的聚合视图。
type HydrateView struct {
	SessionID     string
	Transcript    []TranscriptEntry
	PendingHITL   map[string]any
	RunTurnPhase  string
	HasActiveTurn bool
	QueuePending  int
	NotifySeq     int
	AckSeq        int
	HasUnread     bool
}

// GetHydrateView 返回 session hydrate 快照（活跃 runtime 或 DB 持久化）。
func (m *Manager) GetHydrateView(sessionID string) (*HydrateView, error) {
	rt := m.getRuntime(sessionID)
	if rt != nil {
		return rt.hydrateView(), nil
	}
	if m.store == nil {
		return nil, fmt.Errorf("session_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("session_not_found")
	}
	pending := rec.RuntimeState.Pending
	hasActiveTurn := pending != nil
	state := turn.StateIdle
	if pending != nil {
		state = turn.StateAwaitingTool
	}
	return buildHydrateView(sessionID, rec.Messages, pending, state, 0, hasActiveTurn, rec.RuntimeState.NotifySeq, rec.RuntimeState.AckSeq), nil
}

func (r *runtime) hydrateView() *HydrateView {
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	pending := r.pending
	state := r.state
	queuePending := r.queue.Len()
	hasActiveTurn := r.state != turn.StateIdle || r.pending != nil
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	r.mu.Unlock()
	return buildHydrateView(r.session.ID, msgs, pending, state, queuePending, hasActiveTurn, notifySeq, ackSeq)
}

func buildHydrateView(
	sessionID string,
	messages []llm.Message,
	pending *turn.PendingHITL,
	state turn.State,
	queuePending int,
	hasActiveTurn bool,
	notifySeq int,
	ackSeq int,
) *HydrateView {
	transcript := MessagesToTranscriptEntries(messages)
	if transcript == nil {
		transcript = []TranscriptEntry{}
	}
	return &HydrateView{
		SessionID:     sessionID,
		Transcript:    transcript,
		PendingHITL:   turn.BuildHITLRequiredSnapshot(pending),
		RunTurnPhase:  hydrateRunTurnPhase(messages, pending, state, hasActiveTurn),
		HasActiveTurn: hasActiveTurn,
		QueuePending:  queuePending,
		NotifySeq:     notifySeq,
		AckSeq:        ackSeq,
		HasUnread:     notifySeq > ackSeq,
	}
}

func hydrateRunTurnPhase(messages []llm.Message, pending *turn.PendingHITL, state turn.State, hasActiveTurn bool) string {
	if pending != nil {
		return string(turn.TaskPhaseAwaitingHITL)
	}
	if hasActiveTurn && state != turn.StateIdle {
		return turn.RunTurnPhase(state)
	}
	return string(turn.TaskPhaseOf(messages, pending))
}
