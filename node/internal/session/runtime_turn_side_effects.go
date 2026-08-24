package session

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// runTurnStepWithSideEffects 在标准 runTurnStep 上挂接旁路 Apply/Reconcile。
func (r *runtime) runTurnStepWithSideEffects(
	parent context.Context,
	compressBefore bool,
	run func(ctx context.Context, history *[]llm.Message) turn.StepOutcome,
) (turn.StepOutcome, []llm.Message) {
	return r.runTurnStepWithSideEffectsAtEpoch(parent, compressBefore, 0, run)
}

func (r *runtime) runTurnStepWithSideEffectsAtEpoch(
	parent context.Context,
	compressBefore bool,
	expectedEpoch uint64,
	run func(ctx context.Context, history *[]llm.Message) turn.StepOutcome,
) (turn.StepOutcome, []llm.Message) {
	outcome, history := r.runTurnStepAtEpoch(parent, compressBefore, expectedEpoch, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		if r.sideEffectsEnabled() {
			// A ready async callback is an external fact, but it must not mutate
			// the history while any HITL item from the current tool batch is still
			// pending. Applying it before ContinueAfterResume can split the
			// assistant tool-call batch. The callback bridge is context-only;
			// its acceptance is recorded separately as an external lifecycle fact.
			if r.pendingSnapshot() == nil {
				r.sideEffects.ApplyReady(r.session.ID, r.orch, history, r.triggerDelivery, r.recordSideEffectFact)
			}
		}
		out := run(ctx, history)
		if r.sideEffectsEnabled() {
			pending := r.pendingSnapshot()
			out = r.sideEffects.ReconcileAfterStep(r.session.ID, r.orch, history, pending, out, r.triggerDelivery, func() {
				r.scheduleSideEffectContinue("reconcile")
			}, r.recordSideEffectFact)
		}
		return out
	})
	return outcome, history
}
