package session

import (
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"testing"
)

func TestManagedGoalBudgetResolverRefreshesEachTurn(t *testing.T) {
	remaining := 8
	r := newLifecycleTestRuntime()
	r.turnBudget = turn.TurnBudget{MaxTotalTokens: 10}
	r.budgetResolver = func() (turn.TurnBudget, error) {
		return turn.TurnBudget{MaxTotalTokens: remaining, MaxSteps: 4, MaxToolCalls: 4}, nil
	}
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if got := r.turnCoordinator.Snapshot().Budget.MaxTotalTokens; got != 8 {
		t.Fatalf("first turn budget=%d", got)
	}
	r.lifecycleAfterModelStep(turn.StepOutcome{}, nil, 0)
	remaining = 2
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	if got := r.turnCoordinator.Snapshot().Budget.MaxTotalTokens; got != 2 {
		t.Fatalf("refreshed turn budget=%d", got)
	}
}

func TestManagedGoalUnknownBudgetResolverBlocksNextTurn(t *testing.T) {
	r := newLifecycleTestRuntime()
	r.budgetResolver = func() (turn.TurnBudget, error) { return turn.TurnBudget{}, turn.ErrBudgetExhausted }
	if err := r.lifecycleBeginHumanTurn(); err == nil {
		t.Fatal("expected unknown/exhausted budget to block turn")
	}
}
