package llm

import (
	"context"
	"log/slog"
	"strings"

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
	if err := ValidateToolProtocol(prepared); err != nil {
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

// envAdapterClient 延迟从环境变量读取 API Key；连接参数优先取自 RuntimeSettings（可热切换）。
type envAdapterClient struct {
	fallbackBaseURL string
	fallbackKeyEnv  string
	fallbackAdapter MessageAdapter
	settings        *RuntimeSettings
	logger          *slog.Logger
	mock            *MockClient
}

func newEnvAdapterClient(baseURL, keyEnv string, adapter MessageAdapter, settings *RuntimeSettings, logger *slog.Logger) *envAdapterClient {
	env := keyEnv
	if env == "" {
		env = "OPENAI_API_KEY"
	}
	return &envAdapterClient{
		fallbackBaseURL: baseURL,
		fallbackKeyEnv:  env,
		fallbackAdapter: adapter,
		settings:        settings,
		logger:          logx.OrDefault(logger),
		mock:            &MockClient{adapter: adapter},
	}
}

func (c *envAdapterClient) resolveConnection() (provider, baseURL, keyEnv string, mock bool, adapter MessageAdapter) {
	if c.settings != nil {
		provider, baseURL, keyEnv, mock = c.settings.Connection()
		adapter = NewMessageAdapter(provider)
		if mock {
			return provider, baseURL, keyEnv, true, adapter
		}
		baseURL = resolveBaseURL(adapter.Name(), baseURL)
		return provider, baseURL, keyEnv, false, adapter
	}
	adapter = c.fallbackAdapter
	if adapter == nil {
		adapter = NewMessageAdapter("openai")
	}
	keyEnv = c.fallbackKeyEnv
	if keyEnv == "" {
		keyEnv = "OPENAI_API_KEY"
	}
	return string(adapter.Name()), c.fallbackBaseURL, keyEnv, false, adapter
}

func (c *envAdapterClient) innerClient(key, baseURL string, adapter MessageAdapter) *adapterClient {
	model := ""
	var extra map[string]any
	if c.settings != nil {
		model = c.settings.ModelName()
		extra = c.settings.RequestExtra()
	}
	inner := NewOpenAIClient(OpenAIConfig{
		BaseURL:      baseURL,
		Model:        model,
		APIKey:       key,
		RequestExtra: extra,
	})
	return newAdapterClient(inner, adapter, c.logger)
}

func (c *envAdapterClient) NormalizeAssistant(existing []Message, msg Message) Message {
	_, _, _, mock, adapter := c.resolveConnection()
	if mock {
		return c.mockClient(adapter).NormalizeAssistant(existing, msg)
	}
	if adapter == nil {
		return cloneMessage(msg)
	}
	return adapter.NormalizeAssistantForStorage(existing, msg, c.logger)
}

func (c *envAdapterClient) mockClient(adapter MessageAdapter) *MockClient {
	if c.mock == nil {
		c.mock = &MockClient{}
	}
	c.mock.adapter = adapter
	return c.mock
}

func (c *envAdapterClient) StreamChat(ctx context.Context, req ChatRequest, handler StreamHandler) (ChatResult, error) {
	_, baseURL, keyEnv, mock, adapter := c.resolveConnection()
	if mock {
		return c.mockClient(adapter).StreamChat(ctx, req, handler)
	}
	key, err := c.resolveAPIKey(keyEnv)
	if err != nil {
		return ChatResult{}, err
	}
	return c.innerClient(key, baseURL, adapter).StreamChat(ctx, req, handler)
}

func (c *envAdapterClient) CompleteText(ctx context.Context, req CompleteRequest) (string, error) {
	_, baseURL, keyEnv, mock, adapter := c.resolveConnection()
	if mock {
		return c.mockClient(adapter).CompleteText(ctx, req)
	}
	key, err := c.resolveAPIKey(keyEnv)
	if err != nil {
		return "", err
	}
	return c.innerClient(key, baseURL, adapter).CompleteText(ctx, req)
}

func (c *envAdapterClient) resolveAPIKey(keyEnv string) (string, error) {
	if c.settings != nil {
		if key := strings.TrimSpace(c.settings.APIKeyValue()); key != "" {
			return key, nil
		}
	}
	return lookupEnvAPIKey(keyEnv)
}
