package api

import (
	"context"
	"strings"
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
	err := s.goalStore.ObserveTurn(sessionID, goals.TurnSnapshot{
		TurnID: snapshot.TurnID, TurnStatus: string(snapshot.TurnStatus), StepStatus: string(snapshot.StepStatus),
		TurnEndReason: snapshot.TurnEndReason, StepEndReason: snapshot.StepEndReason, TotalTokens: snapshot.Usage.TotalTokens,
	}, time.Now().UTC())
	if err != nil {
		return err
	}
	return nil
}

func (s *Server) reconcileAutoIntents(ctx context.Context, now time.Time) error {
	if s == nil || s.goalStore == nil || s.triggerStore == nil {
		return nil
	}
	projector := &AutoIntentProjector{Goals: s.goalStore, Triggers: s.triggerStore}
	if err := s.reconcileAutoCycles(ctx, now); err != nil {
		return err
	}
	var firstErr error
	for _, intent := range s.goalStore.ListScheduleIntents("") {
		if _, err := projector.Project(intent.GoalID, intent.Purpose); err != nil {
			// Fenced and unsupported intents are valid durable outcomes and stay
			// isolated; all other errors are retriable persistence/projection
			// failures and are surfaced to the scheduler.
			if strings.Contains(err.Error(), "intent_projection_fenced") || strings.Contains(err.Error(), "event_projection_unsupported") {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	_ = ctx
	_ = now
	return firstErr
}
