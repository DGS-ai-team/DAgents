package turn

import "github.com/DGS-ai-team/DAgents/node/internal/llm"

// TaskPhase 描述 session 旁路/续跑相关的完成阶段。
type TaskPhase string

const (
	TaskPhaseComplete     TaskPhase = "complete"
	TaskPhaseAwaitingHITL TaskPhase = "awaiting_hitl"
	TaskPhaseToolLoop     TaskPhase = "tool_loop"
	TaskPhaseOpenBatch    TaskPhase = "open_batch"
	TaskPhaseOther        TaskPhase = "other"
)

// TaskComplete 判定当前 session 是否处于可安全开旁路续跑 / apply 旁路的稳定完成态。
func TaskComplete(messages []llm.Message, pending *PendingHITL) bool {
	return TaskPhaseOf(messages, pending) == TaskPhaseComplete
}

// TaskPhaseOf 返回任务阶段（供 barrier / 日志 / GET context）。
func TaskPhaseOf(messages []llm.Message, pending *PendingHITL) TaskPhase {
	if pending != nil {
		return TaskPhaseAwaitingHITL
	}
	if len(unrespondedToolCallsAfterLastAssistant(messages)) > 0 {
		return TaskPhaseOpenBatch
	}
	switch classifyToolResultTail(messages) {
	case tailAssistantWithoutToolCalls:
		return TaskPhaseComplete
	case tailTool:
		return TaskPhaseToolLoop
	case tailAssistantWithToolCalls:
		return TaskPhaseOpenBatch
	default:
		return TaskPhaseOther
	}
}
