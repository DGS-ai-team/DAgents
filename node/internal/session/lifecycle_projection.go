package session

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// loadLifecycleProjection restores the durable Turn/Step projection for a
// cold session. Runtime instances use the same restore path during startup;
// read-only APIs must also consult it so they do not expose the deprecated
// RuntimeState.Pending/ToolLoopCount mirror as if it were authoritative.
func (m *Manager) loadLifecycleProjection(ctx context.Context, sessionID, agentID string) (turn.CoordinatorSnapshot, bool, uint64, error) {
	if m == nil || m.store == nil {
		return turn.CoordinatorSnapshot{}, false, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var events []turn.TurnEventEnvelope
	var afterSeq uint64
	for {
		page, err := m.store.ListTurnEvents(ctx, sessionID, afterSeq, 1000)
		if err != nil {
			return turn.CoordinatorSnapshot{}, false, 0, err
		}
		events = append(events, page...)
		if len(page) < 1000 {
			break
		}
		afterSeq = page[len(page)-1].SessionSeq
	}
	if len(events) == 0 {
		return turn.CoordinatorSnapshot{}, false, 0, nil
	}
	coordinator := turn.NewTurnCoordinator(sessionID, agentID)
	if err := coordinator.Restore(events); err != nil {
		return turn.CoordinatorSnapshot{}, true, 0, err
	}
	return coordinator.Snapshot(), true, events[len(events)-1].SessionSeq, nil
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
