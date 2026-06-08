package turn

import "github.com/DGS-ai-team/DAgents/node/internal/llm"

const ToolUserInterruptedMessage = "用户需要补充信息，打断了工具执行。"

// ToolStreamInterruptedMessage 为流式 assistant 输出或工具执行被 cancel 时的 tool 结果文案。
const ToolStreamInterruptedMessage = "流式输出被用户中断。"

// HITLKind 表示暂停等待的 HITL 类型。
type HITLKind string

const (
	HITLApproval         HITLKind = "approval"
	HITLUserInformation  HITLKind = "user_information"
)

// PendingHITL 保存分阶段 HITL 暂停时的待处理 tool call。
type PendingHITL struct {
	Kind      HITLKind       `json:"kind"`
	ToolCalls []llm.ToolCall `json:"tool_calls,omitempty"`
	UserInfo  *llm.ToolCall  `json:"user_info,omitempty"`
}

// AllToolCalls 返回当前 pending 对应的 tool call 列表（用于打断补位）。
func (p *PendingHITL) AllToolCalls() []llm.ToolCall {
	if p == nil {
		return nil
	}
	if p.Kind == HITLApproval {
		return append([]llm.ToolCall(nil), p.ToolCalls...)
	}
	if p.Kind == HITLUserInformation && p.UserInfo != nil {
		return []llm.ToolCall{*p.UserInfo}
	}
	return nil
}
