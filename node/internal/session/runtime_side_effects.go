package session

import (
	"context"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func (r *runtime) sideEffectsEnabled() bool {
	return r.sideEffects != nil && !r.isChildSession()
}

func (r *runtime) handleSideEffectProduceAsync(_ context.Context, payload *queue.AsyncToolResultPayload) {
	if payload == nil || !r.sideEffectsEnabled() {
		return
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	r.mu.Unlock()
	pending := r.pendingSnapshot()

	r.sideEffects.Produce(r.orch, r.session.ID, msgs, sideEffectProduceInput{
		Async: payload,
	})
	r.maybeScheduleSideEffectContinueAfterProduce(msgs, pending)
}

func (r *runtime) maybeScheduleSideEffectContinueAfterProduce(messages []llm.Message, pending *turn.PendingHITL) {
	if pending != nil {
		return
	}
	if len(messages) == 0 || turn.TaskComplete(messages, pending) {
		r.scheduleSideEffectContinue("task_complete_produce")
	}
}

func (r *runtime) scheduleSideEffectContinue(source string) {
	if !r.sideEffectsEnabled() {
		return
	}
	pending := r.pendingSnapshot()
	if pending != nil {
		return
	}
	if !r.sideEffects.markContinuePending() {
		return
	}
	if err := r.enqueue(queue.Envelope{
		RequestType:              queue.RequestTypeSideEffectContinue,
		SideEffectContinueSource: source,
	}, queue.PriorityContinuation); err != nil {
		r.sideEffects.clearContinuePending()
		r.logger.Warn("side_effect_continue enqueue failed",
			"session_id", r.session.ID,
			"source", source,
			"error", err,
		)
	}
}

func (r *runtime) maybeScheduleContinueAfterCancel() {
	if !r.sideEffectsEnabled() {
		return
	}
	pending := r.pendingSnapshot()
	r.mu.Lock()
	hasReady := r.sideEffects.HasReady()
	r.mu.Unlock()
	if pending != nil || !hasReady {
		return
	}
	r.scheduleSideEffectContinue("cancel_recovery")
}

func (r *runtime) handleSideEffectContinue(parent context.Context, source string) {
	if !r.sideEffectsEnabled() {
		return
	}
	r.sideEffects.clearContinuePending()
	// A side-effect continuation is created by enqueue(), which binds it to the
	// current turn generation. If cancellation invalidated that generation, the
	// envelope is discarded before this handler is reached.

	if strings.TrimSpace(source) == "" {
		source = "side_effect_continue"
	}
	pendingCount := r.sideEffects.Len()
	started, err := r.lifecycleBeginContinuationStep(turn.TurnSourceSideEffect)
	if err != nil {
		r.logger.Warn("start side-effect continuation lifecycle failed", "session_id", r.session.ID, "error", err)
		r.persist(context.Background())
		r.finishTurnIdle(turn.StepOutcome{})
		return
	}
	if !started {
		r.persist(context.Background())
		r.finishTurnIdle(turn.StepOutcome{})
		return
	}
	r.orch.PublishSideEffectTurnStart(r.session.ID, source, pendingCount)

	historyStart := r.lifecycleHistoryLength()
	outcome, history := r.runTurnStepWithSideEffects(parent, true, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		return r.orch.ContinueAfterSideEffects(ctx, r.session.ID, history)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil {
		r.logger.Warn("finish side-effect continuation lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(parent, 0, outcome)
	r.finishTurnIdle(outcome)
	r.persist(context.Background())
}
