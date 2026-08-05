package setup

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// LLMProfileSettings 单个 LLM 配置（Web UI / setup API）。
type LLMProfileSettings struct {
	ID                string `json:"id"`
	Provider          string `json:"provider"`
	BaseURL           string `json:"base_url"`
	Model             string `json:"model"`
	APIKeyEnv         string `json:"api_key_env,omitempty"` // 兼容旧客户端；新流程请用 api_key
	APIKey            string `json:"api_key,omitempty"`     // 仅 PATCH 写入；GET 不回传明文
	HasAPIKey         bool   `json:"has_api_key"`
	ClearAPIKey       bool   `json:"clear_api_key,omitempty"`
	Mock              bool   `json:"mock"`
	Thinking          string `json:"thinking,omitempty"`
	ReasoningEffort   string `json:"reasoning_effort,omitempty"`
	MultimodalEnabled bool   `json:"multimodal_enabled"`
}

// LLMSettings LLM 连接配置（支持多配置；列表顺序中第一条为默认）。
type LLMSettings struct {
	Active       string               `json:"active,omitempty"` // 运行时当前选用；缺省取 profiles[0]
	Profiles     []LLMProfileSettings `json:"profiles"`
	Provider  string `json:"provider"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKeyEnv string `json:"api_key_env,omitempty"`
	Mock      bool   `json:"mock"`
}

// ManageSettings Manage 连接配置（安装向导原批次 2）。
type ManageSettings struct {
	Enabled                     bool   `json:"enabled"`
	URL                         string `json:"url"`
	Team                        string `json:"team"`
	RegistrationBaseURL         string `json:"registration_base_url"`
	WorkgroupEnabled            bool   `json:"workgroup_enabled"`
	NodeToken                   string `json:"node_token"`
	RegistrationIntervalSeconds int    `json:"registration_interval_seconds"`
	RegistrationTTLSeconds      int    `json:"registration_ttl_seconds"`
}

// FeatureSettings 功能开关（安装向导原批次 3）。
type FeatureSettings struct {
	SkillsEnabled            bool `json:"skills_enabled"`
	TriggersEnabled          bool `json:"triggers_enabled"`
	ChildAgentsEnabled       bool `json:"child_agents_enabled"`
	UIEnabled                bool `json:"ui_enabled"`
	BrowserEnabled           bool `json:"browser_enabled"`
	WeComEnabled             bool `json:"wecom_enabled"`
	MultimodalEnabled        bool `json:"multimodal_enabled"`
	SkillsMaxInPrompt        int  `json:"skills_max_in_prompt"`
	TriggersPollSeconds      int  `json:"triggers_poll_seconds"`
	RawMessageHistoryEnabled bool `json:"raw_message_history_enabled"`
}

// CompressionSettings 上下文压缩阈值（config compression 块）。
type CompressionSettings struct {
	SilentTriggerTokens         int `json:"silent_trigger_tokens"`
	BlockingTriggerTokens       int `json:"blocking_trigger_tokens"`
	IdleAutoCompressSeconds     int `json:"idle_auto_compress_seconds"`
	IdleAutoCompressPollSeconds int `json:"idle_auto_compress_poll_seconds"`
	IdleAutoCompressMinTokens   int `json:"idle_auto_compress_min_tokens"`
}

// NodeEndpointView 只读：Node 监听地址（须改 config.yaml 并重启）。
type NodeEndpointView struct {
	ListenHost    string `json:"listen_host"`
	ListenPort    int    `json:"listen_port"`
	LocalEndpoint string `json:"local_endpoint"`
}

// RuntimeSettings 运行时路径与日志（不含 listen/local）。
type RuntimeSettings struct {
	NodeID   string `json:"node_id"`
	FSRoot   string `json:"fs_root"`
	LogLevel string `json:"log_level"`
}

// AgentSettings Node 展示身份（历史字段名 agent；UI 称 Node 名称）。
type AgentSettings struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Role        string `json:"role,omitempty"` // deprecated，可选元数据
}

// UserSettings 本机使用者称呼。
type UserSettings struct {
	PreferredName string `json:"preferred_name"`
}

// OnboardingSettings 首次 Node 身份配置门闩。
type OnboardingSettings struct {
	NodeProfileCompleted bool `json:"node_profile_completed"`
}

// ChildAgentsLimits 子 Agent 配额（enabled 见 features）。
type ChildAgentsLimits struct {
	DefaultTTLSeconds         int `json:"default_ttl_seconds"`
	MaxTTLSeconds             int `json:"max_ttl_seconds"`
	DefaultMaxTurns           int `json:"default_max_turns"`
	MaxMaxTurns               int `json:"max_max_turns"`
	MaxActivePerParent        int `json:"max_active_per_parent"`
	DefaultWaitTimeoutSeconds int `json:"default_wait_timeout_seconds"`
}

// BrowserSettings Browser 工具参数（enabled 见 features）。
type BrowserSettings struct {
	ServiceURL        string `json:"service_url"`
	Headed            bool   `json:"headed"`
	DefaultTimeoutMS  int    `json:"default_timeout_ms"`
	OutputDir         string `json:"output_dir"`
	MaxSessions       int    `json:"max_sessions"`
	IgnoreHTTPSErrors bool   `json:"ignore_https_errors"`
	ChromePath        string `json:"chrome_path"`
	CDPURL            string `json:"cdp_url"`
}

// WeComSettings 企业微信消息推送（enabled 见 features）。
type WeComSettings struct {
	// WebhookURL 可填完整 webhook 地址，或仅填 key；保存时规范化。
	WebhookURL string `json:"webhook_url"`
	// WebhookKey 显式密钥；空则保留原值，除非 ClearWebhookKey。
	WebhookKey string `json:"webhook_key,omitempty"`
	// HasWebhookKey GET 时表示是否已配置密钥（不回明文 key）。
	HasWebhookKey bool `json:"has_webhook_key"`
	// ClearWebhookKey 为 true 时清空密钥与 URL。
	ClearWebhookKey bool `json:"clear_webhook_key,omitempty"`
	APIBase         string `json:"api_base"`
}

// ToolsSettings 内置工具组与编码。
type ToolsSettings struct {
	EnabledGroups              []string `json:"enabled_groups"`
	BashOutputEncoding         string   `json:"bash_output_encoding"`
	FileEncoding               string   `json:"file_encoding"`
	BashCompressEnabled        bool     `json:"bash_compress_enabled"`
	BashCompressMaxOutputChars int      `json:"bash_compress_max_output_chars"`
	BashCompressMaxStderrChars int      `json:"bash_compress_max_stderr_chars"`
}

// HooksSettings 常用 Hook 开关（不含 plugin 列表）。
type HooksSettings struct {
	DuplicateToolCallEnabled       bool `json:"duplicate_tool_call_enabled"`
	DuplicateToolCallWindowSeconds int  `json:"duplicate_tool_call_window_seconds"`
	ToolResultEnabled              bool `json:"tool_result_enabled"`
	ToolResultSpillThresholdTokens int  `json:"tool_result_spill_threshold_tokens"`
	InjectTodayDateEnabled         bool `json:"inject_today_date_enabled"`
}

// SettingsView GET /v1/setup/config 响应。
type SettingsView struct {
	ConfigPath      string              `json:"config_path,omitempty"`
	ConfigWritable  bool                `json:"config_writable"`
	RestartRequired bool                `json:"restart_required"`
	Node            NodeEndpointView    `json:"node"`
	LLM             LLMSettings         `json:"llm"`
	Manage          ManageSettings      `json:"manage"`
	Features        FeatureSettings     `json:"features"`
	Compression     CompressionSettings `json:"compression"`
	Runtime         RuntimeSettings     `json:"runtime"`
	Agent           AgentSettings       `json:"agent"`
	User            UserSettings        `json:"user"`
	Onboarding      OnboardingSettings  `json:"onboarding"`
	ChildAgents     ChildAgentsLimits   `json:"child_agents"`
	Browser         BrowserSettings     `json:"browser"`
	WeCom           WeComSettings       `json:"wecom"`
	Tools           ToolsSettings       `json:"tools"`
	Hooks           HooksSettings       `json:"hooks"`
}

// SettingsPatch PATCH /v1/setup/config 请求体（字段均可选）。
type SettingsPatch struct {
	LLM         *LLMSettings         `json:"llm,omitempty"`
	Manage      *ManageSettings      `json:"manage,omitempty"`
	Features    *FeatureSettings     `json:"features,omitempty"`
	Compression *CompressionSettings `json:"compression,omitempty"`
	Runtime     *RuntimeSettings     `json:"runtime,omitempty"`
	Agent       *AgentSettings       `json:"agent,omitempty"`
	User        *UserSettings        `json:"user,omitempty"`
	Onboarding  *OnboardingSettings  `json:"onboarding,omitempty"`
	ChildAgents *ChildAgentsLimits   `json:"child_agents,omitempty"`
	Browser     *BrowserSettings     `json:"browser,omitempty"`
	WeCom       *WeComSettings       `json:"wecom,omitempty"`
	Tools       *ToolsSettings       `json:"tools,omitempty"`
	Hooks       *HooksSettings       `json:"hooks,omitempty"`
}

// ViewFromConfig 从当前 Node 配置构造设置视图。
func ViewFromConfig(cfg *config.Config) SettingsView {
	if cfg == nil {
		return SettingsView{}
	}
	workgroupEnabled := true
	if cfg.Manage.Workgroup.Enabled != nil {
		workgroupEnabled = *cfg.Manage.Workgroup.Enabled
	}
	return SettingsView{
		Node: NodeEndpointView{
			ListenHost:    cfg.Listen.Host,
			ListenPort:    cfg.Listen.Port,
			LocalEndpoint: cfg.Local.Endpoint,
		},
		LLM: LLMSettings{
			Active:    cfg.LLM.ActiveProfileID(),
			Profiles:  llmProfilesFromConfig(cfg),
			Provider:  cfg.LLM.Provider,
			BaseURL:   cfg.LLM.BaseURL,
			Model:     cfg.LLM.Model,
			APIKeyEnv: cfg.LLM.APIKeyEnv,
			Mock:      cfg.LLM.Mock,
		},
		Manage: ManageSettings{
			Enabled:                     cfg.Manage.Enabled,
			URL:                         cfg.Manage.URL,
			Team:                        cfg.Manage.Registration.Team,
			RegistrationBaseURL:         cfg.Manage.Registration.BaseURL,
			WorkgroupEnabled:            workgroupEnabled,
			NodeToken:                   cfg.Manage.NodeToken,
			RegistrationIntervalSeconds: cfg.Manage.Registration.IntervalSeconds,
			RegistrationTTLSeconds:      cfg.Manage.Registration.TTLSeconds,
		},
		Features: FeatureSettings{
			SkillsEnabled:            cfg.Skills.Enabled,
			TriggersEnabled:          cfg.Triggers.Enabled,
			ChildAgentsEnabled:       cfg.ChildAgents.Enabled,
			UIEnabled:                cfg.UIEnabled(),
			BrowserEnabled:           cfg.BrowserEnabled(),
			WeComEnabled:             cfg.WeComEnabled(),
			MultimodalEnabled:        cfg.MultimodalEnabled(),
			SkillsMaxInPrompt:        cfg.Skills.MaxInPrompt,
			TriggersPollSeconds:      cfg.Triggers.PollSeconds,
			RawMessageHistoryEnabled: cfg.RawMessageHistoryEnabled(),
		},
		Compression: CompressionSettings{
			SilentTriggerTokens:         cfg.Compression.SilentTriggerTokens,
			BlockingTriggerTokens:       cfg.Compression.BlockingTriggerTokens,
			IdleAutoCompressSeconds:     cfg.Compression.IdleAutoCompressSeconds,
			IdleAutoCompressPollSeconds: cfg.Compression.IdleAutoCompressPollSeconds,
			IdleAutoCompressMinTokens:   cfg.Compression.IdleAutoCompressMinTokens,
		},
		Runtime: RuntimeSettings{
			NodeID:   cfg.NodeID,
			FSRoot:   cfg.FSRoot,
			LogLevel: cfg.Log.Level,
		},
		Agent: AgentSettings{
			Name:        cfg.Agent.Name,
			Description: cfg.Agent.Description,
			Role:        cfg.Agent.Role,
		},
		User: UserSettings{
			PreferredName: cfg.PreferredName(),
		},
		Onboarding: OnboardingSettings{
			NodeProfileCompleted: cfg.NodeProfileCompleted(),
		},
		ChildAgents: ChildAgentsLimits{
			DefaultTTLSeconds:         cfg.ChildAgents.DefaultTTLSeconds,
			MaxTTLSeconds:             cfg.ChildAgents.MaxTTLSeconds,
			DefaultMaxTurns:           cfg.ChildAgents.DefaultMaxTurns,
			MaxMaxTurns:               cfg.ChildAgents.MaxMaxTurns,
			MaxActivePerParent:        cfg.ChildAgents.MaxActivePerParent,
			DefaultWaitTimeoutSeconds: cfg.ChildAgents.DefaultWaitTimeoutSeconds,
		},
		Browser: BrowserSettings{
			ServiceURL:        cfg.Browser.ServiceURL,
			Headed:            cfg.BrowserHeaded(),
			DefaultTimeoutMS:  cfg.Browser.DefaultTimeoutMS,
			OutputDir:         cfg.Browser.OutputDir,
			MaxSessions:       cfg.Browser.MaxSessions,
			IgnoreHTTPSErrors: cfg.Browser.BrowserIgnoreHTTPSErrors(),
			ChromePath:        cfg.Browser.ChromePath,
			CDPURL:            cfg.Browser.CDPURL,
		},
		WeCom: weComSettingsFromConfig(cfg),
		Tools: ToolsSettings{
			EnabledGroups:              append([]string(nil), cfg.Tools.EnabledGroups...),
			BashOutputEncoding:         cfg.Tools.BashOutputEncoding,
			FileEncoding:               cfg.Tools.FileEncoding,
			BashCompressEnabled:        cfg.Tools.BashCompress.Enabled == nil || *cfg.Tools.BashCompress.Enabled,
			BashCompressMaxOutputChars: cfg.Tools.BashCompress.MaxOutputChars,
			BashCompressMaxStderrChars: cfg.Tools.BashCompress.MaxOutputCharsStderr,
		},
		Hooks: HooksSettings{
			DuplicateToolCallEnabled:       cfg.DuplicateToolCallHookEnabled(),
			DuplicateToolCallWindowSeconds: cfg.DuplicateToolCallWindowSeconds(),
			ToolResultEnabled:              cfg.ToolResultHookEnabled(),
			ToolResultSpillThresholdTokens: cfg.ToolResultSpillThresholdTokens(),
			InjectTodayDateEnabled:         cfg.InjectTodayDateHookEnabled(),
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
		// 功能开关里的 multimodal 写回当前 LLM 档案（兼容旧客户端）。
		out.SyncActiveProfileFromFlat()
	}
	if patch.Compression != nil {
		if err := applyCompressionPatch(&out, *patch.Compression); err != nil {
			return nil, err
		}
	}
	if patch.Runtime != nil {
		if err := applyRuntimePatch(&out, *patch.Runtime); err != nil {
			return nil, err
		}
	}
	if patch.Agent != nil {
		applyAgentPatch(&out, *patch.Agent)
	}
	if patch.User != nil {
		if err := applyUserPatch(&out, *patch.User); err != nil {
			return nil, err
		}
	}
	if patch.Onboarding != nil {
		if err := applyOnboardingPatch(&out, *patch.Onboarding); err != nil {
			return nil, err
		}
	}
	if patch.ChildAgents != nil {
		if err := applyChildAgentsPatch(&out, *patch.ChildAgents); err != nil {
			return nil, err
		}
	}
	if patch.Browser != nil {
		if err := applyBrowserPatch(&out, *patch.Browser); err != nil {
			return nil, err
		}
	}
	if patch.WeCom != nil {
		if err := applyWeComPatch(&out, *patch.WeCom); err != nil {
			return nil, err
		}
	}
	if patch.Tools != nil {
		if err := applyToolsPatch(&out, *patch.Tools); err != nil {
			return nil, err
		}
	}
	if patch.Hooks != nil {
		if err := applyHooksPatch(&out, *patch.Hooks); err != nil {
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
	if len(p.Profiles) > 0 {
		next := make(map[string]config.LLMProfileConfig, len(p.Profiles))
		order := make([]string, 0, len(p.Profiles))
		for _, item := range p.Profiles {
			id := strings.TrimSpace(item.ID)
			if id == "" {
				return fmt.Errorf("llm config id is required")
			}
			prevThinking, prevEffort := "", ""
			if prev, ok := cfg.LLM.GetProfile(id); ok {
				prevThinking = prev.Thinking
				prevEffort = prev.ReasoningEffort
			}
			thinking := strings.TrimSpace(item.Thinking)
			if thinking == "" {
				thinking = prevThinking
			}
			effort := strings.TrimSpace(item.ReasoningEffort)
			if effort == "" {
				effort = prevEffort
			}
			if err := upsertProfileIntoMap(next, id, config.LLMProfileConfig{
				Provider:          item.Provider,
				BaseURL:           item.BaseURL,
				Model:             item.Model,
				APIKeyEnv:         item.APIKeyEnv,
				Mock:              item.Mock,
				Thinking:          thinking,
				ReasoningEffort:   effort,
				MultimodalEnabled: boolPtr(item.MultimodalEnabled),
			}); err != nil {
				return err
			}
			order = append(order, id)
		}
		cfg.LLM.Profiles = next
		cfg.LLM.ProfileOrder = order
		// 默认取第一条；若请求带了 active 且存在则用作运行时选用。
		active := strings.TrimSpace(p.Active)
		if active == "" || !cfgHasProfile(cfg, active) {
			active = order[0]
		}
		if err := cfg.SetActiveLLMProfile(active); err != nil {
			return err
		}
		cfg.ApplyDefaults()
		return nil
	} else if provider := strings.ToLower(strings.TrimSpace(p.Provider)); provider != "" {
		// 兼容旧客户端：只提交顶层字段时，更新当前 active 配置。
		mock := p.Mock || provider == "mock"
		if provider == "mock" {
			mock = true
		}
		model := strings.TrimSpace(p.Model)
		if !mock && model == "" {
			return fmt.Errorf("llm.model is required when mock is false")
		}
		switch provider {
		case "openai", "deepseek", "qwen", "vllm", "mock":
		default:
			return fmt.Errorf("unsupported llm.provider %q", p.Provider)
		}
		cfg.LLM.Provider = provider
		cfg.LLM.BaseURL = strings.TrimSpace(p.BaseURL)
		cfg.LLM.Model = model
		if env := strings.TrimSpace(p.APIKeyEnv); env != "" {
			cfg.LLM.APIKeyEnv = env
		}
		cfg.LLM.Mock = mock
		cfg.SyncActiveProfileFromFlat()
	}

	active := strings.TrimSpace(p.Active)
	if active != "" {
		if err := cfg.SetActiveLLMProfile(active); err != nil {
			return err
		}
	} else if cfg.LLM.ActiveProfileID() != "" {
		cfg.ApplyActiveToFlat()
	} else if first := cfg.LLM.FirstProfileID(); first != "" {
		_ = cfg.SetActiveLLMProfile(first)
	}
	cfg.ApplyDefaults()
	return nil
}

func cfgHasProfile(cfg *config.Config, id string) bool {
	if cfg == nil {
		return false
	}
	_, ok := cfg.LLM.GetProfile(id)
	return ok
}

func upsertProfileIntoMap(dst map[string]config.LLMProfileConfig, id string, prof config.LLMProfileConfig) error {
	tmp := &config.Config{LLM: config.LLMConfig{Profiles: map[string]config.LLMProfileConfig{}}}
	if err := tmp.UpsertProfile(id, prof, true); err != nil {
		return err
	}
	p, _ := tmp.LLM.GetProfile(id)
	dst[id] = p
	return nil
}

func llmProfilesFromConfig(cfg *config.Config) []LLMProfileSettings {
	if cfg == nil {
		return nil
	}
	ids := cfg.LLM.ProfileIDs()
	out := make([]LLMProfileSettings, 0, len(ids))
	for _, id := range ids {
		p, ok := cfg.LLM.GetProfile(id)
		if !ok {
			continue
		}
		out = append(out, LLMProfileSettings{
			ID:                id,
			Provider:          p.Provider,
			BaseURL:           p.BaseURL,
			Model:             p.Model,
			APIKeyEnv:         p.APIKeyEnv,
			Mock:              p.Mock,
			Thinking:          p.Thinking,
			ReasoningEffort:   p.ReasoningEffort,
			MultimodalEnabled: config.ProfileMultimodalEnabled(p),
		})
	}
	return out
}

func applyManagePatch(cfg *config.Config, p ManageSettings) error {
	cfg.Manage.Enabled = p.Enabled
	cfg.Manage.NodeToken = strings.TrimSpace(p.NodeToken)
	cfg.Manage.Workgroup.Enabled = boolPtr(p.WorkgroupEnabled)
	if p.RegistrationIntervalSeconds > 0 {
		cfg.Manage.Registration.IntervalSeconds = p.RegistrationIntervalSeconds
	}
	if p.RegistrationTTLSeconds > 0 {
		cfg.Manage.Registration.TTLSeconds = p.RegistrationTTLSeconds
	}
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
	return nil
}

func applyFeaturesPatch(cfg *config.Config, p FeatureSettings) {
	cfg.Skills.Enabled = p.SkillsEnabled
	cfg.Triggers.Enabled = p.TriggersEnabled
	cfg.ChildAgents.Enabled = p.ChildAgentsEnabled
	cfg.UI.Enabled = boolPtr(p.UIEnabled)
	cfg.Multimodal.Enabled = boolPtr(p.MultimodalEnabled)
	cfg.Browser.Enabled = boolPtr(p.BrowserEnabled)
	cfg.WeCom.Enabled = boolPtr(p.WeComEnabled)
	if p.SkillsMaxInPrompt > 0 {
		cfg.Skills.MaxInPrompt = p.SkillsMaxInPrompt
	}
	if p.TriggersPollSeconds > 0 {
		cfg.Triggers.PollSeconds = p.TriggersPollSeconds
	}
	cfg.RawMessageHistory.Enabled = boolPtr(p.RawMessageHistoryEnabled)
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

func applyRuntimePatch(cfg *config.Config, p RuntimeSettings) error {
	if id := strings.TrimSpace(p.NodeID); id != "" {
		cfg.NodeID = id
	}
	// fs_root 写死不可配置，忽略 PATCH 中的值。
	if level := strings.ToLower(strings.TrimSpace(p.LogLevel)); level != "" {
		switch level {
		case "debug", "info", "warn", "error":
			cfg.Log.Level = level
		default:
			return fmt.Errorf("log.level must be debug|info|warn|error")
		}
	}
	return nil
}

func applyAgentPatch(cfg *config.Config, p AgentSettings) {
	cfg.Agent.Name = strings.TrimSpace(p.Name)
	cfg.Agent.Description = strings.TrimSpace(p.Description)
	// Role 仅作可选元数据；空 PATCH 字段不强制清空已有值以外——与 name/desc 同策略整段覆盖
	cfg.Agent.Role = strings.TrimSpace(p.Role)
}

func applyUserPatch(cfg *config.Config, p UserSettings) error {
	cfg.User.PreferredName = strings.TrimSpace(p.PreferredName)
	return nil
}

func applyOnboardingPatch(cfg *config.Config, p OnboardingSettings) error {
	if p.NodeProfileCompleted {
		if strings.TrimSpace(cfg.Agent.Name) == "" {
			return fmt.Errorf("agent.name is required to complete node profile")
		}
		if strings.TrimSpace(cfg.User.PreferredName) == "" {
			return fmt.Errorf("user.preferred_name is required to complete node profile")
		}
	}
	cfg.Onboarding.NodeProfileCompleted = boolPtr(p.NodeProfileCompleted)
	return nil
}

func applyChildAgentsPatch(cfg *config.Config, p ChildAgentsLimits) error {
	if p.DefaultTTLSeconds < 0 || p.MaxTTLSeconds < 0 || p.DefaultMaxTurns < 0 ||
		p.MaxMaxTurns < 0 || p.MaxActivePerParent < 0 || p.DefaultWaitTimeoutSeconds < 0 {
		return fmt.Errorf("child_agents limits must be >= 0")
	}
	if p.DefaultTTLSeconds > 0 {
		cfg.ChildAgents.DefaultTTLSeconds = p.DefaultTTLSeconds
	}
	if p.MaxTTLSeconds > 0 {
		cfg.ChildAgents.MaxTTLSeconds = p.MaxTTLSeconds
	}
	if p.DefaultMaxTurns > 0 {
		cfg.ChildAgents.DefaultMaxTurns = p.DefaultMaxTurns
	}
	if p.MaxMaxTurns > 0 {
		cfg.ChildAgents.MaxMaxTurns = p.MaxMaxTurns
	}
	if p.MaxActivePerParent > 0 {
		cfg.ChildAgents.MaxActivePerParent = p.MaxActivePerParent
	}
	if p.DefaultWaitTimeoutSeconds > 0 {
		cfg.ChildAgents.DefaultWaitTimeoutSeconds = p.DefaultWaitTimeoutSeconds
	}
	return nil
}

func applyBrowserPatch(cfg *config.Config, p BrowserSettings) error {
	cfg.Browser.ServiceURL = strings.TrimSpace(p.ServiceURL)
	cfg.Browser.Headed = boolPtr(p.Headed)
	if p.DefaultTimeoutMS > 0 {
		cfg.Browser.DefaultTimeoutMS = p.DefaultTimeoutMS
	}
	if dir := strings.TrimSpace(p.OutputDir); dir != "" {
		cfg.Browser.OutputDir = dir
	}
	if p.MaxSessions > 0 {
		cfg.Browser.MaxSessions = p.MaxSessions
	}
	cfg.Browser.IgnoreHTTPSErrors = boolPtr(p.IgnoreHTTPSErrors)
	cfg.Browser.ChromePath = strings.TrimSpace(p.ChromePath)
	cfg.Browser.CDPURL = strings.TrimSpace(p.CDPURL)
	return nil
}

func weComSettingsFromConfig(cfg *config.Config) WeComSettings {
	if cfg == nil {
		return WeComSettings{}
	}
	key := cfg.WeComWebhookKey()
	url := strings.TrimSpace(cfg.WeCom.WebhookURL)
	// GET 不回明文 key；若仅有 key，用脱敏 URL 提示已配置。
	if key != "" && url == "" {
		url = config.DefaultWeComAPIBase + "/cgi-bin/webhook/send?key=***"
	} else if key != "" && strings.Contains(url, "key=") {
		url = redactWeComWebhookURL(url)
	}
	return WeComSettings{
		WebhookURL:    url,
		HasWebhookKey: key != "",
		APIBase:       cfg.WeComAPIBase(),
	}
}

func redactWeComWebhookURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	if q.Get("key") == "" {
		return raw
	}
	q.Set("key", "***")
	u.RawQuery = q.Encode()
	return u.String()
}

func applyWeComPatch(cfg *config.Config, p WeComSettings) error {
	if p.ClearWebhookKey {
		cfg.WeCom.WebhookKey = ""
		cfg.WeCom.WebhookURL = ""
	}
	if base := strings.TrimSpace(p.APIBase); base != "" {
		cfg.WeCom.APIBase = base
	}
	if key := strings.TrimSpace(p.WebhookKey); key != "" {
		cfg.WeCom.WebhookKey = config.ExtractWeComWebhookKey(key)
		if cfg.WeCom.WebhookKey == "" {
			cfg.WeCom.WebhookKey = key
		}
	}
	if rawURL := strings.TrimSpace(p.WebhookURL); rawURL != "" && !strings.Contains(rawURL, "key=***") {
		if extracted := config.ExtractWeComWebhookKey(rawURL); extracted != "" {
			cfg.WeCom.WebhookKey = extracted
			cfg.WeCom.WebhookURL = rawURL
		} else if !strings.Contains(rawURL, "://") {
			// 用户把裸 key 填进 webhook_url 字段。
			cfg.WeCom.WebhookKey = rawURL
			cfg.WeCom.WebhookURL = ""
		} else {
			cfg.WeCom.WebhookURL = rawURL
		}
	}
	return nil
}

func applyToolsPatch(cfg *config.Config, p ToolsSettings) error {
	if p.EnabledGroups != nil {
		cfg.Tools.EnabledGroups = append([]string(nil), p.EnabledGroups...)
	}
	cfg.Tools.BashOutputEncoding = strings.TrimSpace(p.BashOutputEncoding)
	cfg.Tools.FileEncoding = strings.TrimSpace(p.FileEncoding)
	cfg.Tools.BashCompress.Enabled = boolPtr(p.BashCompressEnabled)
	if p.BashCompressMaxOutputChars > 0 {
		cfg.Tools.BashCompress.MaxOutputChars = p.BashCompressMaxOutputChars
	}
	if p.BashCompressMaxStderrChars > 0 {
		cfg.Tools.BashCompress.MaxOutputCharsStderr = p.BashCompressMaxStderrChars
	}
	return nil
}

func applyHooksPatch(cfg *config.Config, p HooksSettings) error {
	cfg.Hooks.DuplicateToolCall.Enabled = boolPtr(p.DuplicateToolCallEnabled)
	if p.DuplicateToolCallWindowSeconds > 0 {
		cfg.Hooks.DuplicateToolCall.WindowSeconds = p.DuplicateToolCallWindowSeconds
	}
	cfg.Hooks.ToolResult.Enabled = boolPtr(p.ToolResultEnabled)
	if p.ToolResultSpillThresholdTokens > 0 {
		cfg.Hooks.ToolResult.SpillThresholdTokens = p.ToolResultSpillThresholdTokens
	}
	cfg.Hooks.InjectTodayDate.Enabled = boolPtr(p.InjectTodayDateEnabled)
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
