package store

import (
	"context"
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

// EnsureDefaultLLMConfig 在全新安装的档案库为空时创建唯一的默认档案。
// 运行时设置不再从旧 YAML/profile 结构迁移；当前设置来源始终是 llm_configs.db。
func EnsureDefaultLLMConfig(ctx context.Context, s *LLMConfigStore, cfg *config.Config) error {
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
	return s.ReplaceAll(ctx, []LLMConfigRecord{rec}, nil, nil)
}
