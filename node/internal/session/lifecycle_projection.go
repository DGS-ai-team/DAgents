package session

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type lifecycleProjectionData struct {
	snapshot turn.CoordinatorSnapshot
	sequence uint64
	events   []turn.TurnEventEnvelope
}

// loadLifecycleProjectionData loads and replays a session's lifecycle once.
// Callers that construct a runtime can pass the returned events through so
// runtime restoration does not issue a second identical database scan.
func (m *Manager) loadLifecycleProjectionData(ctx context.Context, sessionID, agentID string) (lifecycleProjectionData, error) {
	if m == nil || m.store == nil {
		return lifecycleProjectionData{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var events []turn.TurnEventEnvelope
	var afterSeq uint64
	for {
		page, err := m.store.ListTurnEvents(ctx, sessionID, afterSeq, 1000)
		if err != nil {
			return lifecycleProjectionData{}, err
		}
		events = append(events, page...)
		if len(page) < 1000 {
			break
		}
		afterSeq = page[len(page)-1].SessionSeq
	}
	if len(events) == 0 {
		return lifecycleProjectionData{}, nil
	}
	coordinator := turn.NewTurnCoordinator(sessionID, agentID)
	if err := coordinator.Restore(events); err != nil {
		return lifecycleProjectionData{events: events}, err
	}
	return lifecycleProjectionData{
		snapshot: coordinator.Snapshot(),
		sequence: events[len(events)-1].SessionSeq,
		events:   events,
	}, nil
}

// loadLifecycleProjection restores the durable Turn/Step projection for a
// cold session. Runtime instances use the same restore path during startup;
// read-only APIs consult the same projection so every lifecycle view has one
// authority.
func (m *Manager) loadLifecycleProjection(ctx context.Context, sessionID, agentID string) (turn.CoordinatorSnapshot, bool, uint64, error) {
	data, err := m.loadLifecycleProjectionData(ctx, sessionID, agentID)
	return data.snapshot, len(data.events) > 0, data.sequence, err
}

func turnStateFromCoordinatorSnapshot(snapshot turn.CoordinatorSnapshot) turn.State {
	if !snapshot.HasActiveTurn || snapshot.StepStatus.Terminal() {
		return turn.StateIdle
	}
	if snapshot.StepStatus == turn.StepStatusExecutingTools || snapshot.StepStatus == turn.StepStatusWaitingInteraction {
		return turn.StateAwaitingTool
	}
	return turn.StateModelStreaming
}
