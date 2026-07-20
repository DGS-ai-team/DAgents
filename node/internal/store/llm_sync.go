package store

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// ApplyLLMConfigsToConfig 用 SQLite 中的配置覆盖 cfg.LLM.Profiles（不含明文 key）。
// activeID 为空时选用第一条；若仍为空则保持原 active。
func ApplyLLMConfigsToConfig(cfg *config.Config, records []LLMConfigRecord, activeID string) {
	if cfg == nil {
		return
	}
	next := make(map[string]config.LLMProfileConfig, len(records))
	order := make([]string, 0, len(records))
	for _, rec := range records {
		id := strings.TrimSpace(rec.ID)
		if id == "" {
			continue
		}
		mm := rec.MultimodalEnabled
		next[id] = config.LLMProfileConfig{
			Provider:          rec.Provider,
			BaseURL:           rec.BaseURL,
			Model:             rec.Model,
			APIKeyEnv:         "", // key 改由 SQLite 加密存储
			Mock:              rec.Mock,
			Thinking:          rec.Thinking,
			ReasoningEffort:   rec.ReasoningEffort,
			MultimodalEnabled: &mm,
		}
		order = append(order, id)
	}
	if len(next) == 0 {
		return
	}
	cfg.LLM.Profiles = next
	cfg.LLM.ProfileOrder = order
	active := strings.TrimSpace(activeID)
	if active == "" {
		active = order[0]
	} else if _, ok := cfg.LLM.GetProfile(active); !ok {
		active = order[0]
	}
	_ = cfg.SetActiveLLMProfile(active)
}

// MigrateLLMConfigsFromConfig 在 SQLite 为空时，从 config.yaml profiles 导入；
// 若档案声明了 api_key_env，则尝试从环境变量读取并加密入库。
func MigrateLLMConfigsFromConfig(ctx context.Context, s *LLMConfigStore, cfg *config.Config) error {
	if s == nil || cfg == nil {
		return nil
	}
	n, err := s.Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	ids := cfg.LLM.ProfileIDs()
	if len(ids) == 0 {
		// 用顶层快照造一条
		id := strings.TrimSpace(cfg.LLM.Active)
		if id == "" {
			id = "default"
		}
		rec := LLMConfigRecord{
			ID:                id,
			Provider:          cfg.LLM.Provider,
			BaseURL:           cfg.LLM.BaseURL,
			Model:             cfg.LLM.Model,
			Mock:              cfg.LLM.Mock,
			Thinking:          cfg.LLM.Thinking,
			ReasoningEffort:   cfg.LLM.ReasoningEffort,
			MultimodalEnabled: cfg.MultimodalEnabled(),
		}
		keys := map[string]string{}
		if env := strings.TrimSpace(cfg.LLM.APIKeyEnv); env != "" {
			if v := strings.TrimSpace(os.Getenv(env)); v != "" {
				keys[id] = v
			}
		}
		return s.ReplaceAll(ctx, []LLMConfigRecord{rec}, keys, nil)
	}
	records := make([]LLMConfigRecord, 0, len(ids))
	keys := map[string]string{}
	for _, id := range ids {
		p, ok := cfg.LLM.GetProfile(id)
		if !ok {
			continue
		}
		records = append(records, LLMConfigRecord{
			ID:                id,
			Provider:          p.Provider,
			BaseURL:           p.BaseURL,
			Model:             p.Model,
			Mock:              p.Mock,
			Thinking:          p.Thinking,
			ReasoningEffort:   p.ReasoningEffort,
			MultimodalEnabled: config.ProfileMultimodalEnabled(p),
		})
		env := strings.TrimSpace(p.APIKeyEnv)
		if env == "" {
			env = strings.TrimSpace(cfg.LLM.APIKeyEnv)
		}
		if env != "" {
			if v := strings.TrimSpace(os.Getenv(env)); v != "" {
				keys[id] = v
			}
		}
	}
	if len(records) == 0 {
		return fmt.Errorf("no llm profiles to migrate")
	}
	return s.ReplaceAll(ctx, records, keys, nil)
}
