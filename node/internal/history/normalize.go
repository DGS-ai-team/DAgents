package history

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// NormalizeMessageForContext 规范化即将写入会话 history 的单条消息。

// 逻辑：
// 1. 深拷贝待写入消息，避免调用方对象被后续改动污染；
// 2. 非 assistant 或无 tool_calls 的消息直接返回；
// 3. assistant + tool_calls 统一保留 reasoning_content 字段；
// 4. 异步 tool_callback 缺字段时继承最近 assistant 的 reasoning。

// 关键分支：
// - 普通模型 tool_call 缺 reasoning 时补空串并打 warning；
// - 不修改 existing 与原始 message 引用。

// 副作用：无（返回新副本）。
func NormalizeMessageForContext(existing []llm.Message, message llm.Message, logger *slog.Logger) llm.Message {
	normalized := cloneMessage(message)
	if normalized.Role != "assistant" || len(normalized.ToolCalls) == 0 {
		return normalized
	}
	if strings.TrimSpace(normalized.ReasoningContent) != "" {
		return normalized
	}
	if isToolCallbackMessage(normalized) {
		normalized.ReasoningContent = latestAssistantReasoningContent(existing)
	} else if logger != nil {
		logger.Warn("assistant tool_calls message missing reasoning_content; writing empty fallback")
	}
	return normalized
}

func latestAssistantReasoningContent(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "assistant" {
			continue
		}
		return msg.ReasoningContent
	}
	return ""
}

func isToolCallbackMessage(msg llm.Message) bool {
	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == "tool_callback" {
			return true
		}
	}
	return false
}

func cloneMessage(message llm.Message) llm.Message {
	raw, err := json.Marshal(message)
	if err != nil {
		return message
	}
	var out llm.Message
	if err := json.Unmarshal(raw, &out); err != nil {
		return message
	}
	return out
}

// messageToJournalPayload 将消息转为 JSONL 行内 message 对象（assistant+tool_calls 保留 reasoning_content 键）。
func messageToJournalPayload(message llm.Message) map[string]any {
	raw, err := json.Marshal(message)
	if err != nil {
		return map[string]any{"role": message.Role, "content": message.Content}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{"role": message.Role, "content": message.Content}
	}
	if message.Role == "assistant" && len(message.ToolCalls) > 0 {
		payload["reasoning_content"] = message.ReasoningContent
	}
	return payload
}
