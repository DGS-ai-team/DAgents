package turn

// RuntimeToolMessageContent 为 tool_message 回合占位 content（对齐 Python orchestrator）。
const RuntimeToolMessageContent = "tool_message"

// StepOutcome 为单步 turn（一次模型请求 + 可选工具批处理）的结果。
type StepOutcome struct {
	Pending            *PendingHITL
	StepIndex          int
	ScheduleToolResult bool
	Err                error
}
