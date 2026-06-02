// Package llm 封装 OpenAI 兼容 Chat Completions 流式调用。
package llm

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// Message 为 OpenAI chat message（支持 tool call / tool result / reasoning）。
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	Name             string     `json:"name,omitempty"`
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
	OnDelta           func(delta string)
	OnReasoningDelta  func(delta string)
	OnUsage           func(usage Usage)
}

// CompleteRequest 为非流式补全请求（摘要压缩等）。
type CompleteRequest struct {
	SystemPrompt string
	UserPrompt   string
}

// Client 为可替换的 LLM 客户端（生产 OpenAI / 测试 Mock）。
type Client interface {
	StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error)
	CompleteText(ctx context.Context, req CompleteRequest) (string, error)
}
