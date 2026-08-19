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
		c.LLM.Profiles[id] = c.snapshotLLMProfile()
		c.LLM.Active = id
		c.applyLLMProfile(id)
		return
	}
	// 旧配置仅有顶层 multimodal.enabled 时，迁移到尚未声明该字段的档案。
	c.migrateMultimodalIntoProfiles()
	active := strings.TrimSpace(c.LLM.Active)
	if active == "" || !c.LLM.hasProfile(active) {
		if p := c.snapshotLLMProfile(); c.LLM.looksConfigured(p) {
			id := active
			if id == "" {
				id = defaultLLMProfileID
			}
			if !c.LLM.hasProfile(id) {
				c.LLM.Profiles[id] = p
				if len(c.LLM.ProfileOrder) == 0 {
					c.LLM.ProfileOrder = []string{id}
				}
			}
			c.LLM.Active = id
		} else {
			c.LLM.Active = c.LLM.FirstProfileID()
		}
	}
	c.applyLLMProfile(c.LLM.Active)
}

// migrateMultimodalIntoProfiles 将遗留的顶层 multimodal.enabled=true
// 写入尚未设置 multimodal_enabled 的档案（避免升级后丢失开关）。
func (c *Config) migrateMultimodalIntoProfiles() {
	if c == nil || !c.MultimodalEnabled() || len(c.LLM.Profiles) == 0 {
		return
	}
	for id, p := range c.LLM.Profiles {
		if p.MultimodalEnabled != nil {
			continue
		}
		v := true
		p.MultimodalEnabled = &v
		c.LLM.Profiles[id] = p
	}
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

func (c *Config) snapshotLLMProfile() LLMProfileConfig {
	p := LLMProfileConfig{
		Provider:        c.LLM.Provider,
		BaseURL:         c.LLM.BaseURL,
		Model:           c.LLM.Model,
		APIKeyEnv:       c.LLM.APIKeyEnv,
		Mock:            c.LLM.Mock,
		Thinking:        c.LLM.Thinking,
		ReasoningEffort: c.LLM.ReasoningEffort,
	}
	enabled := c.MultimodalEnabled()
	p.MultimodalEnabled = &enabled
	return p
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

// applyLLMProfile 将档案复制到 LLM 顶层快照，并同步 multimodal.enabled。
func (c *Config) applyLLMProfile(id string) {
	if c == nil {
		return
	}
	c.LLM.applyProfileToFlat(id)
	p, ok := c.LLM.GetProfile(id)
	if !ok {
		return
	}
	enabled := false
	if p.MultimodalEnabled != nil {
		enabled = *p.MultimodalEnabled
	}
	c.Multimodal.Enabled = boolPtrCopy(enabled)
}

func boolPtrCopy(v bool) *bool {
	b := v
	return &b
}

// ProfileIDs 返回配置 id 列表：优先 ProfileOrder，否则按字母序。
func (l LLMConfig) ProfileIDs() []string {
	if len(l.ProfileOrder) > 0 {
		out := make([]string, 0, len(l.ProfileOrder))
		seen := make(map[string]struct{}, len(l.ProfileOrder))
		for _, id := range l.ProfileOrder {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if !l.hasProfile(id) {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		for _, id := range sortedProfileIDs(l.Profiles) {
			if _, ok := seen[id]; ok {
				continue
			}
			out = append(out, id)
		}
		return out
	}
	return sortedProfileIDs(l.Profiles)
}

func sortedProfileIDs(profiles map[string]LLMProfileConfig) []string {
	ids := make([]string, 0, len(profiles))
	for id := range profiles {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// FirstProfileID 返回第一条配置 id（默认选用）。
func (l LLMConfig) FirstProfileID() string {
	ids := l.ProfileIDs()
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

// ActiveProfileID 返回当前生效档案 id。
func (l LLMConfig) ActiveProfileID() string {
	return strings.TrimSpace(l.Active)
}

// ApplyActiveToFlat 将当前 active 档案复制到顶层快照与 multimodal。
func (c *Config) ApplyActiveToFlat() {
	if c == nil {
		return
	}
	c.applyLLMProfile(c.LLM.Active)
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
		c.applyLLMProfile(id)
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
	if order := c.LLM.ProfileOrder; len(order) > 0 {
		next := make([]string, 0, len(order))
		for _, item := range order {
			if item == id {
				continue
			}
			next = append(next, item)
		}
		c.LLM.ProfileOrder = next
	}
	if c.LLM.Active == id {
		c.applyLLMProfile(c.LLM.FirstProfileID())
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
	c.applyLLMProfile(id)
	return nil
}

// SyncActiveProfileFromFlat 把顶层快照（含 multimodal）写回当前 active 档案。
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
	c.LLM.Profiles[id] = c.snapshotLLMProfile()
}

func validateLLMProfile(p LLMProfileConfig) error {
	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	if provider == "" {
		return fmt.Errorf("llm.provider is required")
	}
	switch provider {
	case "openai", "deepseek", "qwen", "vllm", "glm", "minimax", "mimo", "mock":
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
	out := LLMProfileConfig{
		Provider:        provider,
		BaseURL:         strings.TrimSpace(p.BaseURL),
		Model:           strings.TrimSpace(p.Model),
		APIKeyEnv:       apiKeyEnv,
		Mock:            mock,
		Thinking:        strings.TrimSpace(p.Thinking),
		ReasoningEffort: strings.TrimSpace(p.ReasoningEffort),
	}
	if p.MultimodalEnabled != nil {
		v := *p.MultimodalEnabled
		out.MultimodalEnabled = &v
	} else {
		f := false
		out.MultimodalEnabled = &f
	}
	return out
}

// ProfileMultimodalEnabled 返回档案的多模态开关（nil/缺省视为 false）。
func ProfileMultimodalEnabled(p LLMProfileConfig) bool {
	return p.MultimodalEnabled != nil && *p.MultimodalEnabled
}
