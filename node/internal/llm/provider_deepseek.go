package llm

import (
	"fmt"
	"log/slog"
	"strings"
)

// deepSeekAdapter 对齐 DeepSeek Thinking / Reasoner 文档：
// - 响应含 reasoning_content；带 tool_calls 的 assistant 必须保留并回传；
// - 纯对话 assistant（无 tool_calls）出站时移除 reasoning_content；
// - 异步 tool_callback 合成消息缺 reasoning 时从最近 assistant 继承。
type deepSeekAdapter struct{}

func (deepSeekAdapter) Name() ProviderName { return ProviderDeepSeek }

// NormalizeAssistantForStorage 规范化 assistant 消息，存储时移除 reasoning_content
func (deepSeekAdapter) NormalizeAssistantForStorage(existing []Message, msg Message, logger *slog.Logger) Message {
	normalized := cloneMessage(msg)
	if normalized.Role != "assistant" || len(normalized.ToolCalls) == 0 {
		return normalized
	}
	if strings.TrimSpace(normalized.ReasoningContent) != "" {
		return normalized
	}
	if isToolCallbackMessage(normalized) {
		normalized.ReasoningContent = latestAssistantReasoningContent(existing)
	} else if logger != nil {
		defaultAdapterLogger(logger).Warn("assistant tool_calls message missing reasoning_content; writing empty fallback")
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
			if strings.TrimSpace(out[i].ReasoningContent) == "" {
				return nil, fmt.Errorf("deepseek: assistant message[%d] has tool_calls but missing reasoning_content", i)
			}
			continue
		}
		out[i].ReasoningContent = ""
	}
	return out, nil
}

// RequestExtra 请求额外参数
func (deepSeekAdapter) RequestExtra() map[string]any {
	return map[string]any{
		"thinking":         map[string]string{"type": "enabled"},
		"reasoning_effort": "high",
	}
}
