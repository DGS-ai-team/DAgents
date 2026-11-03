package store

import (
	"context"
	"fmt"
	"os"
	"sort"
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
			ThinkingBudget:    rec.ThinkingBudget,
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

// EnsureDefaultLLMConfig seeds an empty database once. Legacy node_settings
// profile data is imported as a migration; ordinary YAML business values are
// ignored by OverlayNodeSettings.
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
	ids := cfg.LLM.ProfileIDs()
	if len(ids) > 0 {
		records := make([]LLMConfigRecord, 0, len(ids))
		keys := make(map[string]string)
		missingKeyEnv := make([]string, 0)
		for order, id := range ids {
			profile, ok := cfg.LLM.GetProfile(id)
			if !ok {
				continue
			}
			records = append(records, LLMConfigRecord{
				ID:                id,
				SortOrder:         order,
				Provider:          profile.Provider,
				BaseURL:           profile.BaseURL,
				Model:             profile.Model,
				Mock:              profile.Mock,
				Thinking:          profile.Thinking,
				ReasoningEffort:   profile.ReasoningEffort,
				ThinkingBudget:    profile.ThinkingBudget,
				MultimodalEnabled: config.ProfileMultimodalEnabled(profile),
			})
			if env := strings.TrimSpace(profile.APIKeyEnv); env != "" {
				if value := strings.TrimSpace(os.Getenv(env)); value != "" {
					keys[id] = value
				} else {
					missingKeyEnv = append(missingKeyEnv, env)
				}
			}
		}
		if len(missingKeyEnv) > 0 {
			sort.Strings(missingKeyEnv)
			missingKeyEnv = compactStrings(missingKeyEnv)
			return fmt.Errorf("cannot migrate legacy LLM profiles: required environment variable(s) %s are unset; keep the original settings and start Node with these variables to retry", strings.Join(missingKeyEnv, ", "))
		}
		if len(records) > 0 {
			return s.ReplaceAll(ctx, records, keys, nil)
		}
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
		ThinkingBudget:    cfg.LLM.ThinkingBudget,
		MultimodalEnabled: cfg.MultimodalEnabled(),
	}
	keys := map[string]string{}
	if env := strings.TrimSpace(cfg.LLM.APIKeyEnv); env != "" {
		if value := strings.TrimSpace(os.Getenv(env)); value != "" {
			keys[id] = value
		} else {
			return fmt.Errorf("cannot migrate legacy LLM profile: required environment variable %s is unset; keep the original settings and start Node with this variable to retry", env)
		}
	}
	return s.ReplaceAll(ctx, []LLMConfigRecord{rec}, keys, nil)
}

func compactStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}
