package turn

import (
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

const ToolUserInterruptedMessage = "用户需要补充信息，打断了工具执行。"

// ToolStreamInterruptedMessage 为流式 assistant 输出或工具执行被 cancel 时的 tool 结果文案。
const ToolStreamInterruptedMessage = "流式输出被用户中断。"

const (
	hitlTypeUserInformation = "user_information"
	hitlTypeExecuteTool     = "execute_tool"
	hitlTypeMemoryConflict  = "memory_conflict"
)

// PendingHITLItem 为单条待 HITL 的 tool call（类型由 tool name 推断，不再单独区分 kind）。
type PendingHITLItem struct {
	ToolCall       llm.ToolCall         `json:"tool_call"`
	DuplicateMeta  *hooks.DuplicateMeta `json:"duplicate_meta,omitempty"`
	MemoryConflict *MemoryConflictMeta  `json:"memory_conflict,omitempty"`
}

// PendingHITL 保存 HITL 暂停时的待处理 tool call 批次。
type PendingHITL struct {
	Items []PendingHITLItem `json:"items,omitempty"`
}

// AllToolCalls 返回当前 pending 对应的 tool call 列表（用于打断补位）。
func (p *PendingHITL) AllToolCalls() []llm.ToolCall {
	if p == nil || len(p.Items) == 0 {
		return nil
	}
	out := make([]llm.ToolCall, 0, len(p.Items))
	for _, item := range p.Items {
		out = append(out, item.ToolCall)
	}
	return out
}

func (p *PendingHITL) findItem(toolCallID string) (PendingHITLItem, int, bool) {
	if p == nil {
		return PendingHITLItem{}, -1, false
	}
	for i, item := range p.Items {
		if item.ToolCall.ID == toolCallID {
			return item, i, true
		}
	}
	return PendingHITLItem{}, -1, false
}

func (p *PendingHITL) withoutIndex(idx int) *PendingHITL {
	if p == nil || idx < 0 || idx >= len(p.Items) {
		return p
	}
	remaining := append([]PendingHITLItem(nil), p.Items[:idx]...)
	remaining = append(remaining, p.Items[idx+1:]...)
	if len(remaining) == 0 {
		return nil
	}
	return &PendingHITL{Items: remaining}
}

func (p *PendingHITL) approvalItems() []PendingHITLItem {
	if p == nil {
		return nil
	}
	out := make([]PendingHITLItem, 0, len(p.Items))
	for _, item := range p.Items {
		if tools.IsAskUserInformation(item.ToolCall.Function.Name) || item.MemoryConflict != nil {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (p *PendingHITL) userInformationItems() []PendingHITLItem {
	if p == nil {
		return nil
	}
	out := make([]PendingHITLItem, 0, len(p.Items))
	for _, item := range p.Items {
		if tools.IsAskUserInformation(item.ToolCall.Function.Name) {
			out = append(out, item)
		}
	}
	return out
}

func (p *PendingHITL) memoryConflictItems() []PendingHITLItem {
	if p == nil {
		return nil
	}
	out := make([]PendingHITLItem, 0, len(p.Items))
	for _, item := range p.Items {
		if item.MemoryConflict != nil {
			out = append(out, item)
		}
	}
	return out
}

func (p *PendingHITL) nonApprovalItems() []PendingHITLItem {
	if p == nil {
		return nil
	}
	out := make([]PendingHITLItem, 0, len(p.Items))
	for _, item := range p.Items {
		if tools.IsAskUserInformation(item.ToolCall.Function.Name) || item.MemoryConflict != nil {
			out = append(out, item)
		}
	}
	return out
}

func pendingFromItems(items []PendingHITLItem) *PendingHITL {
	if len(items) == 0 {
		return nil
	}
	return &PendingHITL{Items: append([]PendingHITLItem(nil), items...)}
}
