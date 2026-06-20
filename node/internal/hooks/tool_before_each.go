package hooks

import (
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// ToolBeforeEachInput 为 tool.before_each 输入（Orchestrator → RunPhase）。
type ToolBeforeEachInput struct {
	SessionID    string
	ToolName     string
	ToolArgs     map[string]any
	RawArguments string
}

// ToolBeforeEachResult 为 tool.before_each 链对单条 tool call 的决策结果。
type ToolBeforeEachResult struct {
	Action          policy.Action
	ToolMode        policy.ApprovalMode
	ApprovalReason  string
	ApprovalSubtype string
	DuplicateMeta   *DuplicateMeta
}
