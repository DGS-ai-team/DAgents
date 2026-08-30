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
	SessionID       string
	Transcript      []TranscriptEntry
	PendingHITL     map[string]any
	TurnState       TurnStateView
	RunTurnPhase    string
	HasActiveTurn   bool
	QueuePending    int
	HistoryRevision uint64
	NotifySeq       int
	AckSeq          int
	HasUnread       bool
	ChildAgents     []ChildAgentView
}

// GetHydrateView 返回 session hydrate 快照（活跃 runtime 或 DB 持久化）。
func (m *Manager) GetHydrateView(sessionID string) (*HydrateView, error) {
	rt := m.getRuntime(sessionID)
	if rt != nil {
		// Lifecycle transitions publish turn_state while holding lifecycleMu.
		// Take the same lock before copying messages so a hydrate snapshot cannot
		// combine a pre-commit history with a newer terminal projection.
		rt.lifecycleMu.Lock()
		rt.mu.Lock()
		messages := append([]llm.Message(nil), rt.messages...)
		historyRevision := rt.historyRevision
		queuePending := rt.queue.Len()
		if rt.inputBox != nil {
			queuePending += rt.inputBox.Len()
		}
		notifySeq := rt.notifySeq
		ackSeq := rt.ackSeq
		state := rt.turnState()
		pending := rt.pendingSnapshot()
		lifecycle := rt.turnCoordinator.Snapshot()
		hasActiveTurn := lifecycle.HasActiveTurn
		rt.mu.Unlock()
		rt.lifecycleMu.Unlock()
		view := m.buildHydrateView(sessionID, messages, pending, state, lifecycle, rt.lifecycleEventSequence(), historyRevision, queuePending, hasActiveTurn, notifySeq, ackSeq)
		view.ChildAgents, _ = m.ListChildAgents(sessionID)
		return view, nil
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
	lifecycle, projected, lifecycleSeq, projectionErr := m.loadLifecycleProjection(context.Background(), sessionID, rec.NodeID)
	if projectionErr != nil {
		m.logger.Warn("load persisted turn lifecycle projection failed", "session_id", sessionID, "error", projectionErr)
	} else if projected {
		pending = pendingFromLifecycleSnapshot(lifecycle, nil)
		hasActiveTurn = lifecycle.HasActiveTurn
		state = turnStateFromCoordinatorSnapshot(lifecycle)
	} else if pending != nil {
		state = turn.StateAwaitingTool
	}
	queuePending := inputBoxPendingCount(rec.RuntimeState.InputBoxState)
	view := m.buildHydrateView(sessionID, rec.Messages, pending, state, lifecycle, lifecycleSeq, rec.RuntimeState.HistoryRevision, queuePending, hasActiveTurn, rec.RuntimeState.NotifySeq, rec.RuntimeState.AckSeq)
	view.ChildAgents = []ChildAgentView{}
	return view, nil
}

func (m *Manager) buildHydrateView(
	sessionID string,
	messages []llm.Message,
	pending *turn.PendingHITL,
	state turn.State,
	lifecycle turn.CoordinatorSnapshot,
	lifecycleSeq uint64,
	historyRevision uint64,
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
	turnState := buildTurnStateView(lifecycle, lifecycleSeq)
	turnState.HistoryRevision = historyRevision
	if lifecycle.TurnID == "" && pending != nil {
		turnState = TurnStateView{
			Authority:       "hydrate_legacy",
			Phase:           "tool_waiting",
			Terminal:        false,
			InteractionKind: "approval",
		}
	}
	turnState.HistoryRevision = historyRevision
	return &HydrateView{
		SessionID:       sessionID,
		Transcript:      transcript,
		PendingHITL:     turn.BuildHITLRequiredSnapshot(pending),
		TurnState:       turnState,
		RunTurnPhase:    hydrateRunTurnPhase(messages, pending, state, hasActiveTurn),
		HasActiveTurn:   hasActiveTurn,
		QueuePending:    queuePending,
		HistoryRevision: historyRevision,
		NotifySeq:       notifySeq,
		AckSeq:          ackSeq,
		HasUnread:       notifySeq > ackSeq,
		ChildAgents:     []ChildAgentView{},
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
