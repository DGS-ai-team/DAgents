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

	AgentID           string
	ActiveProfile     string
	profileIDs        []string
	Provider          string
	BaseURL           string
	APIKeyEnv         string // 兼容旧逻辑：无明文 key 时回退环境变量
	APIKey            string // 当前配置的明文 key（内存中，来自 SQLite 解密）
	Model             string
	Mock              bool
	MultimodalEnabled bool
	Thinking          string
	ReasoningEffort   string
}

// LLMSettingsView 为 GET /v1/llm/settings 与 agent/info 嵌套字段。
type LLMSettingsView struct {
	ActiveProfile            string   `json:"active_profile,omitempty"`
	Profiles                 []string `json:"profiles,omitempty"`
	Provider                 string   `json:"provider"`
	Model                    string   `json:"model"`
	Mock                     bool     `json:"mock"`
	MultimodalEnabled        bool     `json:"multimodal_enabled"`
	ThinkingSupported        bool     `json:"thinking_supported"`
	ReasoningEffortSupported bool     `json:"reasoning_effort_supported"`
	ThinkingControl          string   `json:"thinking_control,omitempty"`
	ThinkingLabel            string   `json:"thinking_label,omitempty"`
	ThinkingSecondaryLabel   string   `json:"thinking_secondary_label,omitempty"`
	Thinking                 string   `json:"thinking,omitempty"`
	ReasoningEffort          string   `json:"reasoning_effort,omitempty"`
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
	thinking, effort := NormalizeThinkingSettingsForModel(cfg.LLM.Provider, cfg.LLM.Model, cfg.LLM.Thinking, cfg.LLM.ReasoningEffort)
	return &RuntimeSettings{
		AgentID:           strings.TrimSpace(cfg.NodeID),
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
		ActiveProfile:            s.ActiveProfile,
		Profiles:                 append([]string(nil), s.profileIDs...),
		Provider:                 s.Provider,
		Model:                    s.Model,
		Mock:                     s.Mock,
		MultimodalEnabled:        s.MultimodalEnabled,
		ThinkingSupported:        ThinkingSupported(s.Provider),
		ReasoningEffortSupported: ReasoningEffortSupported(s.Provider),
	}
	view.ThinkingControl, view.ThinkingLabel, view.ThinkingSecondaryLabel = ThinkingControlMetadata(s.Provider, s.Model)
	if view.ThinkingSupported {
		if view.ThinkingControl == ThinkingControlFixed {
			view.Thinking = "enabled"
		} else {
			view.Thinking = s.Thinking
		}
		if s.Thinking == "enabled" && view.ReasoningEffortSupported {
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

// APIKeyValue 返回当前明文 API Key（可能为空）。
func (s *RuntimeSettings) APIKeyValue() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.APIKey)
}

// SetAPIKey 更新内存中的 API Key（切换配置 / 保存后调用）。
func (s *RuntimeSettings) SetAPIKey(key string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.APIKey = strings.TrimSpace(key)
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
	if built := BuildRequestExtraForModel(s.Provider, s.Model, s.Thinking, s.ReasoningEffort); len(built) > 0 {
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
			return s.snapshotLocked(), fmt.Errorf("thinking controls are not supported by llm.provider=%s", s.Provider)
		}
		return s.snapshotLocked(), nil
	}
	if ThinkingControl(s.Provider, s.Model) == ThinkingControlFixed && patch.Thinking != nil {
		return s.snapshotLocked(), fmt.Errorf("thinking cannot be changed for llm.model=%s", s.Model)
	}
	if patch.Thinking != nil {
		normalized, err := normalizeThinking(*patch.Thinking)
		if err != nil {
			return s.snapshotLocked(), err
		}
		s.Thinking = normalized
	}
	if patch.ReasoningEffort != nil {
		if !ReasoningEffortSupported(s.Provider) {
			return s.snapshotLocked(), fmt.Errorf("reasoning effort is not supported by llm.provider=%s", s.Provider)
		}
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
	thinking, effort := NormalizeThinkingSettingsForModel(s.Provider, cfg.LLM.Model, cfg.LLM.Thinking, cfg.LLM.ReasoningEffort)
	s.Thinking = thinking
	s.ReasoningEffort = effort
}

// ThinkingSupported 表示当前 provider 是否支持运行时 thinking 控制。
// openai 按 OpenAI 兼容网关常见约定注入 thinking / reasoning_effort（与 DeepSeek 同形）。
func ThinkingSupported(provider string) bool {
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderDeepSeek, ProviderQwen, ProviderOpenAI, ProviderGLM, ProviderMiniMax, ProviderMiMo:
		return true
	default:
		return false
	}
}

