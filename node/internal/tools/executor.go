package tools

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// Executor 为 turn 编排调用的工具后端；*Registry 与 childagent.RestrictedRegistry 均实现。
type Executor interface {
	Definitions() []ToolDef
	Execute(ctx context.Context, name, arguments string) (string, error)
	TakeBashCompressStatsForCall(toolCallID string) map[string]any
	TakeToolResultMediaForCall(toolCallID string) map[string]any
	TakeReadImageVisionForCall(toolCallID string) *ReadImageVisionPayload
}

// ToolPreflightDecision is a provider-specific safety decision evaluated
// before a tool is scheduled. It deliberately contains no connection or
// credential data; the provider must repeat the check at the execution edge.
type ToolPreflightDecision struct {
	Action         policy.Action
	ApprovalReason string
}

// ToolPreflight lets an executor contribute live target policy without
// changing the model-visible tool schema. It is optional so restricted/test
// executors can keep the existing Executor contract.
type ToolPreflight interface {
	PreflightTool(context.Context, string, map[string]any) (ToolPreflightDecision, bool)
}

// ToolRetryPolicy lets an executor explicitly identify tools whose failed
// transport/read attempt may be retried without risking a duplicate side
// effect. The lifecycle layer still records every retry against the same
// ToolCall/ToolExecution identity.
type ToolRetryPolicy interface {
	ToolRetryAllowed(name string) bool
}

// Ensure Registry implements Executor.
var _ Executor = (*Registry)(nil)
