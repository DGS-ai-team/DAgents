package llm

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

const defaultDeepSeekBaseURL = "https://api.deepseek.com"

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
	baseURL := cfg.LLM.BaseURL
	if strings.TrimSpace(baseURL) == "" {
		if adapter.Name() == ProviderDeepSeek {
			baseURL = defaultDeepSeekBaseURL
		}
	}
	return newEnvAdapterClient(baseURL, cfg.LLM.APIKeyEnv, adapter, settings, slog.Default())
}

func lookupEnvAPIKey(keyEnv string) (string, error) {
	key := os.Getenv(keyEnv)
	if key == "" {
		return "", fmt.Errorf("LLM API key not set in %s", keyEnv)
	}
	return key, nil
}
