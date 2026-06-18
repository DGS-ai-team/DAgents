package llm

import (
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// CloneMessage 深拷贝一条 chat message（JSON round-trip）。
func CloneMessage(message Message) Message {
	raw, err := json.Marshal(message)
	if err != nil {
		return message
	}
	var out Message
	if err := json.Unmarshal(raw, &out); err != nil {
		return message
	}
	return out
}

// EstimateTextTokens 粗算纯文本 token（DeepSeek 字符权重），与 EstimateMessageTokens 一致。
func EstimateTextTokens(text string) int {
	return tokens.EstimateInt(text)
}

// EstimateMessageTokens 粗算 messages token（content/reasoning + 固定开销 + tool_calls 加权）。
// 供 compression 触发与 GET /context 共用，避免两处公式漂移。
func EstimateMessageTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += tokens.EstimateInt(m.Content) + 16
		if len(m.ToolCalls) > 0 {
			total += len(m.ToolCalls) * 32
		}
		if m.ReasoningContent != "" {
			total += tokens.EstimateInt(m.ReasoningContent)
		}
	}
	return total
}

// messageMapPayload 将 Message 转为 map，供出站 API / JSONL 二次加工。
func messageMapPayload(m Message) (map[string]any, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// EnsureToolCallsAssistantReasoningKey 为 assistant+tool_calls 确保 reasoning_content 键存在（值可为空）。
func EnsureToolCallsAssistantReasoningKey(payload map[string]any, m Message) {
	if m.Role == "assistant" && len(m.ToolCalls) > 0 {
		payload["reasoning_content"] = m.ReasoningContent
	}
}

// ApplyDeepSeekOutboundReasoning 按 DeepSeek 出站规则处理 reasoning_content 键。
func ApplyDeepSeekOutboundReasoning(payload map[string]any, m Message) {
	if m.Role == "assistant" && len(m.ToolCalls) > 0 {
		payload["reasoning_content"] = m.ReasoningContent
	} else {
		delete(payload, "reasoning_content")
	}
}

// MessageToDeepSeekAPIPayload 序列化单条消息为 DeepSeek chat/completions 请求形态。
func MessageToDeepSeekAPIPayload(m Message) (map[string]any, error) {
	payload, err := messageMapPayload(m)
	if err != nil {
		return nil, err
	}
	ApplyDeepSeekOutboundReasoning(payload, m)
	return payload, nil
}

// MessageToJournalPayload 将消息转为 JSONL 行内 message 对象（assistant+tool_calls 保留 reasoning_content 键）。
func MessageToJournalPayload(message Message) map[string]any {
	payload, err := messageMapPayload(message)
	if err != nil {
		return map[string]any{"role": message.Role, "content": message.Content}
	}
	EnsureToolCallsAssistantReasoningKey(payload, message)
	return payload
}
