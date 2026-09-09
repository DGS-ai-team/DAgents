package session

import (
	"context"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type HandbookMaintenanceResult struct {
	Usage      turn.TurnUsage
	Changed    bool
	Unknown    bool
	UsageKnown bool
}

// HandbookTurnBinding is invoked after the durable lifecycle has assigned the
// real turn ID and before any model request is made.
type HandbookTurnBinding func(sessionID, turnID string) error

// RunHandbookMaintenance executes one real Agent turn while the caller holds
// the maintenance gate. It deliberately does not acquire a gate itself.
func (m *Manager) RunHandbookMaintenance(ctx context.Context, sessionID, prompt string, budget turn.TurnBudget) (HandbookMaintenanceResult, error) {
	return m.RunHandbookMaintenanceWithBinding(ctx, sessionID, prompt, budget, nil)
}

func (m *Manager) RunHandbookMaintenanceWithBinding(ctx context.Context, sessionID, prompt string, budget turn.TurnBudget, bind HandbookTurnBinding) (HandbookMaintenanceResult, error) {
	if m == nil || ctx == nil {
		return HandbookMaintenanceResult{Unknown: true}, fmt.Errorf("maintenance executor unavailable")
	}
	if budget.MaxSteps <= 0 || budget.MaxTotalTokens <= 0 {
		return HandbookMaintenanceResult{}, fmt.Errorf("handbook maintenance budget is insufficient")
	}
	m.mu.RLock()
	r := m.sessions[sessionID]
	var g *agentExecutionGate
	if r != nil {
		g = m.maintenanceGates[r.agentID]
	}
	m.mu.RUnlock()
	if r == nil || r.orch == nil {
		return HandbookMaintenanceResult{Unknown: true}, fmt.Errorf("session runtime unavailable")
	}
	if !r.autoAgent {
		return HandbookMaintenanceResult{}, fmt.Errorf("handbook maintenance requires Auto Agent")
	}
	if g == nil || !g.owns(ctx) {
		return HandbookMaintenanceResult{}, fmt.Errorf("maintenance gate is not held")
	}
	mutationBefore := uint64(0)
	if reg := r.orch.ToolRegistry(); reg != nil {
		mutationBefore = reg.HandbookMutationCount()
	}
	r.lifecycleMu.Lock()
	previousBudget := r.turnBudget
	r.turnBudget = budget
	r.lifecycleMu.Unlock()
	defer func() { r.lifecycleMu.Lock(); r.turnBudget = previousBudget; r.lifecycleMu.Unlock() }()
	user := llm.UserMessage(prompt, llm.UserNameHuman)
	// lifecycleBeginInputTurn persists the pending input as the durable turn
	// start. Handbook turns bypass the normal input queue, so publish the same
	// pending message explicitly before opening the lifecycle.
	r.mu.Lock()
	r.pendingInputMessage = &user
	r.mu.Unlock()
	beginErr := r.lifecycleBeginInputTurn(turn.TurnSourceSideEffect)
	r.mu.Lock()
	r.pendingInputMessage = nil
	r.mu.Unlock()
	if beginErr != nil {
		return HandbookMaintenanceResult{Unknown: true}, beginErr
	}
	if bind != nil {
		turnID := r.turnCoordinator.Snapshot().TurnID
		if turnID == "" {
			bindErr := fmt.Errorf("handbook turn ID unavailable")
			if cancelErr := r.lifecycleCancel(); cancelErr != nil {
				bindErr = fmt.Errorf("%w; lifecycle cancel: %v", bindErr, cancelErr)
			}
			return HandbookMaintenanceResult{UsageKnown: true}, bindErr
		}
		if bindErr := bind(r.session.ID, turnID); bindErr != nil {
			if cancelErr := r.lifecycleCancel(); cancelErr != nil {
				bindErr = fmt.Errorf("%w; lifecycle cancel: %v", bindErr, cancelErr)
			}
			return HandbookMaintenanceResult{UsageKnown: true}, bindErr
		}
	}
	historyStart := r.lifecycleHistoryLength()
	maintCtx := tools.WithHandbookMaintenance(ctx)
	outcome, history := r.runTurnStepWithSideEffects(maintCtx, false, func(stepCtx context.Context, h *[]llm.Message) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(stepCtx, r.session.ID, h, user)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil && outcome.Err == nil {
		outcome.Err = err
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(maintCtx, 0, outcome)
	r.finishTurnIdle(outcome)
	state := r.turnCoordinator.Snapshot()
	changed := false
	if reg := r.orch.ToolRegistry(); reg != nil {
		changed = reg.HandbookMutationCount() > mutationBefore
	}
	// Mutation accounting is authoritative. A successful write/search_replace
	// can still be a no-op (or an error encoded in a tool result).
	usageKnown := state.ModelUsageKnown
	result := HandbookMaintenanceResult{Usage: state.Usage, Changed: changed, Unknown: !usageKnown, UsageKnown: usageKnown}
	if outcome.Pending != nil || state.StepStatus == turn.StepStatusWaitingInteraction {
		return result, fmt.Errorf("handbook maintenance requires interaction and did not complete")
	}
	if outcome.Err != nil {
		return result, outcome.Err
	}
	return result, nil
}
