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
// 始终返回可热切换连接的 envAdapterClient；mock 由 RuntimeSettings 控制。
// settings 非 nil 时，provider / base_url / api_key_env / model / thinking 均可经 SyncFromConfig 热更新。
func NewFromConfig(cfg *config.Config, settings *RuntimeSettings) Client {
	if settings == nil {
		settings = NewRuntimeSettings(cfg)
	}
	adapter := NewMessageAdapter(cfg.LLM.Provider)
	baseURL := resolveBaseURL(adapter.Name(), cfg.LLM.BaseURL)
	if !cfg.LLM.Mock {
		if warn := mismatchBaseURLWarning(adapter.Name(), cfg.LLM.BaseURL); warn != "" {
			slog.Default().Warn(warn, "provider", adapter.Name(), "base_url", cfg.LLM.BaseURL, "resolved", baseURL)
		}
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
	keyEnv = strings.TrimSpace(keyEnv)
	if keyEnv == "" {
		return "", fmt.Errorf("LLM API key is not configured; set it in 设置 › 连接 › LLM 配置")
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return "", fmt.Errorf("LLM API key not set in %s (or configure api_key in settings)", keyEnv)
	}
	return key, nil
}
