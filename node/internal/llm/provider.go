package llm

import (
	"encoding/json"
	"log/slog"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

// ProviderName 为 config.yaml llm.provider 取值。
type ProviderName string

const (
	ProviderOpenAI   ProviderName = "openai"
	ProviderDeepSeek ProviderName = "deepseek"
)

// MessageAdapter 按厂商处理会话消息的存储规范化与 API 出站形态。
//
// 内部 history 可保留 reasoning_content；PrepareOutboundMessages 在 StreamChat 发送前
// 按厂商规则裁剪/校验，避免 DeepSeek 等接口返回 400。
type MessageAdapter interface {
	Name() ProviderName
	NormalizeAssistantForStorage(existing []Message, msg Message, logger *slog.Logger) Message
	PrepareOutboundMessages(messages []Message) ([]Message, error)
	RequestExtra() map[string]any
}

// NewMessageAdapter 根据 provider 字符串构造适配器；未知值回退 openai。
func NewMessageAdapter(provider string) MessageAdapter {
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderDeepSeek:
		return deepSeekAdapter{}
	default:
		return openAIAdapter{}
	}
}

func cloneMessage(message Message) Message {
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

func latestAssistantReasoningContent(messages []Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "assistant" {
			continue
		}
		return msg.ReasoningContent
	}
	return ""
}

func isToolCallbackMessage(msg Message) bool {
	for _, tc := range msg.ToolCalls {
		if tc.Function.Name == "tool_callback" {
			return true
		}
	}
	return false
}

func defaultAdapterLogger(logger *slog.Logger) *slog.Logger {
	return logx.OrDefault(logger)
}
