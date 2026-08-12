package llm

import (
	"log/slog"
	"strings"
)

// openAIAdapter 为通用 OpenAI 兼容网关。
// 当 RuntimeSettings 注入 thinking 时，出站规则对齐 DeepSeek：
// - 带 tool_calls 的 assistant 保留 reasoning_content（键须存在，值可为空）；
// - 纯对话 assistant 出站剥离 reasoning_content。
type openAIAdapter struct{}

func (openAIAdapter) Name() ProviderName { return ProviderOpenAI }

func (openAIAdapter) NormalizeAssistantForStorage(_ []Message, msg Message, logger *slog.Logger) Message {
	normalized := cloneMessage(msg)
	if normalized.Role != "assistant" || len(normalized.ToolCalls) == 0 {
		return normalized
	}
	if strings.TrimSpace(normalized.ReasoningContent) != "" {
		return normalized
	}
	if logger != nil {
		defaultAdapterLogger(logger).Warn("assistant tool_calls message has empty reasoning_content")
	}
	return normalized
}

func (openAIAdapter) PrepareOutboundMessages(messages []Message) ([]Message, error) {
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

func (openAIAdapter) MarshalChatRequestMessages(messages []Message) ([]map[string]any, bool, error) {
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
