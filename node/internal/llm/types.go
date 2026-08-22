// Package llm 封装 OpenAI 兼容 Chat Completions 流式调用。
package llm

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// Message 为 OpenAI chat message（支持 tool call / tool result / reasoning / 多模态 user）。
//
// 纯文本时仅使用 Content；多模态 user 消息使用 ContentParts，Content 为 text part 摘要。
type Message struct {
	Role             string        `json:"role"`
	Content          string        `json:"content,omitempty"`
	ContentParts     []ContentPart `json:"content_parts,omitempty"`
	ReasoningContent string        `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall    `json:"tool_calls,omitempty"`
	ToolCallID       string        `json:"tool_call_id,omitempty"`
	Name             string        `json:"name,omitempty"`
	// ToolResultMetadata is persisted with internal history but is never sent
	// as a non-standard provider message field.  The outbound preparation
	// layer renders it into the tool content so the model can observe the
	// authoritative status without changing the UI/transcript body.
	ToolResultMetadata *ToolResultMetadata `json:"tool_result_metadata,omitempty"`
}

// ToolResultMetadata is the compact model-facing projection of the runtime
// tool_result envelope.  Tool-specific content remains in Message.Content.
type ToolResultMetadata struct {
	Status string                 `json:"status"`
	Error  *ToolResultErrorDetail `json:"error,omitempty"`
}

type ToolResultErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// ToolCall 为 assistant 消息中的 tool_calls 项。
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction 为 function 类型 tool call 载荷。
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatRequest 为一次 completion 请求。
type ChatRequest struct {
	SystemPrompt string
	Messages     []Message
	Tools        []tools.ToolDef
	// APIMessages 非空时直接作为 HTTP 请求体 messages 字段（已由 MessageAdapter 序列化，通常已含 system）。
	APIMessages []map[string]any
}

// ChatResult 为一次 completion 聚合结果。
type ChatResult struct {
	Content          string
	ReasoningContent string
	ToolCalls        []ToolCall
	FinishReason     string
}

// StreamHandler 接收流式 delta 与最终 usage。
type StreamHandler struct {
	OnDelta          func(delta string)
	OnReasoningDelta func(delta string)
	OnToolCallDelta  func(toolCalls []ToolCall) // 流式 tool_calls 增量快照（可能不完整）
	OnUsage          func(usage Usage)
}

// CompleteRequest 为非流式补全请求（摘要压缩等）。
type CompleteRequest struct {
	SystemPrompt string
	UserPrompt   string
}

// Client 为可替换的 LLM 客户端（生产 OpenAI / DeepSeek / 测试 Mock）。
type Client interface {
	StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error)
	CompleteText(ctx context.Context, req CompleteRequest) (string, error)
	// NormalizeAssistant 写入 session history 前规范化 assistant 消息（含 reasoning_content 策略）。
	NormalizeAssistant(existing []Message, msg Message) Message
}
