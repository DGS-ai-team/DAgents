package session

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// runTurnStepWithSideEffects 在标准 runTurnStep 上挂接旁路 Apply/Reconcile。
func (r *runtime) runTurnStepWithSideEffects(
	parent context.Context,
	initialState turn.State,
	compressBefore bool,
	run func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome,
) (turn.StepOutcome, []llm.Message) {
	outcome, history := r.runTurnStep(parent, initialState, compressBefore, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		if r.sideEffectsEnabled() {
			r.sideEffects.ApplyReady(r.session.ID, r.orch, history, r.triggerDelivery)
		}
		out := run(ctx, history, setState)
		if r.sideEffectsEnabled() {
			r.mu.Lock()
			pending := r.pending
			r.mu.Unlock()
			out = r.sideEffects.ReconcileAfterStep(r.session.ID, r.orch, history, pending, out, r.triggerDelivery, func() {
				r.scheduleSideEffectContinue("reconcile")
			})
		}
		return out
	})
	return outcome, history
}
