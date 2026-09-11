package turn

// RuntimeToolMessageContent 为 tool_message 回合占位 content（对齐 Python orchestrator）。
const RuntimeToolMessageContent = "tool_message"

// StepOutcome 为单步 turn（一次模型请求 + 可选工具批处理）的结果。
type StepOutcome struct {
	Pending            *PendingHITL
	StepIndex          int
	ScheduleToolResult bool
	Err                error
	// ConditionHandled marks a synthetic approval step that executed a
	// condition without making a model request. Runtime lifecycle code settles
	// its tool execution directly and must not schedule an LLM continuation.
	ConditionHandled     bool
	ConditionMatched     bool
	ConditionToolCallID  string
	ConditionExecutionID string
	ConditionResult      string
	// NoWork marks the trusted auto_idle control tool; it is never inferred
	// from assistant text or from an ordinary successful turn.
	NoWork bool
}
