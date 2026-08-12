package llm

import (
	"context"
)

// MockClient 用于单测与无 API Key 联调。
type MockClient struct {
	Prefix     string
	FixedReply string
	// EnableTools 为 true 时：首轮返回 read_file tool call，次轮返回工具结果摘要。
	EnableTools bool
	adapter     MessageAdapter
	callCount   int
}

func (m *MockClient) NormalizeAssistant(existing []Message, msg Message) Message {
	if m.adapter == nil {
		m.adapter = openAIAdapter{}
	}
	return m.adapter.NormalizeAssistantForStorage(existing, msg, nil)
}

// StubNormalizeAssistant 供测试 mock 实现 llm.Client 时复用。
func StubNormalizeAssistant(existing []Message, msg Message) Message {
	return NewMessageAdapter("openai").NormalizeAssistantForStorage(existing, msg, nil)
}

// StreamChat 模拟流式输出；EnableTools 时驱动工具循环测试。
func (m *MockClient) StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error) {
	m.callCount++

	if m.EnableTools && len(req.Tools) > 0 {
		if m.hasToolResult(req.Messages) {
			text := "已读取文件"
			m.streamText(ctx, text, handler)
			return ChatResult{Content: text, FinishReason: "stop"}, nil
		}
		tc := ToolCall{
			ID:   "call-mock-1",
			Type: "function",
			Function: ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"hello.txt"}`,
			},
		}
		return ChatResult{ToolCalls: []ToolCall{tc}, FinishReason: "tool_calls"}, nil
	}

	text := m.FixedReply
	if text == "" {
		var lastUser string
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				lastUser = MessageTextSummary(req.Messages[i])
				break
			}
		}
		text = m.Prefix + lastUser
	}
	m.streamText(ctx, text, handler)
	return ChatResult{Content: text, FinishReason: "stop"}, nil
}

func (m *MockClient) hasToolResult(messages []Message) bool {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "tool" {
			return true
		}
		if messages[i].Role == "user" {
			return false
		}
	}
	return false
}

func (m *MockClient) CompleteText(_ context.Context, req CompleteRequest) (string, error) {
	return "任务目标：压缩摘要\n重要结论：mock\n修改过的文件和资源：无\n下一步动作：继续", nil
}

func (m *MockClient) streamText(ctx context.Context, text string, handler StreamHandler) {
	runes := []rune(text)
	const chunk = 8
	for i := 0; i < len(runes); i += chunk {
		select {
		case <-ctx.Done():
			return
		default:
		}
		end := i + chunk
		if end > len(runes) {
			end = len(runes)
		}
		part := string(runes[i:end])
		if handler.OnDelta != nil {
			handler.OnDelta(part)
		}
	}
	if handler.OnUsage != nil {
		handler.OnUsage(Usage{PromptTokens: 1, CompletionTokens: len(runes), TotalTokens: 1 + len(runes)})
	}
}
