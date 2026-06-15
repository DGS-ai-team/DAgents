package hooks

import "context"

// ToolAfterEachInput 为 tool 执行后 Hook 链输入。
type ToolAfterEachInput struct {
	SessionID     string
	ToolCallID    string
	ToolName      string
	ToolArgs      map[string]any
	RawArguments  string
	RawResult     string
}

// ToolAfterEachOutput 为 Hook 链对 tool 结果的拆分（Client 全文 vs history 摘要）。
type ToolAfterEachOutput struct {
	ForClient  string
	ForHistory string
	SpillPath  string
}

// ToolAfterEachHook 在工具执行后处理结果（如摘要、落盘）。
type ToolAfterEachHook interface {
	Name() string
	Phases() []Phase
	RunToolAfterEach(ctx context.Context, in ToolAfterEachInput, out *ToolAfterEachOutput) error
}
