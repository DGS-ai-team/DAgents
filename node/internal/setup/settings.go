package setup

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// LLMSettings LLM 连接配置（安装向导原批次 1）。
type LLMSettings struct {
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"api_key_env"`
	Mock      bool   `json:"mock"`
}

// ManageSettings Manage 连接配置（安装向导原批次 2）。
type ManageSettings struct {
	Enabled             bool   `json:"enabled"`
	URL                 string `json:"url"`
	Team                string `json:"team"`
	RegistrationBaseURL string `json:"registration_base_url"`
	A2AEnabled          bool   `json:"a2a_enabled"`
}

// FeatureSettings 功能开关（安装向导原批次 3）。
type FeatureSettings struct {
	SkillsEnabled      bool `json:"skills_enabled"`
	TriggersEnabled    bool `json:"triggers_enabled"`
	ChildAgentsEnabled bool `json:"child_agents_enabled"`
	UIEnabled          bool `json:"ui_enabled"`
	BrowserEnabled     bool `json:"browser_enabled"`
	MultimodalEnabled  bool `json:"multimodal_enabled"`
}

// CompressionSettings 上下文压缩阈值（config compression 块）。
type CompressionSettings struct {
	SilentTriggerTokens         int `json:"silent_trigger_tokens"`
	BlockingTriggerTokens       int `json:"blocking_trigger_tokens"`
	IdleAutoCompressSeconds     int `json:"idle_auto_compress_seconds"`
	IdleAutoCompressPollSeconds int `json:"idle_auto_compress_poll_seconds"`
	IdleAutoCompressMinTokens   int `json:"idle_auto_compress_min_tokens"`
}

// SettingsView GET /v1/setup/config 响应。
type SettingsView struct {
	ConfigPath      string              `json:"config_path,omitempty"`
	ConfigWritable  bool                `json:"config_writable"`
	RestartRequired bool                `json:"restart_required"`
	LLM             LLMSettings         `json:"llm"`
	Manage          ManageSettings      `json:"manage"`
	Features        FeatureSettings     `json:"features"`
	Compression     CompressionSettings `json:"compression"`
}

// SettingsPatch PATCH /v1/setup/config 请求体（字段均可选）。
type SettingsPatch struct {
	LLM         *LLMSettings         `json:"llm,omitempty"`
	Manage      *ManageSettings      `json:"manage,omitempty"`
	Features    *FeatureSettings     `json:"features,omitempty"`
	Compression *CompressionSettings `json:"compression,omitempty"`
}

// ViewFromConfig 从当前 Node 配置构造设置视图。
func ViewFromConfig(cfg *config.Config) SettingsView {
	if cfg == nil {
		return SettingsView{}
	}
	a2aEnabled := cfg.ManageA2AEnabled()
	if cfg.Manage.A2A.Enabled != nil {
		a2aEnabled = *cfg.Manage.A2A.Enabled
	}
	return SettingsView{
		LLM: LLMSettings{
			Provider:  cfg.LLM.Provider,
			BaseURL:   cfg.LLM.BaseURL,
			Model:     cfg.LLM.Model,
			APIKeyEnv: cfg.LLM.APIKeyEnv,
			Mock:      cfg.LLM.Mock,
		},
		Manage: ManageSettings{
			Enabled:             cfg.Manage.Enabled,
			URL:                 cfg.Manage.URL,
			Team:                cfg.Manage.Registration.Team,
			RegistrationBaseURL: cfg.Manage.Registration.BaseURL,
			A2AEnabled:          a2aEnabled,
		},
		Features: FeatureSettings{
			SkillsEnabled:      cfg.Skills.Enabled,
			TriggersEnabled:    cfg.Triggers.Enabled,
			ChildAgentsEnabled: cfg.ChildAgents.Enabled,
			UIEnabled:          cfg.UIEnabled(),
			BrowserEnabled:     cfg.BrowserEnabled(),
			MultimodalEnabled:  cfg.MultimodalEnabled(),
		},
		Compression: CompressionSettings{
			SilentTriggerTokens:         cfg.Compression.SilentTriggerTokens,
			BlockingTriggerTokens:       cfg.Compression.BlockingTriggerTokens,
			IdleAutoCompressSeconds:     cfg.Compression.IdleAutoCompressSeconds,
			IdleAutoCompressPollSeconds: cfg.Compression.IdleAutoCompressPollSeconds,
			IdleAutoCompressMinTokens:   cfg.Compression.IdleAutoCompressMinTokens,
		},
	}
}

// ApplyPatch 将 PATCH 合并进 cfg 副本并校验。
func ApplyPatch(cfg *config.Config, patch SettingsPatch) (*config.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	out := *cfg
	if patch.LLM != nil {
		if err := applyLLMPatch(&out, *patch.LLM); err != nil {
			return nil, err
		}
	}
	if patch.Manage != nil {
		if err := applyManagePatch(&out, *patch.Manage); err != nil {
			return nil, err
		}
	}
	if patch.Features != nil {
		applyFeaturesPatch(&out, *patch.Features)
	}
	if patch.Compression != nil {
		if err := applyCompressionPatch(&out, *patch.Compression); err != nil {
			return nil, err
		}
	}
	out.ApplyDefaults()
	if err := out.Validate(); err != nil {
		return nil, err
	}
	return &out, nil
}

func applyLLMPatch(cfg *config.Config, p LLMSettings) error {
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
	if provider == "mock" {
		mock = true
	}
	model := strings.TrimSpace(p.Model)
	if !mock && model == "" {
		return fmt.Errorf("llm.model is required when mock is false")
	}
	cfg.LLM.Provider = provider
	cfg.LLM.BaseURL = strings.TrimSpace(p.BaseURL)
	cfg.LLM.Model = model
	if env := strings.TrimSpace(p.APIKeyEnv); env != "" {
		cfg.LLM.APIKeyEnv = env
	}
	cfg.LLM.Mock = mock
	return nil
}

func applyManagePatch(cfg *config.Config, p ManageSettings) error {
	cfg.Manage.Enabled = p.Enabled
	if !p.Enabled {
		return nil
	}
	url := strings.TrimSpace(p.URL)
	if url == "" {
		return fmt.Errorf("manage.url is required when manage is enabled")
	}
	cfg.Manage.URL = url
	cfg.Manage.Registration.Team = strings.TrimSpace(p.Team)
	cfg.Manage.Registration.BaseURL = strings.TrimSpace(p.RegistrationBaseURL)
	a2a := p.A2AEnabled
	cfg.Manage.A2A.Enabled = boolPtr(a2a)
	return nil
}

func applyFeaturesPatch(cfg *config.Config, p FeatureSettings) {
	cfg.Skills.Enabled = p.SkillsEnabled
	cfg.Triggers.Enabled = p.TriggersEnabled
	cfg.ChildAgents.Enabled = p.ChildAgentsEnabled
	cfg.UI.Enabled = boolPtr(p.UIEnabled)
	cfg.Multimodal.Enabled = boolPtr(p.MultimodalEnabled)
	cfg.Browser.Enabled = boolPtr(p.BrowserEnabled)
}

func applyCompressionPatch(cfg *config.Config, p CompressionSettings) error {
	if p.SilentTriggerTokens < 0 {
		return fmt.Errorf("compression.silent_trigger_tokens must be >= 0")
	}
	if p.BlockingTriggerTokens < 0 {
		return fmt.Errorf("compression.blocking_trigger_tokens must be >= 0")
	}
	if p.IdleAutoCompressSeconds < 0 {
		return fmt.Errorf("compression.idle_auto_compress_seconds must be >= 0")
	}
	if p.IdleAutoCompressPollSeconds < 0 {
		return fmt.Errorf("compression.idle_auto_compress_poll_seconds must be >= 0")
	}
	if p.IdleAutoCompressMinTokens < 0 {
		return fmt.Errorf("compression.idle_auto_compress_min_tokens must be >= 0")
	}
	silent := p.SilentTriggerTokens
	blocking := p.BlockingTriggerTokens
	if silent > 0 && blocking > 0 && blocking < silent {
		return fmt.Errorf("compression.blocking_trigger_tokens must be >= silent_trigger_tokens when both are enabled")
	}
	cfg.Compression.SilentTriggerTokens = silent
	cfg.Compression.BlockingTriggerTokens = blocking
	cfg.Compression.IdleAutoCompressSeconds = p.IdleAutoCompressSeconds
	cfg.Compression.IdleAutoCompressPollSeconds = p.IdleAutoCompressPollSeconds
	cfg.Compression.IdleAutoCompressMinTokens = p.IdleAutoCompressMinTokens
	return nil
}

// CopyConfig 将 src 的配置字段覆盖到 dst（保持 dst 指针稳定）。
func CopyConfig(dst, src *config.Config) {
	if dst == nil || src == nil {
		return
	}
	*dst = *src
}

func boolPtr(v bool) *bool {
	b := v
	return &b
}
