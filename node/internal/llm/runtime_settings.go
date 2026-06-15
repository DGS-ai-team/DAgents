package llm

import (
	"fmt"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// RuntimeSettings 为可热更新的 LLM 运行时参数（model、DeepSeek thinking 等）。
type RuntimeSettings struct {
	mu sync.RWMutex

	AgentID         string
	Provider        string
	Model           string
	Mock            bool
	Thinking        string
	ReasoningEffort string
}

// LLMSettingsView 为 GET /v1/llm/settings 与 agent/info 嵌套字段。
type LLMSettingsView struct {
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	Mock              bool   `json:"mock"`
	ThinkingSupported bool   `json:"thinking_supported"`
	Thinking          string `json:"thinking,omitempty"`
	ReasoningEffort   string `json:"reasoning_effort,omitempty"`
}

// LLMSettingsPatch 为 PATCH /v1/llm/settings 请求体（字段均可选）。
type LLMSettingsPatch struct {
	Thinking        *string `json:"thinking"`
	ReasoningEffort *string `json:"reasoning_effort"`
}

// NewRuntimeSettings 从配置初始化运行时 LLM 参数。
func NewRuntimeSettings(cfg *config.Config) *RuntimeSettings {
	if cfg == nil {
		return &RuntimeSettings{Provider: string(ProviderOpenAI), Thinking: "enabled", ReasoningEffort: "high"}
	}
	thinking, effort := NormalizeThinkingSettings(cfg.LLM.Provider, cfg.LLM.Thinking, cfg.LLM.ReasoningEffort)
	return &RuntimeSettings{
		AgentID:         strings.TrimSpace(cfg.AgentID),
		Provider:        strings.TrimSpace(cfg.LLM.Provider),
		Model:           strings.TrimSpace(cfg.LLM.Model),
		Mock:            cfg.LLM.Mock,
		Thinking:        thinking,
		ReasoningEffort: effort,
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
		Provider:          s.Provider,
		Model:             s.Model,
		Mock:              s.Mock,
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

// ApplyPatch 热更新 thinking / reasoning_effort。
func (s *RuntimeSettings) ApplyPatch(patch LLMSettingsPatch) (LLMSettingsView, error) {
	if s == nil {
		return LLMSettingsView{}, fmt.Errorf("llm settings unavailable")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !ThinkingSupported(s.Provider) {
		if patch.Thinking != nil || patch.ReasoningEffort != nil {
			return s.snapshotLocked(), fmt.Errorf("thinking controls require llm.provider=deepseek or qwen")
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

// ThinkingSupported 表示当前 provider 是否支持运行时 thinking 控制。
func ThinkingSupported(provider string) bool {
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderDeepSeek, ProviderQwen:
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
	default:
		return buildDeepSeekRequestExtra(t, e)
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
