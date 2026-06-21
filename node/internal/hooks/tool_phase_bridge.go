package hooks

import (
	"context"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

const (
	toolBeforeEachPriorityPolicy     = 0
	toolBeforeEachPriorityAgentOwned = 10
	toolBeforeEachPriorityDuplicate  = 20

	toolAfterEachPriorityResult     = 0
	toolAfterEachPriorityAgentOwned = 10
)

// DefaultToolBeforeEachResult 返回保守默认决策（require_approval + rule）。
func DefaultToolBeforeEachResult() ToolBeforeEachResult {
	return ToolBeforeEachResult{
		Action:   policy.ActionRequireApproval,
		ToolMode: policy.ModeRule,
	}
}

func defaultToolBeforeEachResult() ToolBeforeEachResult {
	return DefaultToolBeforeEachResult()
}

func defaultToolAfterEachOutput(raw string) ToolAfterEachOutput {
	return ToolAfterEachOutput{
		ForClient:  raw,
		ForHistory: raw,
	}
}

func ensureToolDecision(hc *Context) *ToolBeforeEachResult {
	if hc.ToolDecision == nil {
		d := defaultToolBeforeEachResult()
		hc.ToolDecision = &d
	}
	return hc.ToolDecision
}

func ensureToolAfterEachOutput(hc *Context) *ToolAfterEachOutput {
	if hc.ToolAfterEachOutput == nil {
		raw := ""
		if hc.ToolAfterEach != nil {
			raw = hc.ToolAfterEach.RawResult
		}
		out := defaultToolAfterEachOutput(raw)
		hc.ToolAfterEachOutput = &out
	}
	return hc.ToolAfterEachOutput
}

func toolBeforeEachInputFromContext(hc *Context) (ToolBeforeEachInput, error) {
	if hc == nil || hc.ToolBeforeEach == nil {
		return ToolBeforeEachInput{}, fmt.Errorf("hooks: missing ToolBeforeEach payload")
	}
	return ToolBeforeEachInput{
		SessionID:    hc.SessionID,
		ToolName:     hc.ToolBeforeEach.ToolName,
		ToolArgs:     hc.ToolBeforeEach.ToolArgs,
		RawArguments: hc.ToolBeforeEach.RawArguments,
	}, nil
}

func toolAfterEachInputFromContext(hc *Context) (ToolAfterEachInput, error) {
	if hc == nil || hc.ToolAfterEach == nil {
		return ToolAfterEachInput{}, fmt.Errorf("hooks: missing ToolAfterEach payload")
	}
	return ToolAfterEachInput{
		SessionID:    hc.SessionID,
		ToolCallID:   hc.ToolAfterEach.ToolCallID,
		ToolName:     hc.ToolAfterEach.ToolName,
		ToolArgs:     hc.ToolAfterEach.ToolArgs,
		RawArguments: hc.ToolAfterEach.RawArguments,
		RawResult:    hc.ToolAfterEach.RawResult,
	}, nil
}

func contextFromToolBeforeEachInput(in ToolBeforeEachInput) *Context {
	decision := defaultToolBeforeEachResult()
	return &Context{
		Phase:     PhaseToolBeforeEach,
		SessionID: in.SessionID,
		ToolBeforeEach: &ToolBeforeEachPayload{
			ToolName:     in.ToolName,
			ToolArgs:     in.ToolArgs,
			RawArguments: in.RawArguments,
		},
		ToolDecision: &decision,
	}
}

func contextFromToolAfterEachInput(in ToolAfterEachInput) *Context {
	out := defaultToolAfterEachOutput(in.RawResult)
	return &Context{
		Phase:     PhaseToolAfterEach,
		SessionID: in.SessionID,
		ToolAfterEach: &ToolAfterEachPayload{
			ToolCallID:   in.ToolCallID,
			ToolName:     in.ToolName,
			ToolArgs:     in.ToolArgs,
			RawArguments: in.RawArguments,
			RawResult:    in.RawResult,
		},
		ToolAfterEachOutput: &out,
	}
}

func toolBeforeEachResultFromContext(hc Context) ToolBeforeEachResult {
	if hc.ToolDecision != nil {
		return *hc.ToolDecision
	}
	return defaultToolBeforeEachResult()
}

func toolAfterEachOutputFromContext(hc Context) ToolAfterEachOutput {
	if hc.ToolAfterEachOutput != nil {
		return *hc.ToolAfterEachOutput
	}
	raw := ""
	if hc.ToolAfterEach != nil {
		raw = hc.ToolAfterEach.RawResult
	}
	return defaultToolAfterEachOutput(raw)
}

// BuildToolBeforeEachContext 构造 tool.before_each 的 RunPhase 上下文。
func BuildToolBeforeEachContext(in ToolBeforeEachInput) *Context {
	return contextFromToolBeforeEachInput(in)
}

// ToolBeforeEachDecisionFrom 从 RunPhase 返回的 Context 读取 tool 决策。
func ToolBeforeEachDecisionFrom(hc Context) ToolBeforeEachResult {
	return toolBeforeEachResultFromContext(hc)
}

// BuildToolAfterEachContext 构造 tool.after_each 的 RunPhase 上下文。
func BuildToolAfterEachContext(in ToolAfterEachInput) *Context {
	return contextFromToolAfterEachInput(in)
}

// ToolAfterEachOutputFrom 从 RunPhase 返回的 Context 读取 tool 结果拆分。
func ToolAfterEachOutputFrom(hc Context) ToolAfterEachOutput {
	return toolAfterEachOutputFromContext(hc)
}

// BuildPromptBuildContext 构造 prompt.build 的 RunPhase 上下文。
func BuildPromptBuildContext(sessionID, agentID, systemPrompt string) *Context {
	return &Context{
		Phase:     PhasePromptBuild,
		SessionID: sessionID,
		AgentID:   agentID,
		PromptBuild: &PromptBuildPayload{
			SystemPrompt: systemPrompt,
		},
	}
}

// SystemPromptFrom 从 RunPhase 返回的 Context 读取 system prompt；空则回退 fallback。
func SystemPromptFrom(hc Context, fallback string) string {
	if hc.PromptBuild != nil && hc.PromptBuild.SystemPrompt != "" {
		return hc.PromptBuild.SystemPrompt
	}
	return fallback
}

// BuildTurnDoneContext 构造 turn.done 的 RunPhase 上下文。
func BuildTurnDoneContext(sessionID, agentID, finishReason string) *Context {
	return &Context{
		Phase:     PhaseTurnDone,
		SessionID: sessionID,
		AgentID:   agentID,
		TurnDone:  &TurnDonePayload{FinishReason: finishReason},
	}
}

// BuildLLMAfterCallContext 构造 llm.after_call 的 RunPhase 上下文。
func BuildLLMAfterCallContext(sessionID, agentID string, result LLMAfterCallInput) *Context {
	return &Context{
		Phase:     PhaseLLMAfterCall,
		SessionID: sessionID,
		AgentID:   agentID,
		Metadata: map[string]any{
			"finish_reason": result.FinishReason,
		},
		LLMAfterCall: &LLMAfterCallPayload{
			AssistantContent: result.AssistantContent,
			ToolCalls:        append([]llm.ToolCall(nil), result.ToolCalls...),
		},
	}
}

// LLMAfterCallInput 为 llm.after_call 锚点输入。
type LLMAfterCallInput struct {
	AssistantContent string
	ToolCalls        []llm.ToolCall
	FinishReason     string
}

// ApplyLLMAfterCallToResult 将 RunPhase 后的 Context 合并回 ChatResult 输入。
func ApplyLLMAfterCallToResult(hc Context, result LLMAfterCallInput) LLMAfterCallInput {
	if hc.LLMAfterCall == nil {
		return result
	}
	out := result
	out.AssistantContent = hc.LLMAfterCall.AssistantContent
	if len(hc.LLMAfterCall.ToolCalls) > 0 {
		out.ToolCalls = append([]llm.ToolCall(nil), hc.LLMAfterCall.ToolCalls...)
	}
	return out
}

func runToolBeforeEachHook(
	ctx context.Context,
	hc *Context,
	name string,
	fn func(context.Context, ToolBeforeEachInput, *ToolBeforeEachResult) error,
) (Result, error) {
	in, err := toolBeforeEachInputFromContext(hc)
	if err != nil {
		return Result{}, fmt.Errorf("hooks: %q: %w", name, err)
	}
	out := ensureToolDecision(hc)
	if err := fn(ctx, in, out); err != nil {
		return Result{}, err
	}
	return Result{Action: ActionContinue}, nil
}

func runToolAfterEachHook(
	ctx context.Context,
	hc *Context,
	name string,
	fn func(context.Context, ToolAfterEachInput, *ToolAfterEachOutput) error,
) (Result, error) {
	in, err := toolAfterEachInputFromContext(hc)
	if err != nil {
		return Result{}, fmt.Errorf("hooks: %q: %w", name, err)
	}
	out := ensureToolAfterEachOutput(hc)
	if err := fn(ctx, in, out); err != nil {
		return Result{}, err
	}
	return Result{Action: ActionContinue}, nil
}

func registerBuiltinToolBeforeEachHooks(r *Registry, ph *PolicyToolHook, ah *AgentOwnedFileHook, dh *DuplicateToolCallHook) {
	if r == nil {
		return
	}
	opts := RegisterOpts{Timeout: DefaultInlineHookTimeout, OnError: OnErrorContinue}
	if ph != nil {
		o := opts
		o.Priority = toolBeforeEachPriorityPolicy
		r.RegisterPhaseHook(ph, o)
	}
	if ah != nil {
		o := opts
		o.Priority = toolBeforeEachPriorityAgentOwned
		r.RegisterPhaseHook(ah, o)
	}
	if dh != nil {
		o := opts
		o.Priority = toolBeforeEachPriorityDuplicate
		r.RegisterPhaseHook(dh, o)
	}
}

func registerBuiltinToolAfterEachHooks(r *Registry, rh *ToolResultPackageHook, aah *AgentOwnedFileAfterHook) {
	if r == nil {
		return
	}
	opts := RegisterOpts{Timeout: DefaultInlineHookTimeout, OnError: OnErrorContinue}
	if rh != nil {
		o := opts
		o.Priority = toolAfterEachPriorityResult
		r.RegisterPhaseHook(rh, o)
	}
	if aah != nil {
		o := opts
		o.Priority = toolAfterEachPriorityAgentOwned
		r.RegisterPhaseHook(aah, o)
	}
}
