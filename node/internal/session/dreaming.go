package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// DreamingTurnResult is the successful model output that may be committed by
// the autonomy store. Session deliberately does not persist experience or
// context-reset state here.
type DreamingTurnResult struct {
	Content    string
	Changed    bool
	Usage      turn.TurnUsage
	UsageKnown bool
}

// RunDreaming runs on an already-created main session while its maintenance
// lease is held. It uses the real Agent model and handbook tools, but has no
// receipt, token-budget, or external-fact side channel.
func (m *Manager) RunDreaming(ctx context.Context, sessionID, prompt string, maxToolRounds int) (DreamingTurnResult, error) {
	if m == nil || ctx == nil || maxToolRounds <= 0 {
		return DreamingTurnResult{}, fmt.Errorf("dreaming executor unavailable")
	}
	m.mu.RLock()
	r := m.sessions[sessionID]
	var gate *agentExecutionGate
	if r != nil {
		gate = m.maintenanceGates[r.agentID]
	}
	m.mu.RUnlock()
	if r == nil || r.orch == nil || !r.autoAgent {
		return DreamingTurnResult{}, fmt.Errorf("dreaming requires an Auto session")
	}
	if gate == nil || !gate.owns(ctx) {
		return DreamingTurnResult{}, fmt.Errorf("maintenance gate is not held")
	}
	reg := r.orch.ToolRegistry()
	if reg == nil || strings.TrimSpace(reg.HandbookRoot()) == "" {
		return DreamingTurnResult{}, fmt.Errorf("handbook is unavailable")
	}
	mutationBefore := reg.HandbookMutationCount()
	dreamBudget := turn.TurnBudget{MaxToolRounds: maxToolRounds, ReserveFinalSummary: true}
	r.lifecycleMu.Lock()
	previousBudget, previousResolver := r.turnBudget, r.budgetResolver
	// A configured resolver normally supplies the user-chat budget. Dreaming
	// has its own bounded tool-round budget, so suspend that resolver only for
	// this lifecycle start and restore it before returning.
	r.turnBudget = dreamBudget
	r.budgetResolver = func() (turn.TurnBudget, error) { return dreamBudget, nil }
	r.lifecycleMu.Unlock()
	defer func() {
		r.lifecycleMu.Lock()
		r.turnBudget, r.budgetResolver = previousBudget, previousResolver
		r.lifecycleMu.Unlock()
	}()

	user := llm.UserMessage(strings.TrimSpace(prompt), llm.UserNameHuman)
	r.mu.Lock()
	r.pendingInputMessage = &user
	r.mu.Unlock()
	beginErr := r.lifecycleBeginInputTurn(turn.TurnSourceSideEffect)
	r.mu.Lock()
	r.pendingInputMessage = nil
	r.mu.Unlock()
	if beginErr != nil {
		return DreamingTurnResult{}, beginErr
	}
	historyStart := r.lifecycleHistoryLength()
	dreamCtx := tools.WithHandbookMaintenance(ctx)
	outcome, history := r.runTurnStepWithSideEffects(dreamCtx, false, func(stepCtx context.Context, h *[]llm.Message) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(stepCtx, r.session.ID, h, user)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil && outcome.Err == nil {
		outcome.Err = err
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(dreamCtx, 0, outcome)
	r.finishTurnIdle(outcome)
	state := r.turnCoordinator.Snapshot()
	result := DreamingTurnResult{Usage: state.Usage, UsageKnown: state.ModelUsageKnown, Changed: reg.HandbookMutationCount() > mutationBefore}
	if outcome.Pending != nil || state.StepStatus == turn.StepStatusWaitingInteraction {
		return result, fmt.Errorf("dreaming turn requires interaction")
	}
	if outcome.Err != nil {
		return result, outcome.Err
	}
	active := r.activeMessagesSnapshot()
	final, ok := lastAssistantMessage(active, 0)
	if !ok || len(final.ToolCalls) != 0 || strings.TrimSpace(final.Content) == "" ||
		state.TurnStatus != turn.TurnStatusCompleted || state.StepStatus != turn.StepStatusCompleted || state.HasActiveTurn {
		return result, fmt.Errorf("dreaming result is empty")
	}
	result.Content = strings.TrimSpace(final.Content)
	return result, nil
}
