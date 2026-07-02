package llm

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	defaultDeepSeekBaseURL = "https://api.deepseek.com"
	defaultQwenBaseURL     = "https://dashscope.aliyuncs.com/compatible-mode/v1"
	defaultVLLMBaseURL     = "http://127.0.0.1:8000/v1"
)

// NewFromConfig 根据 Node 配置构造 LLM Client。
//
// mock=true 时返回 MockClient；否则按 llm.provider 选择 MessageAdapter 与默认 base_url。
// settings 非 nil 时，model 与 thinking 参数可在运行时通过 API 热更新。
func NewFromConfig(cfg *config.Config, settings *RuntimeSettings) Client {
	if settings == nil {
		settings = NewRuntimeSettings(cfg)
	}
	adapter := NewMessageAdapter(cfg.LLM.Provider)
	if cfg.LLM.Mock {
		return &MockClient{Prefix: "", adapter: adapter}
	}
	baseURL := resolveBaseURL(adapter.Name(), cfg.LLM.BaseURL)
	if warn := mismatchBaseURLWarning(adapter.Name(), cfg.LLM.BaseURL); warn != "" {
		slog.Default().Warn(warn, "provider", adapter.Name(), "base_url", cfg.LLM.BaseURL, "resolved", baseURL)
	}
	return newEnvAdapterClient(baseURL, cfg.LLM.APIKeyEnv, adapter, settings, slog.Default())
}

func defaultBaseURL(provider ProviderName) string {
	switch provider {
	case ProviderDeepSeek:
		return defaultDeepSeekBaseURL
	case ProviderQwen:
		return defaultQwenBaseURL
	case ProviderVLLM:
		return defaultVLLMBaseURL
	default:
		return ""
	}
}

func lookupEnvAPIKey(keyEnv string) (string, error) {
	key := os.Getenv(keyEnv)
	if key == "" {
		return "", fmt.Errorf("LLM API key not set in %s", keyEnv)
	}
	return key, nil
}
