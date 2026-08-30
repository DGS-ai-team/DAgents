package llm

import (
	"encoding/json"
	"strings"

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

// EstimateMessageTokens 粗算模型实际看到的 messages token（content/reasoning
// + 固定开销 + tool_calls 加权）。工具结果的请求侧 metadata 也计入，保证
// compression 与 GET /context 不会因 history 保留原始正文而低估模型输入。
func EstimateMessageTokens(messages []Message) int {
	messages = PrepareToolResultMessagesForModel(messages)
	total := 0
	for _, m := range messages {
		total += EstimateMessageContentTokens(m) + 16
		if len(m.ToolCalls) > 0 {
			total += len(m.ToolCalls) * 32
		}
		if m.ReasoningContent != "" {
			total += tokens.EstimateInt(m.ReasoningContent)
		}
	}
	return total
}

// MessageToAPIPayload 序列化单条消息为 OpenAI chat/completions messages[] 元素。
func MessageToAPIPayload(m Message) (map[string]any, error) {
	return messageToAPIPayload(messageWithFileReferencePrompt(m))
}

func messageToAPIPayload(m Message) (map[string]any, error) {
	payload := map[string]any{"role": m.Role}
	if n := strings.TrimSpace(m.Name); n != "" {
		payload["name"] = n
	}
	if len(m.ToolCalls) > 0 {
		payload["tool_calls"] = m.ToolCalls
	}
	if id := strings.TrimSpace(m.ToolCallID); id != "" {
		payload["tool_call_id"] = id
	}
	if len(m.ContentParts) > 0 {
		parts := make([]map[string]any, len(m.ContentParts))
		for i, part := range m.ContentParts {
			parts[i] = contentPartToMap(part)
		}
		payload["content"] = parts
	} else {
		payload["content"] = m.Content
	}
	return payload, nil
}

func messageWithFileReferencePrompt(m Message) Message {
	if m.Role != "user" || len(m.FileReferences) == 0 {
		return m
	}
	prompt := FileReferencePrompt(m.FileReferences)
	if prompt == "" {
		return m
	}
	if len(m.ContentParts) == 0 {
		m.Content = strings.TrimSpace(strings.Join([]string{prompt, m.Content}, "\n\n"))
		return m
	}
	parts := append([]ContentPart(nil), m.ContentParts...)
	for i := range parts {
		if parts[i].Type != "text" {
			continue
		}
		parts[i].Text = strings.TrimSpace(strings.Join([]string{prompt, parts[i].Text}, "\n\n"))
		m.Content = ""
		m.ContentParts = parts
		return m
	}
	parts = append([]ContentPart{{Type: "text", Text: prompt}}, parts...)
	m.Content = ""
	m.ContentParts = parts
	return m
}

// messageMapPayload 将 Message 转为 map，供出站 API / JSONL 二次加工。
func messageMapPayload(m Message) (map[string]any, error) {
	payload, err := messageToAPIPayload(m)
	if err != nil {
		return nil, err
	}
	if m.ReasoningContent != "" {
		payload["reasoning_content"] = m.ReasoningContent
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
	payload, err := MessageToAPIPayload(m)
	if err != nil {
		return nil, err
	}
	ApplyDeepSeekOutboundReasoning(payload, m)
	return payload, nil
}

// MessagesToAPIPayload serializes a complete message list without exposing
// internal source/provenance fields. The list may include a system message.
func MessagesToAPIPayload(messages []Message) ([]map[string]any, error) {
	out := make([]map[string]any, len(messages))
	for i, message := range messages {
		payload, err := MessageToDeepSeekAPIPayload(message)
		if err != nil {
			return nil, err
		}
		out[i] = payload
	}
	return out, nil
}

// MessageToJournalPayload 将消息转为 JSONL 行内 message 对象（assistant+tool_calls 保留 reasoning_content 键）。
func MessageToJournalPayload(message Message) map[string]any {
	payload, err := messageMapPayload(message)
	if err != nil {
		return map[string]any{"role": message.Role, "content": message.Content}
	}
	EnsureToolCallsAssistantReasoningKey(payload, message)
	if message.Source != nil {
		payload["source"] = message.Source
	}
	if message.Provenance != nil {
		payload["provenance"] = message.Provenance
	}
	if len(message.FileReferences) > 0 {
		payload["file_refs"] = message.FileReferences
	}
	if message.Role == "tool" && message.ToolResultMetadata != nil {
		payload["tool_result_metadata"] = message.ToolResultMetadata
	}
	return payload
}
