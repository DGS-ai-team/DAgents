package llm

import (
	"fmt"
	"os"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// NewFromConfig 根据 Node 配置构造 LLM Client。

// mock=true 时返回 MockClient（无网络）；否则返回从 api_key_env 读密钥的 OpenAI 兼容客户端。
func NewFromConfig(cfg *config.Config) Client {
	if cfg.LLM.Mock {
		return &MockClient{Prefix: ""}
	}
	return NewEnvOpenAIClient(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKeyEnv)
}

func lookupEnvAPIKey(keyEnv string) (string, error) {
	key := os.Getenv(keyEnv)
	if key == "" {
		return "", fmt.Errorf("LLM API key not set in %s", keyEnv)
	}
	return key, nil
}
