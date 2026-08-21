package session

import (
	"context"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/media"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// HydrateView 为 GET /v1/agents/{id}/hydrate 的聚合视图。
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
		rt.mu.Lock()
		messages := append([]llm.Message(nil), rt.messages...)
		queuePending := rt.queue.Len()
		notifySeq := rt.notifySeq
		ackSeq := rt.ackSeq
		rt.mu.Unlock()
		state := rt.turnState()
		pending := rt.pendingSnapshot()
		lifecycle := rt.turnCoordinator.Snapshot()
		hasActiveTurn := lifecycle.HasActiveTurn
		return m.buildHydrateView(sessionID, messages, pending, state, queuePending, hasActiveTurn, notifySeq, ackSeq), nil
	}
	if m.store == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	pending := rec.RuntimeState.Pending
	hasActiveTurn := pending != nil
	state := turn.StateIdle
	lifecycle, projected, projectionErr := m.loadLifecycleProjection(context.Background(), sessionID, rec.NodeID)
	if projectionErr != nil {
		m.logger.Warn("load persisted turn lifecycle projection failed", "session_id", sessionID, "error", projectionErr)
	} else if projected {
		pending = pendingFromLifecycleSnapshot(lifecycle, nil)
		hasActiveTurn = lifecycle.HasActiveTurn
		state = turnStateFromCoordinatorSnapshot(lifecycle)
	} else if pending != nil {
		state = turn.StateAwaitingTool
	}
	return m.buildHydrateView(sessionID, rec.Messages, pending, state, 0, hasActiveTurn, rec.RuntimeState.NotifySeq, rec.RuntimeState.AckSeq), nil
}

func (m *Manager) buildHydrateView(
	sessionID string,
	messages []llm.Message,
	pending *turn.PendingHITL,
	state turn.State,
	queuePending int,
	hasActiveTurn bool,
	notifySeq int,
	ackSeq int,
) *HydrateView {
	transcript := MessagesToTranscriptEntriesWithMedia(messages, m.mediaRegistry(sessionID))
	if transcript == nil {
		transcript = []TranscriptEntry{}
	}
	if reg := m.mediaRegistry(sessionID); reg != nil {
		callIndex := buildToolCallIndex(messages)
		mediaByCall := media.RehydrateFromMessages(reg, messages, callIndex)
		EnrichTranscriptMedia(transcript, mediaByCall)
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
