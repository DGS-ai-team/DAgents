package api

import (
	"fmt"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
)

const (
	goalDefaultMaxSteps     = 64
	goalDefaultMaxToolCalls = 128
	goalDefaultMaxWallTime  = 30 * time.Minute
)

// goalForSession returns the goal that owns a dedicated runtime. Goal sessions
// are named at creation time and remain stable across wakes.
func (s *Server) goalForSession(sessionID string) (goals.Goal, bool, error) {
	if s.goalStore == nil {
		return goals.Goal{}, false, nil
	}
	for _, goal := range s.goalStore.List() {
		if goal.SessionID == sessionID || "goal-session-"+goal.ID == sessionID {
			for _, run := range s.goalStore.Runs(goal.ID) {
				if run.Status == "unknown" {
					return goals.Goal{}, false, fmt.Errorf("goal usage is unknown after runtime restart; reconcile before waking")
				}
				if run.FinishedAt != nil && run.TokensUsed == 0 && run.Status != "failed" && run.Status != "cancelled" {
					return goals.Goal{}, false, fmt.Errorf("goal usage is unknown; refusing to continue")
				}
			}
			if goal.TokensUsed < 0 {
				return goals.Goal{}, false, fmt.Errorf("goal usage is unknown")
			}
			return goal, true, nil
		}
	}
	return goals.Goal{}, false, nil
}

// lookupGoalSession only resolves ownership. Read-only session endpoints must
// be able to hydrate paused/completed history without passing run-budget
// checks or changing the goal's runnable state.
func (s *Server) lookupGoalSession(sessionID string) (goals.Goal, bool) {
	if s == nil || s.goalStore == nil {
		return goals.Goal{}, false
	}
	for _, goal := range s.goalStore.List() {
		if goal.SessionID == sessionID || "goal-session-"+goal.ID == sessionID {
			return goal, true
		}
	}
	return goals.Goal{}, false
}

// applyGoalBudget gives a goal turn both a per-turn ceiling and a cumulative
// remaining ceiling. Zero agent limits are tightened to finite goal defaults;
// existing stricter limits are preserved.
func applyGoalBudget(opts *session.TurnOptions, goal goals.Goal) error {
	if opts == nil {
		return fmt.Errorf("nil turn options")
	}
	if goal.TokenBudget < 0 || goal.TokensUsed < 0 {
		return fmt.Errorf("goal usage is unknown")
	}
	if goal.Status == goals.StatusStopped || goal.Status == goals.StatusPaused || goal.Status == goals.StatusCompleted {
		return fmt.Errorf("goal is not runnable: %s", goal.Status)
	}
	if goal.ExpiresAt != nil {
		remainingWall := time.Until(*goal.ExpiresAt)
		if remainingWall <= 0 {
			return fmt.Errorf("goal deadline expired")
		}
		// A turn must never outlive the goal deadline. Keep a stricter
		// agent-supplied wall limit when one is already configured.
		if opts.Budget.MaxWallTime == 0 || remainingWall < opts.Budget.MaxWallTime {
			opts.Budget.MaxWallTime = remainingWall
		}
	}
	remaining := goal.TokenBudget - goal.TokensUsed
	if remaining <= 0 {
		return fmt.Errorf("goal token budget exhausted")
	}
	perTurn := int64(goal.TurnTokenBudget)
	if perTurn <= 0 || perTurn > remaining {
		perTurn = remaining
	}
	if perTurn > int64(^uint(0)>>1) {
		perTurn = int64(^uint(0) >> 1)
	}
	budget := &opts.Budget
	capInt := int(perTurn)
	if budget.MaxTotalTokens == 0 || budget.MaxTotalTokens > capInt {
		budget.MaxTotalTokens = capInt
	}
	if budget.MaxSteps == 0 || budget.MaxSteps > goalDefaultMaxSteps {
		budget.MaxSteps = goalDefaultMaxSteps
	}
	if budget.MaxToolCalls == 0 || budget.MaxToolCalls > goalDefaultMaxToolCalls {
		budget.MaxToolCalls = goalDefaultMaxToolCalls
	}
	if budget.MaxWallTime == 0 || budget.MaxWallTime > goalDefaultMaxWallTime {
		budget.MaxWallTime = goalDefaultMaxWallTime
	}
	return nil
}
