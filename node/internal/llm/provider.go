package llm

import (
	"log/slog"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

// ProviderName 为 config.yaml llm.provider 取值。
type ProviderName string

const (
	ProviderOpenAI   ProviderName = "openai"
	ProviderDeepSeek ProviderName = "deepseek"
	ProviderQwen     ProviderName = "qwen"
	ProviderVLLM     ProviderName = "vllm"
)

// MessageAdapter 按厂商处理会话消息的存储规范化与 API 出站形态。
//
// 内部 history 可保留 reasoning_content；PrepareOutboundMessages 在 StreamChat 发送前
// 按厂商规则裁剪/校验；MarshalChatRequestMessages 负责 HTTP 请求体 messages 字段的最终序列化。
type MessageAdapter interface {
	Name() ProviderName
	NormalizeAssistantForStorage(existing []Message, msg Message, logger *slog.Logger) Message
	PrepareOutboundMessages(messages []Message) ([]Message, error)
	// MarshalChatRequestMessages 序列化出站 messages（已含 system）。
	// ok=false 时由 OpenAIClient 使用 []Message 默认 JSON 编码；ok=true 时使用 payloads。
	MarshalChatRequestMessages(messages []Message) (payloads []map[string]any, ok bool, err error)
	RequestExtra() map[string]any
}

// NewMessageAdapter 根据 provider 字符串构造适配器；未知值回退 openai。
func NewMessageAdapter(provider string) MessageAdapter {
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderDeepSeek:
		return deepSeekAdapter{}
	case ProviderQwen:
		return qwenAdapter{}
	case ProviderVLLM:
		return vllmAdapter{}
	default:
		return openAIAdapter{}
	}
}

func cloneMessage(message Message) Message {
	return CloneMessage(message)
}

func defaultAdapterLogger(logger *slog.Logger) *slog.Logger {
	return logx.OrDefault(logger)
}
