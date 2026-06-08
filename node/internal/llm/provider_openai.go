package llm

import "log/slog"

// openAIAdapter 为通用 OpenAI 兼容网关：出站剥离 reasoning_content，入站 assistant 原样存储。
type openAIAdapter struct{}

func (openAIAdapter) Name() ProviderName { return ProviderOpenAI }

func (openAIAdapter) NormalizeAssistantForStorage(_ []Message, msg Message, _ *slog.Logger) Message {
	return cloneMessage(msg)
}

func (openAIAdapter) PrepareOutboundMessages(messages []Message) ([]Message, error) {
	out := make([]Message, len(messages))
	for i, m := range messages {
		out[i] = cloneMessage(m)
		if out[i].Role == "assistant" {
			out[i].ReasoningContent = ""
		}
	}
	return out, nil
}

func (openAIAdapter) RequestExtra() map[string]any { return nil }
