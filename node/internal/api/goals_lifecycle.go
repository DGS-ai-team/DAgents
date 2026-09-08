package api

import (
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// observeGoalLifecycle converts the session-owned Turn projection at the API
// boundary. The goals package deliberately receives a small DTO to avoid an
// import cycle through tools.
func (s *Server) observeGoalLifecycle(sessionID string, snapshot turn.CoordinatorSnapshot) error {
	if s == nil || s.goalStore == nil {
		return nil
	}
	return s.goalStore.ObserveTurn(sessionID, goals.TurnSnapshot{
		TurnID: snapshot.TurnID, TurnStatus: string(snapshot.TurnStatus), StepStatus: string(snapshot.StepStatus),
		TurnEndReason: snapshot.TurnEndReason, StepEndReason: snapshot.StepEndReason, TotalTokens: snapshot.Usage.TotalTokens,
	}, time.Now().UTC())
}
