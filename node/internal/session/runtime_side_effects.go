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

func (r *runtime) handleSideEffectProduceAsync(parent context.Context, payload *queue.AsyncToolResultPayload) {
	if payload == nil || !r.sideEffectsEnabled() {
		return
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	pending := r.pending
	r.mu.Unlock()

	r.sideEffects.Produce(r.orch, r.session.ID, msgs, sideEffectProduceInput{
		Kind:  turn.SideEffectAsync,
		Async: payload,
	})
	r.maybeScheduleSideEffectContinueAfterProduce(msgs, pending)
}

func (r *runtime) handleSideEffectProduceExternal(parent context.Context, env queue.Envelope) {
	if !r.sideEffectsEnabled() {
		return
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	pending := r.pending
	r.mu.Unlock()

	r.sideEffects.Produce(r.orch, r.session.ID, msgs, sideEffectProduceInput{
		Kind:           turn.SideEffectExternalMessage,
		MessageContent: env.Content,
		UserName:       env.UserName,
		TriggerID:      env.TriggerID,
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
	r.mu.Lock()
	pending := r.pending
	r.mu.Unlock()
	if pending != nil {
		return
	}
	if !r.sideEffects.markContinuePending() {
		return
	}
	_ = source
	_ = r.enqueue(queue.Envelope{
		RequestType:              queue.RequestTypeSideEffectContinue,
		SideEffectContinueSource: source,
	}, queue.PriorityToolResult)
}

func (r *runtime) maybeScheduleContinueAfterCancel() {
	if !r.sideEffectsEnabled() {
		return
	}
	r.mu.Lock()
	pending := r.pending
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

	if strings.TrimSpace(source) == "" {
		source = "side_effect_continue"
	}
	pendingCount := r.sideEffects.Len()
	r.orch.PublishSideEffectTurnStart(r.session.ID, source, pendingCount)

	loopCount := r.toolLoopCountSnapshot()
	outcome, history := r.runTurnStepWithSideEffects(parent, turn.StateModelStreaming, true, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		return r.orch.ContinueAfterSideEffects(ctx, r.session.ID, history, setState, loopCount)
	})
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	r.afterToolStep(outcome)
}