// ReasoningEffortSupported 表示 provider 是否接受 high/max 这套强度参数。
// GLM、MiniMax、MiMo 的 OpenAI-compatible 接口使用各自的 thinking 结构，
// 不把 DeepSeek/Qwen 的 reasoning_effort 字段发给它们。
func ReasoningEffortSupported(provider string) bool {
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderDeepSeek, ProviderQwen, ProviderOpenAI:
		return true
	default:
		return false
	}
}

// ThinkingControl describes the controls that are meaningful for a provider/model.
// effort and budget both expose the thinking toggle plus a high/max secondary control;
// toggle only exposes the on/off switch; fixed exposes a read-only status.
const (
	ThinkingControlEffort = "effort"
	ThinkingControlBudget = "budget"
	ThinkingControlToggle = "toggle"
	ThinkingControlFixed  = "fixed"
)

// ThinkingControl returns the UI/request control shape for the current provider/model.
// MiniMax M3 supports adaptive/disabled thinking; older MiniMax models keep thinking
// enabled and must not present a fake off switch.
func ThinkingControl(provider, model string) string {
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderDeepSeek, ProviderOpenAI:
		return ThinkingControlEffort
	case ProviderQwen:
		return ThinkingControlBudget
	case ProviderGLM, ProviderMiMo:
		return ThinkingControlToggle
	case ProviderMiniMax:
		if strings.Contains(strings.ToLower(strings.TrimSpace(model)), "m3") || strings.TrimSpace(model) == "" {
			return ThinkingControlToggle
		}
		return ThinkingControlFixed
	default:
		return ""
	}
}

// ThinkingControlMetadata returns labels that keep provider-specific terminology out
// of the web UI while preserving a small, stable API contract.
func ThinkingControlMetadata(provider, model string) (control, label, secondaryLabel string) {
	control = ThinkingControl(provider, model)
	switch control {
	case ThinkingControlEffort:
		return control, "思考", "推理强度"
	case ThinkingControlBudget:
		return control, "思考", "思考预算"
	case ThinkingControlToggle:
		if ProviderName(strings.ToLower(strings.TrimSpace(provider))) == ProviderMiMo {
			return control, "深度思考", ""
		}
		return control, "思考", ""
	case ThinkingControlFixed:
		return control, "思考", ""
	default:
		return "", "", ""
	}
}

// NormalizeThinkingSettings 规范化配置中的 thinking 字段。
func NormalizeThinkingSettings(provider, thinking, effort string) (string, string) {
	return NormalizeThinkingSettingsForModel(provider, "", thinking, effort)
}

// NormalizeThinkingSettingsForModel additionally applies model-specific restrictions.
func NormalizeThinkingSettingsForModel(provider, model, thinking, effort string) (string, string) {
	if !ThinkingSupported(provider) {
		return "", ""
	}
	if ThinkingControl(provider, model) == ThinkingControlFixed {
		return "enabled", ""
	}
	t, _ := normalizeThinkingDefault(thinking)
	if !ReasoningEffortSupported(provider) {
		return t, ""
	}
	e, _ := normalizeReasoningEffortDefault(effort)
	if t != "enabled" {
		return t, "high"
	}
	return t, e
}

// BuildRequestExtraForModel builds provider-specific request fields with model-aware
// restrictions.
func BuildRequestExtraForModel(provider, model, thinking, effort string) map[string]any {
	if !ThinkingSupported(provider) {
		return nil
	}
	t, e := NormalizeThinkingSettingsForModel(provider, model, thinking, effort)
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderQwen:
		return buildQwenRequestExtra(t, e)
	case ProviderDeepSeek, ProviderOpenAI:
		return buildDeepSeekRequestExtra(t, e)
	case ProviderGLM:
		return buildGLMRequestExtra(t)
	case ProviderMiniMax:
		return buildMiniMaxRequestExtra(t)
	case ProviderMiMo:
		return buildMiMoRequestExtra(t)
	default:
		return nil
	}
}

func buildGLMRequestExtra(thinking string) map[string]any {
	if thinking == "disabled" {
		return map[string]any{"thinking": map[string]string{"type": "disabled"}}
	}
	// GLM's preserved-thinking mode is the safe mode for multi-turn tool use:
	// the assistant reasoning_content is returned and must be sent back intact.
	return map[string]any{
		"thinking": map[string]any{"type": "enabled", "clear_thinking": false},
	}
}

func buildMiniMaxRequestExtra(thinking string) map[string]any {
	thinkingType := "adaptive"
	if thinking == "disabled" {
		thinkingType = "disabled"
	}
	return map[string]any{
		"thinking":        map[string]string{"type": thinkingType},
		"reasoning_split": true,
	}
}

func buildMiMoRequestExtra(thinking string) map[string]any {
	typ := "enabled"
	if thinking == "disabled" {
		typ = "disabled"
	}
	return map[string]any{"thinking": map[string]string{"type": typ}}
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
