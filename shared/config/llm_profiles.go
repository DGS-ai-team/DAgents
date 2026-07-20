package config

import (
	"fmt"
	"sort"
	"strings"
)

const defaultLLMProfileID = "default"

// normalizeLLMProfiles 保证至少有一个 profile，并把 active 快照同步到顶层字段。
func (c *Config) normalizeLLMProfiles() {
	if c == nil {
		return
	}
	if c.LLM.Profiles == nil {
		c.LLM.Profiles = make(map[string]LLMProfileConfig)
	}
	if len(c.LLM.Profiles) == 0 {
		id := strings.TrimSpace(c.LLM.Active)
		if id == "" {
			id = defaultLLMProfileID
		}
		c.LLM.Profiles[id] = c.LLM.snapshotProfile()
		c.LLM.Active = id
		return
	}
	active := strings.TrimSpace(c.LLM.Active)
	if active == "" || !c.LLM.hasProfile(active) {
		// 优先保留已有顶层快照对应的档案；否则取排序后第一个 id。
		if p := c.LLM.snapshotProfile(); c.LLM.looksConfigured(p) {
			id := active
			if id == "" {
				id = defaultLLMProfileID
			}
			if !c.LLM.hasProfile(id) {
				c.LLM.Profiles[id] = p
			}
			c.LLM.Active = id
		} else {
			ids := c.LLM.ProfileIDs()
			c.LLM.Active = ids[0]
		}
	}
	c.LLM.applyProfileToFlat(c.LLM.Active)
}

func (l LLMConfig) hasProfile(id string) bool {
	_, ok := l.Profiles[strings.TrimSpace(id)]
	return ok
}

func (l LLMConfig) looksConfigured(p LLMProfileConfig) bool {
	if p.Mock {
		return true
	}
	return strings.TrimSpace(p.Provider) != "" || strings.TrimSpace(p.Model) != "" || strings.TrimSpace(p.BaseURL) != ""
}

func (l LLMConfig) snapshotProfile() LLMProfileConfig {
	return LLMProfileConfig{
		Provider:        l.Provider,
		BaseURL:         l.BaseURL,
		Model:           l.Model,
		APIKeyEnv:       l.APIKeyEnv,
		Mock:            l.Mock,
		Thinking:        l.Thinking,
		ReasoningEffort: l.ReasoningEffort,
	}
}

func (l *LLMConfig) applyProfileToFlat(id string) {
	p, ok := l.Profiles[strings.TrimSpace(id)]
	if !ok {
		return
	}
	l.Active = strings.TrimSpace(id)
	l.Provider = p.Provider
	l.BaseURL = p.BaseURL
	l.Model = p.Model
	l.APIKeyEnv = p.APIKeyEnv
	l.Mock = p.Mock
	l.Thinking = p.Thinking
	l.ReasoningEffort = p.ReasoningEffort
}

// ProfileIDs 返回排序后的档案 id 列表。
func (l LLMConfig) ProfileIDs() []string {
	ids := make([]string, 0, len(l.Profiles))
	for id := range l.Profiles {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ActiveProfileID 返回当前生效档案 id。
func (l LLMConfig) ActiveProfileID() string {
	return strings.TrimSpace(l.Active)
}

// ApplyActiveToFlat 将当前 active 档案复制到顶层快照字段。
func (l *LLMConfig) ApplyActiveToFlat() {
	if l == nil {
		return
	}
	l.applyProfileToFlat(l.Active)
}

// GetProfile 返回指定档案；不存在时 ok=false。
func (l LLMConfig) GetProfile(id string) (LLMProfileConfig, bool) {
	p, ok := l.Profiles[strings.TrimSpace(id)]
	return p, ok
}

// UpsertProfile 写入/更新档案；若设为 active 或当前无 active，则同步到顶层快照。
func (c *Config) UpsertProfile(id string, profile LLMProfileConfig, makeActive bool) error {
	if c == nil {
		return fmt.Errorf("config unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("llm profile id is required")
	}
	if err := validateLLMProfile(profile); err != nil {
		return err
	}
	if c.LLM.Profiles == nil {
		c.LLM.Profiles = make(map[string]LLMProfileConfig)
	}
	c.LLM.Profiles[id] = normalizeLLMProfile(profile)
	if makeActive || strings.TrimSpace(c.LLM.Active) == "" || c.LLM.Active == id {
		c.LLM.applyProfileToFlat(id)
	}
	return nil
}

// DeleteProfile 删除档案；不可删除最后一个；若删的是 active 则切到剩余第一个。
func (c *Config) DeleteProfile(id string) error {
	if c == nil {
		return fmt.Errorf("config unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("llm profile id is required")
	}
	if !c.LLM.hasProfile(id) {
		return fmt.Errorf("llm profile %q not found", id)
	}
	if len(c.LLM.Profiles) <= 1 {
		return fmt.Errorf("cannot delete the last llm profile")
	}
	delete(c.LLM.Profiles, id)
	if c.LLM.Active == id {
		ids := c.LLM.ProfileIDs()
		c.LLM.applyProfileToFlat(ids[0])
	}
	return nil
}

// SetActiveLLMProfile 切换当前生效档案，并把字段复制到顶层快照。
func (c *Config) SetActiveLLMProfile(id string) error {
	if c == nil {
		return fmt.Errorf("config unavailable")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("llm profile id is required")
	}
	if !c.LLM.hasProfile(id) {
		return fmt.Errorf("llm profile %q not found", id)
	}
	c.LLM.applyProfileToFlat(id)
	return nil
}

// SyncActiveProfileFromFlat 把顶层快照写回当前 active 档案（编辑当前连接后调用）。
func (c *Config) SyncActiveProfileFromFlat() {
	if c == nil {
		return
	}
	id := strings.TrimSpace(c.LLM.Active)
	if id == "" {
		id = defaultLLMProfileID
		c.LLM.Active = id
	}
	if c.LLM.Profiles == nil {
		c.LLM.Profiles = make(map[string]LLMProfileConfig)
	}
	c.LLM.Profiles[id] = c.LLM.snapshotProfile()
}

func validateLLMProfile(p LLMProfileConfig) error {
	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	if provider == "" {
		return fmt.Errorf("llm.provider is required")
	}
	switch provider {
	case "openai", "deepseek", "qwen", "vllm", "mock":
	default:
		return fmt.Errorf("unsupported llm.provider %q", p.Provider)
	}
	mock := p.Mock || provider == "mock"
	if !mock && strings.TrimSpace(p.Model) == "" {
		return fmt.Errorf("llm.model is required when mock is false")
	}
	return nil
}

func normalizeLLMProfile(p LLMProfileConfig) LLMProfileConfig {
	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	mock := p.Mock || provider == "mock"
	if provider == "mock" {
		mock = true
	}
	apiKeyEnv := strings.TrimSpace(p.APIKeyEnv)
	if apiKeyEnv == "" {
		apiKeyEnv = "OPENAI_API_KEY"
	}
	return LLMProfileConfig{
		Provider:        provider,
		BaseURL:         strings.TrimSpace(p.BaseURL),
		Model:           strings.TrimSpace(p.Model),
		APIKeyEnv:       apiKeyEnv,
		Mock:            mock,
		Thinking:        strings.TrimSpace(p.Thinking),
		ReasoningEffort: strings.TrimSpace(p.ReasoningEffort),
	}
}
