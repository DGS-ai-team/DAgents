package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func autonomyView(g goals.Goal) map[string]any {
	return map[string]any{
		"goal_id": g.ID, "status": g.Status, "objective": g.Objective,
		"acceptance": g.Acceptance, "next_wake_at": g.NextWakeAt,
		"limits":     map[string]any{"max_runs": g.MaxRuns, "token_budget": g.TokenBudget, "turn_token_budget": g.TurnTokenBudget, "min_wake_interval_seconds": g.MinWakeIntervalSeconds, "expires_at": g.ExpiresAt},
		"usage":      map[string]any{"runs": g.Runs, "tokens_used": g.TokensUsed},
		"updated_at": g.UpdatedAt,
	}
}

func (s *Server) autonomyToolGet(ctx context.Context, agentID string) (any, error) {
	if tools.GoalIDFromContext(ctx) != "" {
		return nil, fmt.Errorf("autonomy tools are unavailable during a managed goal run")
	}
	if s == nil || s.goalStore == nil {
		return nil, fmt.Errorf("goals unavailable")
	}
	_, g, err := s.currentAutoGoal(agentID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if g == nil {
		return map[string]any{"goal_id": "", "status": "disabled", "objective": "", "acceptance": "", "next_wake_at": nil, "limits": nil, "usage": nil}, nil
	}
	return autonomyView(*g), nil
}

func (s *Server) autonomyToolUpdate(ctx context.Context, agentID string, in tools.AutonomyUpdate) (any, error) {
	if tools.GoalIDFromContext(ctx) != "" {
		return nil, fmt.Errorf("autonomy tools are unavailable during a managed goal run")
	}
	if s == nil || s.goalStore == nil {
		return nil, fmt.Errorf("goals unavailable")
	}
	s.goalWakeMu.Lock()
	defer s.goalWakeMu.Unlock()
	_, gp, err := s.currentAutoGoal(agentID, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if gp == nil {
		return nil, fmt.Errorf("autonomy is not configured; configure it in Agent settings first")
	}
	goal := *gp
	if goal.Status == goals.StatusCompleted || goal.Status == goals.StatusStopped || goal.Runs >= goal.MaxRuns || goal.TokensUsed >= goal.TokenBudget {
		return nil, fmt.Errorf("goal is not adjustable in its current state")
	}
	now := time.Now().UTC()
	if goal.ExpiresAt != nil && !now.Before(*goal.ExpiresAt) {
		return nil, fmt.Errorf("goal expired")
	}
	for _, run := range s.goalStore.Runs(goal.ID) {
		if run.FinishedAt == nil || run.Status == "unknown" || run.Status == "running" || run.Status == "pending" {
			return nil, fmt.Errorf("goal execution is busy or usage is unknown")
		}
		if run.TokensUsed < 0 || (run.TokensUsed == 0 && run.Status != "failed" && run.Status != "cancelled") {
			return nil, fmt.Errorf("goal usage is unknown")
		}
	}
	if goal.TriggerID != "" && s.triggerStore != nil {
		if _, ok := s.triggerStore.GetTrigger(goal.TriggerID); !ok {
			return nil, fmt.Errorf("managed goal trigger is unavailable")
		}
	}
	oldGoal := goal
	objective, acceptance := goal.Objective, goal.Acceptance
	if in.Objective != nil {
		objective = strings.TrimSpace(*in.Objective)
	}
	if in.Acceptance != nil {
		acceptance = strings.TrimSpace(*in.Acceptance)
	}
	if in.NextWakeAt != nil {
		if !in.NextWakeAt.After(now) {
			return nil, fmt.Errorf("next_wake_at must be in the future")
		}
		if goal.MinWakeIntervalSeconds > 0 && in.NextWakeAt.Before(now.Add(time.Duration(goal.MinWakeIntervalSeconds)*time.Second)) {
			return nil, fmt.Errorf("next_wake_at violates minimum wake interval")
		}
	}
	// All intent changes, including a text-only change, use the same atomic
	// store operation so limits and counters cannot be overwritten.
	goal, err = s.goalStore.UpdateAutonomyIntent(goal.ID, objective, acceptance, in.NextWakeAt, now)
	if err != nil {
		return nil, err
	}
	if in.Pause {
		var err error
		goal, err = s.goalStore.SetStatus(goal.ID, goals.StatusPaused, now)
		if err != nil {
			return nil, err
		}
	}
	if err := s.syncManagedTrigger(goal); err != nil {
		_ = s.goalStore.RestoreConfiguration(oldGoal.ID, oldGoal, time.Now().UTC())
		return nil, err
	}
	return autonomyView(goal), nil
}
