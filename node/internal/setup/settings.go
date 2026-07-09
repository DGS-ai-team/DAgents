package setup

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// LLMSettings LLM 连接配置（安装向导原批次 1）。
type LLMSettings struct {
	Provider     string `json:"provider"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	APIKeyEnv    string `json:"api_key_env"`
	Mock         bool   `json:"mock"`
	MaxToolLoops int    `json:"max_tool_loops"`
}

// ManageSettings Manage 连接配置（安装向导原批次 2）。
type ManageSettings struct {
	Enabled                     bool   `json:"enabled"`
	URL                         string `json:"url"`
	Team                        string `json:"team"`
	RegistrationBaseURL         string `json:"registration_base_url"`
	A2AEnabled                  bool   `json:"a2a_enabled"`
	NodeToken                   string `json:"node_token"`
	RegistrationIntervalSeconds int    `json:"registration_interval_seconds"`
	RegistrationTTLSeconds      int    `json:"registration_ttl_seconds"`
	A2AInboxWaitSeconds         int    `json:"a2a_inbox_wait_seconds"`
	A2AInboxPollSeconds         int    `json:"a2a_inbox_poll_seconds"`
}

// FeatureSettings 功能开关（安装向导原批次 3）。
type FeatureSettings struct {
	SkillsEnabled            bool `json:"skills_enabled"`
	TriggersEnabled          bool `json:"triggers_enabled"`
	ChildAgentsEnabled       bool `json:"child_agents_enabled"`
	UIEnabled                bool `json:"ui_enabled"`
	BrowserEnabled           bool `json:"browser_enabled"`
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
	AgentID  string `json:"agent_id"`
	FSRoot   string `json:"fs_root"`
	LogLevel string `json:"log_level"`
}

// AgentSettings Agent 身份（config agent 块）。
type AgentSettings struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Role           string `json:"role"`
	CompliancePeer string `json:"compliance_peer"`
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
	DuplicateToolCallEnabled      bool `json:"duplicate_tool_call_enabled"`
	DuplicateToolCallWindowSeconds int  `json:"duplicate_tool_call_window_seconds"`
	ToolResultEnabled             bool `json:"tool_result_enabled"`
	ToolResultSpillThresholdTokens int  `json:"tool_result_spill_threshold_tokens"`
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
	ChildAgents     ChildAgentsLimits   `json:"child_agents"`
	Browser         BrowserSettings     `json:"browser"`
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
	ChildAgents *ChildAgentsLimits   `json:"child_agents,omitempty"`
	Browser     *BrowserSettings     `json:"browser,omitempty"`
	Tools       *ToolsSettings       `json:"tools,omitempty"`
	Hooks       *HooksSettings       `json:"hooks,omitempty"`
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
		Node: NodeEndpointView{
			ListenHost:    cfg.Listen.Host,
			ListenPort:    cfg.Listen.Port,
			LocalEndpoint: cfg.Local.Endpoint,
		},
		LLM: LLMSettings{
			Provider:     cfg.LLM.Provider,
			BaseURL:      cfg.LLM.BaseURL,
			Model:        cfg.LLM.Model,
			APIKeyEnv:    cfg.LLM.APIKeyEnv,
			Mock:         cfg.LLM.Mock,
			MaxToolLoops: cfg.LLM.MaxToolLoops,
		},
		Manage: ManageSettings{
			Enabled:                     cfg.Manage.Enabled,
			URL:                         cfg.Manage.URL,
			Team:                        cfg.Manage.Registration.Team,
			RegistrationBaseURL:         cfg.Manage.Registration.BaseURL,
			A2AEnabled:                  a2aEnabled,
			NodeToken:                   cfg.Manage.NodeToken,
			RegistrationIntervalSeconds: cfg.Manage.Registration.IntervalSeconds,
			RegistrationTTLSeconds:      cfg.Manage.Registration.TTLSeconds,
			A2AInboxWaitSeconds:         cfg.Manage.A2A.InboxWaitSeconds,
			A2AInboxPollSeconds:         cfg.Manage.A2A.InboxPollSeconds,
		},
		Features: FeatureSettings{
			SkillsEnabled:            cfg.Skills.Enabled,
			TriggersEnabled:          cfg.Triggers.Enabled,
			ChildAgentsEnabled:       cfg.ChildAgents.Enabled,
			UIEnabled:                cfg.UIEnabled(),
			BrowserEnabled:           cfg.BrowserEnabled(),
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
			AgentID:  cfg.AgentID,
			FSRoot:   cfg.FSRoot,
			LogLevel: cfg.Log.Level,
		},
		Agent: AgentSettings{
			Name:           cfg.Agent.Name,
			Description:    cfg.Agent.Description,
			Role:           cfg.Agent.Role,
			CompliancePeer: cfg.Agent.CompliancePeer,
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
	if patch.Runtime != nil {
		if err := applyRuntimePatch(&out, *patch.Runtime); err != nil {
			return nil, err
		}
	}
	if patch.Agent != nil {
		applyAgentPatch(&out, *patch.Agent)
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
	if p.MaxToolLoops > 0 {
		cfg.LLM.MaxToolLoops = p.MaxToolLoops
	}
	return nil
}

func applyManagePatch(cfg *config.Config, p ManageSettings) error {
	cfg.Manage.Enabled = p.Enabled
	cfg.Manage.NodeToken = strings.TrimSpace(p.NodeToken)
	if p.RegistrationIntervalSeconds > 0 {
		cfg.Manage.Registration.IntervalSeconds = p.RegistrationIntervalSeconds
	}
	if p.RegistrationTTLSeconds > 0 {
		cfg.Manage.Registration.TTLSeconds = p.RegistrationTTLSeconds
	}
	if p.A2AInboxWaitSeconds > 0 {
		cfg.Manage.A2A.InboxWaitSeconds = p.A2AInboxWaitSeconds
	}
	if p.A2AInboxPollSeconds > 0 {
		cfg.Manage.A2A.InboxPollSeconds = p.A2AInboxPollSeconds
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
	if id := strings.TrimSpace(p.AgentID); id != "" {
		cfg.AgentID = id
	}
	if root := strings.TrimSpace(p.FSRoot); root != "" {
		cfg.FSRoot = root
	}
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
	cfg.Agent.Role = strings.TrimSpace(p.Role)
	cfg.Agent.CompliancePeer = strings.TrimSpace(p.CompliancePeer)
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
