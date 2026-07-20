package llm

import (
	"fmt"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// RuntimeSettings 为可热更新的 LLM 运行时参数（连接 + model + thinking 等）。
type RuntimeSettings struct {
	mu sync.RWMutex

	AgentID         string
	ActiveProfile   string
	profileIDs      []string
	Provider        string
	BaseURL         string
	APIKeyEnv       string
	Model           string
	Mock            bool
	MultimodalEnabled bool
	Thinking        string
	ReasoningEffort string
}

// LLMSettingsView 为 GET /v1/llm/settings 与 agent/info 嵌套字段。
type LLMSettingsView struct {
	ActiveProfile     string   `json:"active_profile,omitempty"`
	Profiles          []string `json:"profiles,omitempty"`
	Provider          string   `json:"provider"`
	Model             string   `json:"model"`
	Mock              bool     `json:"mock"`
	MultimodalEnabled bool     `json:"multimodal_enabled"`
	ThinkingSupported bool     `json:"thinking_supported"`
	Thinking          string   `json:"thinking,omitempty"`
	ReasoningEffort   string   `json:"reasoning_effort,omitempty"`
}

// LLMSettingsPatch 为 PATCH /v1/llm/settings 请求体（字段均可选）。
type LLMSettingsPatch struct {
	ActiveProfile   *string `json:"active_profile"`
	Thinking        *string `json:"thinking"`
	ReasoningEffort *string `json:"reasoning_effort"`
}

// NewRuntimeSettings 从配置初始化运行时 LLM 参数。
func NewRuntimeSettings(cfg *config.Config) *RuntimeSettings {
	if cfg == nil {
		return &RuntimeSettings{
			Provider:        string(ProviderOpenAI),
			APIKeyEnv:       "OPENAI_API_KEY",
			Thinking:        "enabled",
			ReasoningEffort: "high",
		}
	}
	thinking, effort := NormalizeThinkingSettings(cfg.LLM.Provider, cfg.LLM.Thinking, cfg.LLM.ReasoningEffort)
	return &RuntimeSettings{
		AgentID:           strings.TrimSpace(cfg.AgentID),
		ActiveProfile:     cfg.LLM.ActiveProfileID(),
		profileIDs:        cfg.LLM.ProfileIDs(),
		Provider:          strings.TrimSpace(cfg.LLM.Provider),
		BaseURL:           strings.TrimSpace(cfg.LLM.BaseURL),
		APIKeyEnv:         strings.TrimSpace(cfg.LLM.APIKeyEnv),
		Model:             strings.TrimSpace(cfg.LLM.Model),
		Mock:              cfg.LLM.Mock,
		MultimodalEnabled: cfg.MultimodalEnabled(),
		Thinking:          thinking,
		ReasoningEffort:   effort,
	}
}

// Snapshot 返回当前 LLM 设置只读视图。
func (s *RuntimeSettings) Snapshot() LLMSettingsView {
	if s == nil {
		return LLMSettingsView{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshotLocked()
}

func (s *RuntimeSettings) snapshotLocked() LLMSettingsView {
	view := LLMSettingsView{
		ActiveProfile:     s.ActiveProfile,
		Profiles:          append([]string(nil), s.profileIDs...),
		Provider:          s.Provider,
		Model:             s.Model,
		Mock:              s.Mock,
		MultimodalEnabled: s.MultimodalEnabled,
		ThinkingSupported: ThinkingSupported(s.Provider),
	}
	if view.ThinkingSupported {
		view.Thinking = s.Thinking
		if s.Thinking == "enabled" {
			view.ReasoningEffort = s.ReasoningEffort
		}
	}
	return view
}

// Connection 返回当前连接参数（线程安全）。
func (s *RuntimeSettings) Connection() (provider, baseURL, keyEnv string, mock bool) {
	if s == nil {
		return string(ProviderOpenAI), "", "OPENAI_API_KEY", false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	keyEnv = strings.TrimSpace(s.APIKeyEnv)
	if keyEnv == "" {
		keyEnv = "OPENAI_API_KEY"
	}
	return s.Provider, s.BaseURL, keyEnv, s.Mock
}

// Model 返回当前模型名（线程安全）。
func (s *RuntimeSettings) ModelName() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Model
}

// RequestExtra 构造当前 provider 的 Chat Completions 顶层扩展字段（含 user_id=agent_id）。
func (s *RuntimeSettings) RequestExtra() map[string]any {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var extra map[string]any
	if built := BuildRequestExtra(s.Provider, s.Thinking, s.ReasoningEffort); len(built) > 0 {
		extra = built
	}
	if uid := strings.TrimSpace(s.AgentID); uid != "" {
		if extra == nil {
			extra = make(map[string]any, 1)
		}
		extra["user_id"] = uid
	}
	return extra
}

// ApplyPatch 热更新 active_profile / thinking / reasoning_effort。
// active_profile 仅更新运行时视图字段名提示；实际切档案需经 setup 写盘 + SyncFromConfig，
// 或由 Server 在 PATCH 中先 SetActiveLLMProfile 再 SyncFromConfig。
func (s *RuntimeSettings) ApplyPatch(patch LLMSettingsPatch) (LLMSettingsView, error) {
	if s == nil {
		return LLMSettingsView{}, fmt.Errorf("llm settings unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if patch.ActiveProfile != nil {
		id := strings.TrimSpace(*patch.ActiveProfile)
		if id == "" {
			return s.snapshotLocked(), fmt.Errorf("active_profile is required")
		}
		s.ActiveProfile = id
	}
	if !ThinkingSupported(s.Provider) {
		if patch.Thinking != nil || patch.ReasoningEffort != nil {
			return s.snapshotLocked(), fmt.Errorf("thinking controls require llm.provider=deepseek, qwen, or openai")
		}
		return s.snapshotLocked(), nil
	}
	if patch.Thinking != nil {
		normalized, err := normalizeThinking(*patch.Thinking)
		if err != nil {
			return s.snapshotLocked(), err
		}
		s.Thinking = normalized
	}
	if patch.ReasoningEffort != nil {
		normalized, err := normalizeReasoningEffort(*patch.ReasoningEffort)
		if err != nil {
			return s.snapshotLocked(), err
		}
		s.ReasoningEffort = normalized
	}
	if s.Thinking != "enabled" {
		s.ReasoningEffort = "high"
	}
	return s.snapshotLocked(), nil
}

// SyncFromConfig 将 config.yaml 中的 LLM 连接字段同步到运行时（保存设置 / 切换档案后调用）。
func (s *RuntimeSettings) SyncFromConfig(cfg *config.Config) {
	if s == nil || cfg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ActiveProfile = cfg.LLM.ActiveProfileID()
	s.profileIDs = cfg.LLM.ProfileIDs()
	s.Provider = strings.TrimSpace(cfg.LLM.Provider)
	s.BaseURL = strings.TrimSpace(cfg.LLM.BaseURL)
	s.APIKeyEnv = strings.TrimSpace(cfg.LLM.APIKeyEnv)
	s.Model = strings.TrimSpace(cfg.LLM.Model)
	s.Mock = cfg.LLM.Mock
	s.MultimodalEnabled = cfg.MultimodalEnabled()
	thinking, effort := NormalizeThinkingSettings(s.Provider, cfg.LLM.Thinking, cfg.LLM.ReasoningEffort)
	s.Thinking = thinking
	s.ReasoningEffort = effort
}

// ThinkingSupported 表示当前 provider 是否支持运行时 thinking 控制。
// openai 按 OpenAI 兼容网关常见约定注入 thinking / reasoning_effort（与 DeepSeek 同形）。
func ThinkingSupported(provider string) bool {
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderDeepSeek, ProviderQwen, ProviderOpenAI:
		return true
	default:
		return false
	}
}

// NormalizeThinkingSettings 规范化配置中的 thinking 字段。
func NormalizeThinkingSettings(provider, thinking, effort string) (string, string) {
	if !ThinkingSupported(provider) {
		return "", ""
	}
	t, _ := normalizeThinkingDefault(thinking)
	e, _ := normalizeReasoningEffortDefault(effort)
	if t != "enabled" {
		return t, "high"
	}
	return t, e
}

// BuildRequestExtra 按 provider 与 thinking 参数构造出站 JSON 扩展字段。
func BuildRequestExtra(provider, thinking, effort string) map[string]any {
	if !ThinkingSupported(provider) {
		return nil
	}
	t, e := NormalizeThinkingSettings(provider, thinking, effort)
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderQwen:
		return buildQwenRequestExtra(t, e)
	case ProviderDeepSeek, ProviderOpenAI:
		return buildDeepSeekRequestExtra(t, e)
	default:
		return nil
	}
}

func buildDeepSeekRequestExtra(thinking, effort string) map[string]any {
	if thinking == "disabled" {
		return map[string]any{
			"thinking": map[string]string{"type": "disabled"},
		}
	}
	return map[string]any{
		"thinking":         map[string]string{"type": "enabled"},
		"reasoning_effort": effort,
	}
}

func buildQwenRequestExtra(thinking, effort string) map[string]any {
	if thinking == "disabled" {
		return map[string]any{"enable_thinking": false}
	}
	extra := map[string]any{"enable_thinking": true}
	if budget := qwenThinkingBudget(effort); budget > 0 {
		extra["thinking_budget"] = budget
	}
	return extra
}

func qwenThinkingBudget(effort string) int {
	switch effort {
	case "max":
		return 32768
	case "high":
		return 8192
	default:
		return 8192
	}
}

func normalizeThinkingDefault(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "enabled", nil
	}
	return normalizeThinking(v)
}

func normalizeThinking(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "enabled", "on", "true", "1":
		return "enabled", nil
	case "disabled", "off", "false", "0":
		return "disabled", nil
	default:
		return "", fmt.Errorf("invalid thinking %q (want enabled|disabled)", raw)
	}
}

func normalizeReasoningEffortDefault(raw string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return "high", nil
	}
	return normalizeReasoningEffort(v)
}

func normalizeReasoningEffort(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "high", "low", "medium":
		return "high", nil
	case "max", "xhigh":
		return "max", nil
	default:
		return "", fmt.Errorf("invalid reasoning_effort %q (want high|max)", raw)
	}
}
