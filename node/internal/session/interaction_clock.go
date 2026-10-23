package session

import (
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// recoverLastHumanInteraction reconciles the runtime checkpoint with the
// durable lifecycle journal. A process can stop after a terminal event is
// committed but before finishTurnIdle saves the runtime clock; the journal is
// the completion authority for that crash window.
func recoverLastHumanInteraction(events []turn.TurnEventEnvelope, checkpoint time.Time) time.Time {
	sources := make(map[string]string)
	lastAt := checkpoint
	for _, event := range events {
		source := event.Source
		if event.EventType == turn.EventTurnStarted || event.EventType == turn.EventTurnInputAccepted {
			if source != "" {
				sources[event.TurnID] = source
			}
		}
		if isTerminalTurnEvent(event.EventType) {
			if source == "" {
				source = sources[event.TurnID]
			}
			if source == string(turn.TurnSourceHuman) && event.CreatedAt.After(lastAt) {
				lastAt = event.CreatedAt.UTC()
			}
		}
	}
	return lastAt
}

func isTerminalTurnEvent(eventType turn.EventType) bool {
	switch eventType {
	case turn.EventTurnCompleted, turn.EventTurnFailed, turn.EventTurnCancelled, turn.EventTurnInterrupted, turn.EventTurnBudgetExhausted:
		return true
	default:
		return false
	}
}
