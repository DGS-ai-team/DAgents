package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

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
	APIKeyEnv         string // 未存储明文 key 时使用的环境变量
	APIKey            string // 当前配置的明文 key（内存中，来自 SQLite 解密）
	Model             string
	Mock              bool
	MultimodalEnabled bool
	Thinking          string
	ReasoningEffort   string
	ThinkingBudget    *int
}

// LLMSettingsView 为 GET /v1/llm/settings 与 agent/info 嵌套字段。
type LLMSettingsView struct {
	ActiveProfile          string   `json:"active_profile,omitempty"`
	Profiles               []string `json:"profiles,omitempty"`
	Provider               string   `json:"provider"`
	Model                  string   `json:"model"`
	Mock                   bool     `json:"mock"`
	MultimodalEnabled      bool     `json:"multimodal_enabled"`
	ThinkingSupported      bool     `json:"thinking_supported"`
	ThinkingControl        string   `json:"thinking_control,omitempty"`
	ThinkingLabel          string   `json:"thinking_label,omitempty"`
	ThinkingSecondaryLabel string   `json:"thinking_secondary_label,omitempty"`
	Thinking               string   `json:"thinking,omitempty"`
	ReasoningEffort        string   `json:"reasoning_effort,omitempty"`
	ThinkingBudget         *int     `json:"thinking_budget,omitempty"`
	AllowedEfforts         []string `json:"allowed_efforts,omitempty"`
	DefaultEffort          string   `json:"default_effort,omitempty"`
	CanDisableThinking     bool     `json:"can_disable_thinking"`
	ThinkingBudgetMin      int      `json:"thinking_budget_min,omitempty"`
	ThinkingBudgetMax      int      `json:"thinking_budget_max,omitempty"`
}

// LLMSettingsPatch 为 PATCH /v1/llm/settings 请求体（字段均可选）。
type LLMSettingsPatch struct {
	ActiveProfile *string `json:"active_profile"`
}

