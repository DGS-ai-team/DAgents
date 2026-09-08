package api

import (
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestApplyGoalBudgetUsesRemainingAndFiniteLimits(t *testing.T) {
	opts := session.TurnOptions{Budget: turn.TurnBudget{MaxSteps: 200, MaxToolCalls: 500, MaxWallTime: time.Hour}}
	err := applyGoalBudget(&opts, goals.Goal{TokenBudget: 1000, TokensUsed: 250, TurnTokenBudget: 600})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Budget.MaxTotalTokens != 600 {
		t.Fatalf("total budget = %d", opts.Budget.MaxTotalTokens)
	}
	if opts.Budget.MaxSteps != goalDefaultMaxSteps || opts.Budget.MaxToolCalls != goalDefaultMaxToolCalls || opts.Budget.MaxWallTime != goalDefaultMaxWallTime {
		t.Fatalf("finite limits = %+v", opts.Budget)
	}
	opts = session.TurnOptions{Budget: turn.TurnBudget{MaxSteps: 2, MaxToolCalls: 3, MaxWallTime: time.Second, MaxTotalTokens: 100}}
	if err := applyGoalBudget(&opts, goals.Goal{TokenBudget: 1000, TokensUsed: 900, TurnTokenBudget: 500}); err != nil {
		t.Fatal(err)
	}
	if opts.Budget.MaxTotalTokens != 100 || opts.Budget.MaxSteps != 2 || opts.Budget.MaxToolCalls != 3 || opts.Budget.MaxWallTime != time.Second {
		t.Fatalf("stricter agent limits overwritten: %+v", opts.Budget)
	}
}

func TestApplyGoalBudgetRejectsUnknownOrExhaustedUsage(t *testing.T) {
	if err := applyGoalBudget(&session.TurnOptions{}, goals.Goal{TokenBudget: 10, TokensUsed: 10, TurnTokenBudget: 1}); err == nil {
		t.Fatal("expected exhausted goal rejection")
	}
	if err := applyGoalBudget(&session.TurnOptions{}, goals.Goal{TokenBudget: 10, TokensUsed: -1, TurnTokenBudget: 1}); err == nil {
		t.Fatal("expected unknown usage rejection")
	}
}

func TestApplyGoalBudgetBlocksQueuedGoalAfterStopOrPause(t *testing.T) {
	for _, status := range []goals.Status{goals.StatusPaused, goals.StatusStopped, goals.StatusCompleted} {
		if err := applyGoalBudget(&session.TurnOptions{}, goals.Goal{Status: status, TokenBudget: 10, TurnTokenBudget: 2}); err == nil {
			t.Fatalf("status %s was allowed to start a new turn", status)
		}
	}
}

func TestApplyGoalBudgetCapsWallTimeAtDeadline(t *testing.T) {
	deadline := time.Now().Add(2 * time.Second)
	opts := session.TurnOptions{Budget: turn.TurnBudget{MaxWallTime: time.Minute}}
	if err := applyGoalBudget(&opts, goals.Goal{TokenBudget: 10, TurnTokenBudget: 2, ExpiresAt: &deadline}); err != nil {
		t.Fatal(err)
	}
	if opts.Budget.MaxWallTime <= 0 || opts.Budget.MaxWallTime > 2*time.Second {
		t.Fatalf("wall time was not capped by deadline: %s", opts.Budget.MaxWallTime)
	}
	expired := time.Now().Add(-time.Second)
	if err := applyGoalBudget(&session.TurnOptions{}, goals.Goal{TokenBudget: 10, TurnTokenBudget: 2, ExpiresAt: &expired}); err == nil {
		t.Fatal("expected expired goal rejection")
	}
}
