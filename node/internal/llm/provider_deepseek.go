package llm

import (
	"log/slog"
	"strings"
)

// deepSeekAdapter 对齐 DeepSeek Thinking / Reasoner 文档：
// - 响应含 reasoning_content；带 tool_calls 的 assistant 必须保留并回传（键须存在，值可为空）；
// - 纯对话 assistant（无 tool_calls）出站时移除 reasoning_content。
type deepSeekAdapter struct{}

func (deepSeekAdapter) Name() ProviderName { return ProviderDeepSeek }

// NormalizeAssistantForStorage 规范化 assistant 消息，存储时移除 reasoning_content
func (deepSeekAdapter) NormalizeAssistantForStorage(_ []Message, msg Message, logger *slog.Logger) Message {
	normalized := cloneMessage(msg)
	if normalized.Role != "assistant" || len(normalized.ToolCalls) == 0 {
		return normalized
	}
	if strings.TrimSpace(normalized.ReasoningContent) != "" {
		return normalized
	}
	if len(normalized.ToolCalls) > 0 && logger != nil {
		defaultAdapterLogger(logger).Warn("assistant tool_calls message has empty reasoning_content")
	}
	return normalized
}

// PrepareOutboundMessages 准备 outbound messages，移除 reasoning_content
func (deepSeekAdapter) PrepareOutboundMessages(messages []Message) ([]Message, error) {
	out := make([]Message, len(messages))
	for i, m := range messages {
		out[i] = cloneMessage(m)
		if out[i].Role != "assistant" {
			continue
		}
		if len(out[i].ToolCalls) > 0 {
			continue
		}
		out[i].ReasoningContent = ""
	}
	return out, nil
}

// MarshalChatRequestMessages 为 DeepSeek thinking+tools 序列化出站 messages。
func (deepSeekAdapter) MarshalChatRequestMessages(messages []Message) ([]map[string]any, bool, error) {
	out := make([]map[string]any, len(messages))
	for i, m := range messages {
		payload, err := MessageToDeepSeekAPIPayload(m)
		if err != nil {
			return nil, false, err
		}
		out[i] = payload
	}
	return out, true, nil
}

// RequestExtra 已由 RuntimeSettings.BuildRequestExtra 在出站时注入；适配器层返回 nil。
func (deepSeekAdapter) RequestExtra() map[string]any { return nil }