// NewRuntimeSettings 从配置初始化运行时 LLM 参数。
func NewRuntimeSettings(cfg *config.Config) *RuntimeSettings {
	if cfg == nil {
		return &RuntimeSettings{
			Provider:        string(ProviderOpenAI),
			APIKeyEnv:       "OPENAI_API_KEY",
			Thinking:        "enabled",
			ReasoningEffort: "medium",
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
		ThinkingBudget:    copyInt(cfg.LLM.ThinkingBudget),
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
		ThinkingSupported: ThinkingSupported(s.Provider, s.Model),
	}
	view.ThinkingControl, view.ThinkingLabel, view.ThinkingSecondaryLabel = ThinkingControlMetadata(s.Provider, s.Model)
	capabilities := ModelCapabilities(s.Provider, s.Model)
	view.AllowedEfforts = capabilities.AllowedEfforts
	view.DefaultEffort = capabilities.DefaultEffort
	view.CanDisableThinking = capabilities.CanDisable
	view.ThinkingBudgetMin = capabilities.BudgetMin
	view.ThinkingBudgetMax = capabilities.BudgetMax
	if view.ThinkingSupported {
		if view.ThinkingControl == ThinkingControlFixed {
			view.Thinking = "enabled"
		} else {
			view.Thinking = s.Thinking
		}
		if capabilities.Control == ThinkingControlEffort {
			_, effort := NormalizeThinkingSettingsForModel(s.Provider, s.Model, s.Thinking, s.ReasoningEffort)
			view.ReasoningEffort = effort
		}
		if capabilities.Control == ThinkingControlBudget {
			view.ThinkingBudget = copyInt(s.ThinkingBudget)
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

// Fingerprint identifies the effective connection settings without exposing
// the API key. It lets an Agent runtime detect a changed bound profile
// without consulting process-global active-profile state.
func (s *RuntimeSettings) Fingerprint() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	h := sha256.New()
	budget := ""
	if s.ThinkingBudget != nil {
		budget = fmt.Sprint(*s.ThinkingBudget)
	}
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%t\x00%t\x00%s\x00%s\x00%s\x00%s",
		strings.TrimSpace(s.Provider),
		strings.TrimSpace(s.BaseURL),
		strings.TrimSpace(s.APIKeyEnv),
		strings.TrimSpace(s.Model),
		s.Mock,
		s.MultimodalEnabled,
		strings.TrimSpace(s.Thinking),
		strings.TrimSpace(s.ReasoningEffort),
		strings.TrimSpace(s.APIKey),
		budget,
	)
	return hex.EncodeToString(h.Sum(nil))
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
	if built := BuildRequestExtraForProfile(s.Provider, s.Model, s.Thinking, s.ReasoningEffort, s.ThinkingBudget); len(built) > 0 {
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
	s.ThinkingBudget = copyInt(cfg.LLM.ThinkingBudget)
}

// ThinkingControl describes the provider/model-specific controls the API has verified.
const (
	ThinkingControlEffort = "effort"
	ThinkingControlBudget = "budget"
	ThinkingControlToggle = "toggle"
	ThinkingControlFixed  = "fixed"
)

// ModelCapability is the single capability description shared by the settings UI
// and request adapter. Unknown/custom model names deliberately have no controls.
type ModelCapability struct {
	Control        string   `json:"thinking_control,omitempty"`
	Label          string   `json:"thinking_label,omitempty"`
	SecondaryLabel string   `json:"thinking_secondary_label,omitempty"`
	AllowedEfforts []string `json:"allowed_efforts,omitempty"`
	DefaultEffort  string   `json:"default_effort,omitempty"`
	CanDisable     bool     `json:"can_disable_thinking"`
	BudgetMin      int      `json:"thinking_budget_min,omitempty"`
	BudgetMax      int      `json:"thinking_budget_max,omitempty"`
}

func ModelCapabilities(provider, model string) ModelCapability {
	p := ProviderName(strings.ToLower(strings.TrimSpace(provider)))
	m := strings.ToLower(strings.TrimSpace(model))
	switch p {
	case ProviderDeepSeek:
		if matchesVerifiedModel(m, "deepseek-flash", "deepseek-pro", "deepseek-v4-flash", "deepseek-v4-pro") {
			return ModelCapability{Control: ThinkingControlEffort, Label: "思考", SecondaryLabel: "推理强度", AllowedEfforts: []string{"low", "high", "max"}, DefaultEffort: "high", CanDisable: true}
		}
	case ProviderOpenAI:
		if matchesVerifiedModel(m, "gpt-5.1") {
			return ModelCapability{Control: ThinkingControlEffort, Label: "推理", SecondaryLabel: "推理强度", AllowedEfforts: []string{"none", "low", "medium", "high"}, DefaultEffort: "medium"}
		}
		if matchesVerifiedModel(m, "gpt-5") {
			return ModelCapability{Control: ThinkingControlEffort, Label: "推理", SecondaryLabel: "推理强度", AllowedEfforts: []string{"minimal", "low", "medium", "high"}, DefaultEffort: "medium"}
		}
		if matchesVerifiedModel(m, "o1", "o3", "o3-mini", "o4-mini") {
			efforts := []string{"low", "medium", "high"}
			return ModelCapability{Control: ThinkingControlEffort, Label: "推理", SecondaryLabel: "推理强度", AllowedEfforts: efforts, DefaultEffort: "medium"}
		}
	case ProviderQwen:
		if matchesVerifiedModel(m, "qwen-plus", "qwen-flash") {
			return ModelCapability{Control: ThinkingControlBudget, Label: "深度思考", SecondaryLabel: "思考预算（Token）", CanDisable: true, BudgetMin: 1, BudgetMax: 32768}
		}
		if matchesVerifiedModel(m, "qwen3-8b", "qwen3-14b", "qwen3-32b", "qwen3-30b-a3b", "qwen3-235b-a22b") {
			return ModelCapability{Control: ThinkingControlBudget, Label: "深度思考", SecondaryLabel: "思考预算（Token）", CanDisable: true, BudgetMin: 1, BudgetMax: 32768}
		}
		if matchesVerifiedModel(m, "qwq-32b") {
			return ModelCapability{Control: ThinkingControlBudget, Label: "深度思考", SecondaryLabel: "思考预算（Token）", BudgetMin: 1, BudgetMax: 32768}
		}
	case ProviderGLM:
		if matchesVerifiedModel(m, "glm-5.3", "glm-5.3-flash", "glm-5.3-flashx") {
			return ModelCapability{Control: ThinkingControlEffort, Label: "思考", SecondaryLabel: "推理强度", AllowedEfforts: []string{"low", "high", "max"}, DefaultEffort: "max"}
		}
		if matchesVerifiedModel(m, "glm-5.2") {
			return ModelCapability{Control: ThinkingControlEffort, Label: "思考", SecondaryLabel: "推理强度", AllowedEfforts: []string{"high", "max"}, DefaultEffort: "max", CanDisable: true}
		}
		if supportedGLMToggleModel(m) {
			return ModelCapability{Control: ThinkingControlToggle, Label: "思考", CanDisable: true}
		}
	case ProviderMiMo:
		if matchesVerifiedModel(m, "mimo-v2.5", "mimo-v2.5-pro", "mimo-v2-flash") {
			return ModelCapability{Control: ThinkingControlToggle, Label: "深度思考", CanDisable: true}
		}
	case ProviderMiniMax:
		if matchesVerifiedModel(m, "minimax-m3.1-flash") {
			return ModelCapability{Control: ThinkingControlEffort, Label: "思考", SecondaryLabel: "推理强度", AllowedEfforts: []string{"low", "medium", "high", "xhigh", "max"}, DefaultEffort: "max"}
		}
		if matchesVerifiedModel(m, "minimax-m3") {
			return ModelCapability{Control: ThinkingControlToggle, Label: "思考", CanDisable: true}
		}
		if matchesVerifiedModel(m, "minimax-m2", "minimax-m2.1", "minimax-m2.5") {
			return ModelCapability{Control: ThinkingControlFixed, Label: "思考"}
		}
	}
	return ModelCapability{}
}

func ValidateThinkingSettings(provider, model, thinking, effort string, budget *int) error {
	capability := ModelCapabilities(provider, model)
	if capability.Control == "" {
		return nil
	}
	if strings.TrimSpace(thinking) != "" {
		normalized, err := normalizeThinking(thinking)
		if err != nil {
			return err
		}
		if !capability.CanDisable && normalized == "disabled" {
			return fmt.Errorf("thinking cannot be disabled for %s/%s", provider, model)
		}
	}
	if value := strings.TrimSpace(effort); value != "" {
		if capability.Control != ThinkingControlEffort {
			return fmt.Errorf("reasoning_effort is not supported by %s/%s", provider, model)
		}
		allowed := false
		for _, option := range capability.AllowedEfforts {
			if strings.EqualFold(value, option) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("invalid reasoning_effort %q for %s/%s", effort, provider, model)
		}
	}
	if budget != nil {
		if capability.Control != ThinkingControlBudget {
			return fmt.Errorf("thinking_budget is not supported by %s/%s", provider, model)
		}
		if *budget < capability.BudgetMin || *budget > capability.BudgetMax {
			return fmt.Errorf("thinking_budget must be between %d and %d", capability.BudgetMin, capability.BudgetMax)
		}
	}
	return nil
}

func matchesVerifiedModel(model string, names ...string) bool {
	for _, name := range names {
		if model == name {
			return true
		}
		prefix := name + "-"
		if !strings.HasPrefix(model, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(model, prefix)
		for _, layout := range []string{"20060102", "2006-01-02"} {
			if _, err := time.Parse(layout, suffix); err == nil {
				return true
			}
		}
	}
	return false
}

func supportedGLMToggleModel(model string) bool {
	return matchesVerifiedModel(model, "glm-4.5", "glm-4.5-air", "glm-4.5-flash", "glm-4.6", "glm-4.7")
}

func ThinkingSupported(provider, model string) bool {
	return ModelCapabilities(provider, model).Control != ""
}

func ThinkingControl(provider, model string) string {
	return ModelCapabilities(provider, model).Control
}

// ThinkingControlMetadata returns labels that keep provider-specific terminology out
// of the web UI while preserving a small, stable API contract.
func ThinkingControlMetadata(provider, model string) (control, label, secondaryLabel string) {
	capability := ModelCapabilities(provider, model)
	return capability.Control, capability.Label, capability.SecondaryLabel
}

// NormalizeThinkingSettingsForModel additionally applies model-specific restrictions.
func NormalizeThinkingSettingsForModel(provider, model, thinking, effort string) (string, string) {
	capability := ModelCapabilities(provider, model)
	if capability.Control == "" {
		return "", ""
	}
	if capability.Control == ThinkingControlFixed {
		return "enabled", ""
	}
	t, _ := normalizeThinkingDefault(thinking)
	if !capability.CanDisable {
		t = "enabled"
	}
	if capability.Control != ThinkingControlEffort {
		return t, ""
	}
	e, _ := normalizeReasoningEffortForModel(effort, capability.AllowedEfforts, capability.DefaultEffort)
	if t != "enabled" {
		return t, capability.DefaultEffort
	}
	return t, e
}

// BuildRequestExtraForModel builds provider-specific request fields with model-aware
// restrictions.
func BuildRequestExtraForModel(provider, model, thinking, effort string) map[string]any {
	return BuildRequestExtraForProfile(provider, model, thinking, effort, nil)
}

func BuildRequestExtraForProfile(provider, model, thinking, effort string, budget *int) map[string]any {
	capability := ModelCapabilities(provider, model)
	if capability.Control == "" {
		return nil
	}
	t, e := NormalizeThinkingSettingsForModel(provider, model, thinking, effort)
	switch ProviderName(strings.ToLower(strings.TrimSpace(provider))) {
	case ProviderQwen:
		return buildQwenRequestExtra(t, budget)
	case ProviderDeepSeek:
		return buildDeepSeekRequestExtra(t, e)
	case ProviderOpenAI:
		return map[string]any{"reasoning_effort": e}
	case ProviderGLM:
		return buildGLMRequestExtra(t, e, capability.Control)
	case ProviderMiniMax:
		return buildMiniMaxRequestExtra(t, e, capability.Control)
	case ProviderMiMo:
		return buildMiMoRequestExtra(t)
	default:
		return nil
	}
}

func buildGLMRequestExtra(thinking, effort, control string) map[string]any {
	if control == ThinkingControlEffort {
		extra := map[string]any{"reasoning_effort": effort}
		if thinking == "disabled" {
			extra["thinking"] = map[string]string{"type": "disabled"}
		} else {
			extra["thinking"] = map[string]any{"type": "enabled", "clear_thinking": false}
		}
		return extra
	}
	if thinking == "disabled" {
		return map[string]any{"thinking": map[string]string{"type": "disabled"}}
	}
	// GLM's preserved-thinking mode is the safe mode for multi-turn tool use:
	// the assistant reasoning_content is returned and must be sent back intact.
	return map[string]any{
		"thinking": map[string]any{"type": "enabled", "clear_thinking": false},
	}
}

func buildMiniMaxRequestExtra(thinking, effort, control string) map[string]any {
	if control == ThinkingControlEffort {
		return map[string]any{"reasoning_effort": effort, "reasoning_split": true}
	}
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

func buildQwenRequestExtra(thinking string, budget *int) map[string]any {
	if thinking == "disabled" {
		return map[string]any{"enable_thinking": false}
	}
	extra := map[string]any{"enable_thinking": true}
	if budget != nil && *budget > 0 {
		extra["thinking_budget"] = *budget
	}
	return extra
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

func normalizeReasoningEffortForModel(raw string, allowed []string, fallback string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return fallback, nil
	}
	for _, option := range allowed {
		if v == option {
			return v, nil
		}
	}
	return fallback, nil
}

func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
