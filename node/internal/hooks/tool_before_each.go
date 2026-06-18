package hooks

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// ToolBeforeEachInput 为 tool 执行前 Hook 链输入。
type ToolBeforeEachInput struct {
	SessionID     string
	ToolName      string
	ToolArgs      map[string]any
	RawArguments  string
}

// ToolBeforeEachResult 为 Hook 链对单条 tool call 的决策结果。
type ToolBeforeEachResult struct {
	Action          policy.Action
	ToolMode        policy.ApprovalMode
	ApprovalReason  string
	ApprovalSubtype string
	DuplicateMeta   *DuplicateMeta
}

// ToolBeforeEachHook 在工具执行前参与决策。
type ToolBeforeEachHook interface {
	Name() string
	Phases() []Phase
	RunToolBeforeEach(ctx context.Context, in ToolBeforeEachInput, out *ToolBeforeEachResult) error
}
