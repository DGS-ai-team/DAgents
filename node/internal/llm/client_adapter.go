package llm

import (
	"context"
	"log/slog"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
)

// adapterClient 在 OpenAI 兼容 HTTP 客户端外包装厂商 MessageAdapter。
type adapterClient struct {
	inner   *OpenAIClient
	adapter MessageAdapter
	logger  *slog.Logger
}

func newAdapterClient(inner *OpenAIClient, adapter MessageAdapter, logger *slog.Logger) *adapterClient {
	return &adapterClient{
		inner:   inner,
		adapter: adapter,
		logger:  logx.OrDefault(logger),
	}
}

func (c *adapterClient) NormalizeAssistant(existing []Message, msg Message) Message {
	if c == nil || c.adapter == nil {
		return cloneMessage(msg)
	}
	return c.adapter.NormalizeAssistantForStorage(existing, msg, c.logger)
}

func (c *adapterClient) StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error) {
	prepared, err := c.adapter.PrepareOutboundMessages(req.Messages)
	if err != nil {
		return ChatResult{}, err
	}
	outbound := ChatRequest{
		SystemPrompt: req.SystemPrompt,
		Messages:     prepared,
		Tools:        req.Tools,
	}
	ready := MessagesWithSystem(req.SystemPrompt, prepared)
	if payloads, ok, merr := c.adapter.MarshalChatRequestMessages(ready); ok {
		if merr != nil {
			return ChatResult{}, merr
		}
		outbound.APIMessages = payloads
	}
	return c.inner.StreamChat(ctx, outbound, handler)
}

func (c *adapterClient) CompleteText(ctx context.Context, req CompleteRequest) (string, error) {
	return c.inner.CompleteText(ctx, req)
}

// envAdapterClient 延迟从环境变量读取 API Key。
type envAdapterClient struct {
	baseURL  string
	keyEnv   string
	adapter  MessageAdapter
	settings *RuntimeSettings
	logger   *slog.Logger
}

func newEnvAdapterClient(baseURL, keyEnv string, adapter MessageAdapter, settings *RuntimeSettings, logger *slog.Logger) *envAdapterClient {
	env := keyEnv
	if env == "" {
		env = "OPENAI_API_KEY"
	}
	return &envAdapterClient{
		baseURL:  baseURL,
		keyEnv:   env,
		adapter:  adapter,
		settings: settings,
		logger:   logx.OrDefault(logger),
	}
}

func (c *envAdapterClient) innerClient(key string) *adapterClient {
	model := ""
	var extra map[string]any
	if c.settings != nil {
		model = c.settings.ModelName()
		extra = c.settings.RequestExtra()
	}
	inner := NewOpenAIClient(OpenAIConfig{
		BaseURL:      c.baseURL,
		Model:        model,
		APIKey:       key,
		RequestExtra: extra,
	})
	return newAdapterClient(inner, c.adapter, c.logger)
}

func (c *envAdapterClient) NormalizeAssistant(existing []Message, msg Message) Message {
	if c.adapter == nil {
		return cloneMessage(msg)
	}
	return c.adapter.NormalizeAssistantForStorage(existing, msg, c.logger)
}

func (c *envAdapterClient) StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error) {
	key, err := lookupEnvAPIKey(c.keyEnv)
	if err != nil {
		return ChatResult{}, err
	}
	return c.innerClient(key).StreamChat(ctx, req, handler)
}

func (c *envAdapterClient) CompleteText(ctx context.Context, req CompleteRequest) (string, error) {
	key, err := lookupEnvAPIKey(c.keyEnv)
	if err != nil {
		return "", err
	}
	return c.innerClient(key).CompleteText(ctx, req)
}
