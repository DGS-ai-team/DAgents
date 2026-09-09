package turn

import (
	"fmt"
	"strings"
)

// MaintenanceReconciliation is the read-only conclusion of replaying one
// persisted maintenance turn. Completed is intentionally stricter than a
// terminal coordinator status: every model attempt must have usage and no
// tool or interaction may remain recoverable.
type MaintenanceReconciliation struct {
	Completed  bool
	UsageKnown bool
	Usage      TurnUsage
	TurnStatus TurnStatus
	Reason     string
}

// ReconcileMaintenanceTurn validates identity and journal continuity, then
// replays the supplied events through the existing coordinator state machine.
// It never mutates the events or any external store and never executes work.
func ReconcileMaintenanceTurn(events []TurnEventEnvelope, expectedAgent, expectedSession, expectedTurn string) (MaintenanceReconciliation, error) {
	expectedAgent = strings.TrimSpace(expectedAgent)
	expectedSession = strings.TrimSpace(expectedSession)
	expectedTurn = strings.TrimSpace(expectedTurn)
	if expectedAgent == "" || expectedSession == "" || expectedTurn == "" {
		return MaintenanceReconciliation{}, fmt.Errorf("maintenance reconciliation identity is required")
	}
	if len(events) == 0 {
		return MaintenanceReconciliation{Reason: "turn events are missing"}, nil
	}
	var terminal bool
	for i, event := range events {
		if err := event.Validate(); err != nil {
			return MaintenanceReconciliation{}, fmt.Errorf("event %d: %w", i, err)
		}
		if event.EventVersion != 1 {
			return MaintenanceReconciliation{}, fmt.Errorf("event %d: unsupported event version %d", i, event.EventVersion)
		}
		if strings.TrimSpace(event.AgentID) != expectedAgent || event.SessionID != expectedSession || event.TurnID != expectedTurn {
			return MaintenanceReconciliation{}, fmt.Errorf("event %d identity mismatch", i)
		}
		if i == 0 {
			if event.EventType != EventTurnStarted || event.SessionSeq == 0 || event.TurnSeq != 1 {
				return MaintenanceReconciliation{}, fmt.Errorf("maintenance turn must begin with turn.started at turn sequence 1")
			}
		} else {
			previous := events[i-1]
			if event.SessionSeq != previous.SessionSeq+1 || event.TurnSeq != previous.TurnSeq+1 {
				return MaintenanceReconciliation{}, fmt.Errorf("event %d sequence gap", i)
			}
			if terminal {
				return MaintenanceReconciliation{}, fmt.Errorf("event %d follows terminal turn", i)
			}
		}
		if event.EventType == EventTurnCompleted || event.EventType == EventTurnFailed || event.EventType == EventTurnCancelled || event.EventType == EventTurnInterrupted || event.EventType == EventTurnBudgetExhausted {
			terminal = true
		}
	}

	c := NewTurnCoordinator(expectedSession, expectedAgent)
	if err := c.Restore(events); err != nil {
		return MaintenanceReconciliation{}, fmt.Errorf("replay maintenance turn: %w", err)
	}
	snapshot := c.Snapshot()
	result := MaintenanceReconciliation{
		UsageKnown: snapshot.ModelUsageKnown,
		Usage:      snapshot.Usage,
		TurnStatus: snapshot.TurnStatus,
	}
	if !terminal {
		result.Reason = "turn has no terminal event"
		return result, nil
	}
	if snapshot.RecoveryRequired {
		result.Reason = "turn requires recovery"
		return result, nil
	}
	for _, execution := range snapshot.ToolExecutions {
		if !execution.Status.Terminal() {
			result.Reason = "tool execution is incomplete"
			return result, nil
		}
	}
	if snapshot.InteractionID != "" || snapshot.TurnStatus == TurnStatusWaiting {
		result.Reason = "turn is waiting for interaction"
		return result, nil
	}
	if !snapshot.ModelUsageKnown {
		result.Reason = "model usage is incomplete"
		return result, nil
	}
	if snapshot.TurnStatus != TurnStatusCompleted {
		result.Reason = fmt.Sprintf("turn ended with status %s", snapshot.TurnStatus)
		return result, nil
	}
	result.Completed = true
	result.Reason = "completed"
	return result, nil
}
