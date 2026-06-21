package hooks

// ToolAfterEachInput 为 tool.after_each 输入（Orchestrator → RunPhase）。
type ToolAfterEachInput struct {
	SessionID    string
	ToolCallID   string
	ToolName     string
	ToolArgs     map[string]any
	RawArguments string
	RawResult    string
}

// ToolAfterEachOutput 为 tool.after_each 链对 tool 结果的拆分（Client 全文 vs history 摘要）。
type ToolAfterEachOutput struct {
	ForClient  string
	ForHistory string
	SpillPath  string
}
