package turn

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// ExecutionGuard checks the latest execution policy immediately before a tool
// is scheduled. It intentionally does not carry the ModelContextSnapshot:
// policy, Linux channel state and credentials are safety state and must remain
// live even while a Turn keeps its prompt/tools stable for cache reuse.
type ExecutionGuard interface {
	Check(context.Context, string, *[]llm.Message, llm.ToolCall) hooks.ToolBeforeEachResult
}

type executionGuardFunc func(context.Context, string, *[]llm.Message, llm.ToolCall) hooks.ToolBeforeEachResult

func (f executionGuardFunc) Check(ctx context.Context, sessionID string, history *[]llm.Message, call llm.ToolCall) hooks.ToolBeforeEachResult {
	return f(ctx, sessionID, history, call)
}
