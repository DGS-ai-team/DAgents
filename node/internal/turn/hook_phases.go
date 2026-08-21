package turn

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// Hook phase 接线命名：turn 包内 run{PhaseSuffix}Phase（未导出）；
// 仅 session 等外部包需要时导出 Run{PhaseSuffix}Phase；底层统一 runPhase（hook_host.go）。

func (o *Orchestrator) runMessageEnqueuedPhase(ctx context.Context, sessionID string, history *[]llm.Message, content string, metadata map[string]any) {
	if o == nil || o.toolHooks == nil {
		return
	}
	hc := &hooks.Context{
		Phase:     hooks.PhaseMessageEnqueued,
		SessionID: sessionID,
		AgentID:   o.agentID,
		Metadata:  metadata,
		MessageEnqueued: &hooks.MessageEnqueuedPayload{
			Content:  content,
			Metadata: metadata,
		},
	}
	_, _ = o.runPhase(ctx, hooks.PhaseMessageEnqueued, hc, sessionID, history, "")
}

func (o *Orchestrator) runTurnBeforeStepPhase(ctx context.Context, sessionID string, history *[]llm.Message, stepKind string, stepIndex int) {
	if o == nil || o.toolHooks == nil {
		return
	}
	o.setHookHostRuntime(stepIndex, false)
	hc := &hooks.Context{
		Phase:     hooks.PhaseTurnBeforeStep,
		SessionID: sessionID,
		AgentID:   o.agentID,
		TurnBeforeStep: &hooks.TurnBeforeStepPayload{
			StepKind: stepKind,
		},
	}
	_, _ = o.runPhase(ctx, hooks.PhaseTurnBeforeStep, hc, sessionID, history, "")
}

func (o *Orchestrator) runHITLBeforePausePhase(ctx context.Context, sessionID string, history *[]llm.Message, reason string) {
	if o == nil || o.toolHooks == nil {
		return
	}
	o.setHookHostRuntime(StepIndexFromContext(ctx), true)
	hc := &hooks.Context{
		Phase:     hooks.PhaseHITLBeforePause,
		SessionID: sessionID,
		AgentID:   o.agentID,
		HITLBeforePause: &hooks.HITLBeforePausePayload{
			Reason: reason,
		},
	}
	_, _ = o.runPhase(ctx, hooks.PhaseHITLBeforePause, hc, sessionID, history, reason)
}

func (o *Orchestrator) runHITLAfterResumePhase(ctx context.Context, sessionID string, history *[]llm.Message, resumeKind string) {
	if o == nil || o.toolHooks == nil {
		return
	}
	hc := &hooks.Context{
		Phase:     hooks.PhaseHITLAfterResume,
		SessionID: sessionID,
		AgentID:   o.agentID,
		HITLAfterResume: &hooks.HITLAfterResumePayload{
			ResumeKind: resumeKind,
		},
	}
	_, _ = o.runPhase(ctx, hooks.PhaseHITLAfterResume, hc, sessionID, history, "")
}

func (o *Orchestrator) runTurnErrorPhase(ctx context.Context, sessionID string, history *[]llm.Message, err error) {
	if o == nil || o.toolHooks == nil || err == nil {
		return
	}
	hc := &hooks.Context{
		Phase:     hooks.PhaseTurnError,
		SessionID: sessionID,
		AgentID:   o.agentID,
		TurnError: &hooks.TurnErrorPayload{Err: err},
	}
	_, _ = o.runPhase(ctx, hooks.PhaseTurnError, hc, sessionID, history, "error")
}

func (o *Orchestrator) runTurnCancelPhase(ctx context.Context, sessionID string, history *[]llm.Message, reason string) {
	if o == nil || o.toolHooks == nil {
		return
	}
	hc := &hooks.Context{
		Phase:     hooks.PhaseTurnCancel,
		SessionID: sessionID,
		AgentID:   o.agentID,
		TurnCancel: &hooks.TurnCancelPayload{
			Reason: reason,
		},
	}
	_, _ = o.runPhase(ctx, hooks.PhaseTurnCancel, hc, sessionID, history, "cancelled")
}

// RunTurnBeforeCompressPhase 供 session 包在步前压缩前触发 turn.before_compress。
func (o *Orchestrator) RunTurnBeforeCompressPhase(ctx context.Context, sessionID string, history *[]llm.Message, skip bool) bool {
	if o == nil || o.toolHooks == nil {
		return skip
	}
	hc := &hooks.Context{
		Phase:     hooks.PhaseTurnBeforeCompress,
		SessionID: sessionID,
		AgentID:   o.agentID,
		TurnBeforeCompress: &hooks.TurnBeforeCompressPayload{
			SkipCompress: skip,
		},
	}
	out, err := o.runPhase(ctx, hooks.PhaseTurnBeforeCompress, hc, sessionID, history, "")
	if err != nil || out.TurnBeforeCompress == nil {
		return skip
	}
	return out.TurnBeforeCompress.SkipCompress
}

// RunSessionLifecyclePhase 供 session 包在 create/restore 时触发 session.lifecycle。
func (o *Orchestrator) RunSessionLifecyclePhase(ctx context.Context, sessionID, event string) {
	if o == nil || o.toolHooks == nil {
		return
	}
	hc := &hooks.Context{
		Phase:     hooks.PhaseSessionLifecycle,
		SessionID: sessionID,
		AgentID:   o.agentID,
		SessionLifecycle: &hooks.SessionLifecyclePayload{
			Event: event,
		},
	}
	_, _ = o.runPhase(ctx, hooks.PhaseSessionLifecycle, hc, sessionID, nil, "")
}
