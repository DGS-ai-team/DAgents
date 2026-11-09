package setup

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

// LLMProfileSettings 单个 LLM 配置（Web UI / setup API）。
type LLMProfileSettings struct {
	ID string `json:"id"`
	// OriginalID is write-only PATCH metadata used when renaming a profile so
	// its stored secret can move with it. It is never populated in GET views.
	OriginalID           string `json:"original_id,omitempty"`
	Provider             string `json:"provider"`
	BaseURL              string `json:"base_url"`
	Model                string `json:"model"`
	APIKeyEnv            string `json:"api_key_env,omitempty"`
	APIKey               string `json:"api_key,omitempty"` // 仅 PATCH 写入；GET 不回传明文
	HasAPIKey            bool   `json:"has_api_key"`
	ClearAPIKey          bool   `json:"clear_api_key,omitempty"`
	Mock                 bool   `json:"mock"`
	Thinking             string `json:"thinking,omitempty"`
	ClearThinking        bool   `json:"clear_thinking,omitempty"`
	ReasoningEffort      string `json:"reasoning_effort,omitempty"`
	ClearReasoningEffort bool   `json:"clear_reasoning_effort,omitempty"`
	ThinkingBudget       *int   `json:"thinking_budget,omitempty"`
	ClearThinkingBudget  bool   `json:"clear_thinking_budget,omitempty"`
	MultimodalEnabled    bool   `json:"multimodal_enabled"`
}

// LLMSettings LLM 连接配置（支持多配置；列表顺序中第一条为默认）。
type LLMSettings struct {
	Active   string               `json:"active,omitempty"` // 运行时当前选用；缺省取 profiles[0]
	Profiles []LLMProfileSettings `json:"profiles"`
}

// ManageSettings Manage 连接配置（安装向导原批次 2）。
type ManageSettings struct {
	Enabled                     bool   `json:"enabled"`
	URL                         string `json:"url"`
	Team                        string `json:"team"`
	RegistrationBaseURL         string `json:"registration_base_url"`
	WorkgroupEnabled            bool   `json:"workgroup_enabled"`
	HasNodeToken                bool   `json:"has_node_token"`
	RegistrationIntervalSeconds int    `json:"registration_interval_seconds"`
	RegistrationTTLSeconds      int    `json:"registration_ttl_seconds"`
}

// ManageSettingsPatch merges only fields explicitly present in PATCH. The
// token is write-only: an empty value preserves the stored token and
// clear_node_token is the explicit deletion operation.
type ManageSettingsPatch struct {
	Enabled                     *bool   `json:"enabled,omitempty"`
	URL                         *string `json:"url,omitempty"`
	Team                        *string `json:"team,omitempty"`
	RegistrationBaseURL         *string `json:"registration_base_url,omitempty"`
	WorkgroupEnabled            *bool   `json:"workgroup_enabled,omitempty"`
	NodeToken                   *string `json:"node_token,omitempty"`
	ClearNodeToken              bool    `json:"clear_node_token,omitempty"`
	RegistrationIntervalSeconds *int    `json:"registration_interval_seconds,omitempty"`
	RegistrationTTLSeconds      *int    `json:"registration_ttl_seconds,omitempty"`
}

// FeatureSettings 功能开关（安装向导原批次 3）。
type FeatureSettings struct {
	SkillsEnabled            bool   `json:"skills_enabled"`
	TriggersEnabled          bool   `json:"triggers_enabled"`
	ChildAgentsEnabled       bool   `json:"child_agents_enabled"`
	BrowserEnabled           bool   `json:"browser_enabled"`
	WeComEnabled             bool   `json:"wecom_enabled"`
	MultimodalEnabled        bool   `json:"multimodal_enabled"`
	SkillsMaxInPrompt        int    `json:"skills_max_in_prompt"`
	TriggersPollSeconds      int    `json:"triggers_poll_seconds"`
	RawMessageHistoryEnabled bool   `json:"raw_message_history_enabled"`
	RawMessageHistorySource  string `json:"raw_message_history_source"`
}

// FeatureSettingsPatch merges the explicitly supplied process-level feature
// controls. Setup panels may omit controls they do not own without changing
// their current values.
type FeatureSettingsPatch struct {
	SkillsEnabled            *bool `json:"skills_enabled,omitempty"`
	TriggersEnabled          *bool `json:"triggers_enabled,omitempty"`
	ChildAgentsEnabled       *bool `json:"child_agents_enabled,omitempty"`
	BrowserEnabled           *bool `json:"browser_enabled,omitempty"`
	WeComEnabled             *bool `json:"wecom_enabled,omitempty"`
	MultimodalEnabled        *bool `json:"multimodal_enabled,omitempty"`
	SkillsMaxInPrompt        *int  `json:"skills_max_in_prompt,omitempty"`
	TriggersPollSeconds      *int  `json:"triggers_poll_seconds,omitempty"`
	RawMessageHistoryEnabled *bool `json:"raw_message_history_enabled,omitempty"`
}

// CompressionSettings 上下文压缩阈值（config compression 块）。
type CompressionSettings struct {
	SilentTriggerTokens        int `json:"silent_trigger_tokens"`
	BlockingTriggerTokens      int `json:"blocking_trigger_tokens"`
	IdleNewConversationSeconds int `json:"idle_new_conversation_seconds"`
}

// MemorySettings controls the optional compression-to-memory background
// pipeline. Automatic extraction is disabled by default because it costs an
// LLM call and inferred candidates are kept behind the consolidation boundary.
type MemorySettings struct {
	AutoExtract        bool `json:"auto_extract"`
	CandidateQueueSize int  `json:"candidate_queue_size"`
	MaxCandidates      int  `json:"max_candidates"`
	CoreBudgetTokens   int  `json:"core_budget_tokens"`
}

// NodeEndpointView 只读：Node 监听地址（须改 config.yaml 并重启）。
type NodeEndpointView struct {
	ListenHost string `json:"listen_host"`
	ListenPort int    `json:"listen_port"`
}

// RuntimeSettings 运行时路径与日志；listener 单独通过只读 NodeEndpointView 暴露。
type RuntimeSettings struct {
	NodeID      string `json:"node_id"`
	RuntimeRoot string `json:"runtime_root"`
	LogLevel    string `json:"log_level"`
}

// AgentSettings Node 展示身份（历史字段名 agent；UI 称 Node 名称）。
type AgentSettings struct {
	Name        string `json:"name"`
	Description string `json:"description"`
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
	DefaultTTLSeconds  int `json:"default_ttl_seconds"`
	MaxTTLSeconds      int `json:"max_ttl_seconds"`
	MaxActivePerParent int `json:"max_active_per_parent"`
}

// BrowserSettings Browser 工具参数（enabled 见 features）。
type BrowserSettings struct {
	Headed            bool   `json:"headed"`
	DefaultTimeoutMS  int    `json:"default_timeout_ms"`
	OutputDir         string `json:"output_dir"`
	MaxSessions       int    `json:"max_sessions"`
	IgnoreHTTPSErrors bool   `json:"ignore_https_errors"`
	ChromePath        string `json:"chrome_path"`
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
	ClearWebhookKey bool   `json:"clear_webhook_key,omitempty"`
	APIBase         string `json:"api_base"`
}

// ToolsSettings 内置工具编码与 bash 压缩。
type ToolsSettings struct {
	BashOutputEncoding                string `json:"bash_output_encoding"`
	FileEncoding                      string `json:"file_encoding"`
	BashCompressEnabled               bool   `json:"bash_compress_enabled"`
	BashCompressMaxOutputChars        int    `json:"bash_compress_max_output_chars"`
	BashCompressMaxStderrChars        int    `json:"bash_compress_max_stderr_chars"`
	BashCompressDefaultMaxOutputChars int    `json:"bash_compress_default_max_output_chars"`
	BashCompressDefaultMaxStderrChars int    `json:"bash_compress_default_max_stderr_chars"`
	BashCompressOutputMode            string `json:"bash_compress_output_mode"`
	BashCompressTailChars             int    `json:"bash_compress_tail_chars"`
	BashCompressTailStderrChars       int    `json:"bash_compress_tail_stderr_chars"`
}

// ToolsSettingsPatch preserves omitted settings and rejects invalid explicit limits.
type ToolsSettingsPatch struct {
	BashOutputEncoding          *string `json:"bash_output_encoding,omitempty"`
	FileEncoding                *string `json:"file_encoding,omitempty"`
	BashCompressEnabled         *bool   `json:"bash_compress_enabled,omitempty"`
	BashCompressMaxOutputChars  *int    `json:"bash_compress_max_output_chars,omitempty"`
	BashCompressMaxStderrChars  *int    `json:"bash_compress_max_stderr_chars,omitempty"`
	BashCompressOutputMode      *string `json:"bash_compress_output_mode,omitempty"`
	BashCompressTailChars       *int    `json:"bash_compress_tail_chars,omitempty"`
	BashCompressTailStderrChars *int    `json:"bash_compress_tail_stderr_chars,omitempty"`
	ResetBashCompressLimits     bool    `json:"reset_bash_compress_limits,omitempty"`
}

// HooksSettings 常用 Hook 开关（不含 plugin 列表）。
type HooksSettings struct {
	DuplicateToolCallEnabled       bool `json:"duplicate_tool_call_enabled"`
	DuplicateToolCallWindowSeconds int  `json:"duplicate_tool_call_window_seconds"`
	ToolResultEnabled              bool `json:"tool_result_enabled"`
	ToolResultSpillThresholdTokens int  `json:"tool_result_spill_threshold_tokens"`
	InjectTodayDateEnabled         bool `json:"inject_today_date_enabled"`
}

// SettingsView GET /v1/settings response.
type SettingsView struct {
	ConfigPath      string              `json:"config_path,omitempty"`
	ConfigWritable  bool                `json:"config_writable"`
	RestartRequired bool                `json:"restart_required"`
	Node            NodeEndpointView    `json:"node"`
	LLM             LLMSettings         `json:"llm"`
	Manage          ManageSettings      `json:"manage"`
	Features        FeatureSettings     `json:"features"`
	Compression     CompressionSettings `json:"compression"`
	Memory          MemorySettings      `json:"memory"`
	Runtime         RuntimeSettings     `json:"runtime"`
	Agent           AgentSettings       `json:"agent"`
	User            UserSettings        `json:"user"`
	Onboarding      OnboardingSettings  `json:"onboarding"`
	ChildAgents     ChildAgentsLimits   `json:"child_agents"`
	Browser         BrowserSettings     `json:"browser"`
	WeCom           WeComSettings       `json:"wecom"`
	Tools           ToolsSettings       `json:"tools"`
	Hooks           HooksSettings       `json:"hooks"`
	// AvailableToolGroups 本 Node 当前可供 Agent 勾选的工具组（已按 browser/wecom 进程开关过滤）。
	AvailableToolGroups []string `json:"available_tool_groups"`
}

// SettingsPatch PATCH /v1/settings 请求体（字段均可选）。
type SettingsPatch struct {
	LLM         *LLMSettings          `json:"llm,omitempty"`
	Manage      *ManageSettingsPatch  `json:"manage,omitempty"`
	Features    *FeatureSettingsPatch `json:"features,omitempty"`
	Compression *CompressionSettings  `json:"compression,omitempty"`
	Memory      *MemorySettings       `json:"memory,omitempty"`
	Runtime     *RuntimeSettings      `json:"runtime,omitempty"`
	Agent       *AgentSettings        `json:"agent,omitempty"`
	User        *UserSettings         `json:"user,omitempty"`
	Onboarding  *OnboardingSettings   `json:"onboarding,omitempty"`
	ChildAgents *ChildAgentsLimits    `json:"child_agents,omitempty"`
	Browser     *BrowserSettings      `json:"browser,omitempty"`
	WeCom       *WeComSettings        `json:"wecom,omitempty"`
	Tools       *ToolsSettingsPatch   `json:"tools,omitempty"`
	Hooks       *HooksSettings        `json:"hooks,omitempty"`
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
		Node: NodeEndpointView{ListenHost: cfg.Listen.Host, ListenPort: cfg.Listen.Port},
		LLM: LLMSettings{
			Active:   cfg.LLM.ActiveProfileID(),
			Profiles: llmProfilesFromConfig(cfg),
		},
		Manage: ManageSettings{
			Enabled:                     cfg.Manage.Enabled,
			URL:                         cfg.Manage.URL,
			Team:                        cfg.Manage.Registration.Team,
			RegistrationBaseURL:         cfg.Manage.Registration.BaseURL,
			WorkgroupEnabled:            workgroupEnabled,
			HasNodeToken:                strings.TrimSpace(cfg.Manage.NodeToken) != "",
			RegistrationIntervalSeconds: cfg.Manage.Registration.IntervalSeconds,
			RegistrationTTLSeconds:      cfg.Manage.Registration.TTLSeconds,
		},
		Features: FeatureSettings{
			SkillsEnabled:            cfg.Skills.Enabled,
			TriggersEnabled:          cfg.Triggers.Enabled,
			ChildAgentsEnabled:       cfg.ChildAgents.Enabled,
			BrowserEnabled:           cfg.BrowserEnabled(),
			WeComEnabled:             cfg.WeComEnabled(),
			MultimodalEnabled:        cfg.MultimodalEnabled(),
			SkillsMaxInPrompt:        cfg.Skills.MaxInPrompt,
			TriggersPollSeconds:      cfg.Triggers.PollSeconds,
			RawMessageHistoryEnabled: cfg.RawMessageHistoryEnabled(),
			RawMessageHistorySource:  cfg.RawMessageHistorySource(),
		},
		Compression: CompressionSettings{
			SilentTriggerTokens:        cfg.Compression.SilentTriggerTokens,
			BlockingTriggerTokens:      cfg.Compression.BlockingTriggerTokens,
			IdleNewConversationSeconds: cfg.Compression.IdleNewConversationSeconds,
		},
		Memory: MemorySettings{
			AutoExtract:        cfg.Memory.AutoExtract,
			CandidateQueueSize: cfg.Memory.CandidateQueueSize,
			MaxCandidates:      cfg.Memory.MaxCandidates,
			CoreBudgetTokens:   cfg.Memory.CoreBudgetTokens,
		},
		Runtime: RuntimeSettings{
			NodeID:      cfg.NodeID,
			RuntimeRoot: cfg.RuntimeDir(),
			LogLevel:    cfg.Log.Level,
		},
		Agent: AgentSettings{
			Name:        cfg.Agent.Name,
			Description: cfg.Agent.Description,
		},
		User: UserSettings{
			PreferredName: cfg.PreferredName(),
		},
		Onboarding: OnboardingSettings{
			NodeProfileCompleted: cfg.NodeProfileCompleted(),
		},
		ChildAgents: ChildAgentsLimits{
			DefaultTTLSeconds:  cfg.ChildAgents.DefaultTTLSeconds,
			MaxTTLSeconds:      cfg.ChildAgents.MaxTTLSeconds,
			MaxActivePerParent: cfg.ChildAgents.MaxActivePerParent,
		},
		Browser: BrowserSettings{
			Headed:            cfg.BrowserHeaded(),
			DefaultTimeoutMS:  cfg.Browser.DefaultTimeoutMS,
			OutputDir:         cfg.Browser.OutputDir,
			MaxSessions:       cfg.Browser.MaxSessions,
			IgnoreHTTPSErrors: cfg.Browser.BrowserIgnoreHTTPSErrors(),
			ChromePath:        cfg.Browser.ChromePath,
		},
		WeCom: weComSettingsFromConfig(cfg),
		Tools: ToolsSettings{
			BashOutputEncoding:                cfg.Tools.BashOutputEncoding,
			FileEncoding:                      cfg.Tools.FileEncoding,
			BashCompressEnabled:               cfg.Tools.BashCompress.Enabled == nil || *cfg.Tools.BashCompress.Enabled,
			BashCompressMaxOutputChars:        cfg.Tools.BashCompress.MaxOutputChars,
			BashCompressMaxStderrChars:        cfg.Tools.BashCompress.MaxOutputCharsStderr,
			BashCompressDefaultMaxOutputChars: config.DefaultBashCompressMaxOutputChars,
			BashCompressDefaultMaxStderrChars: config.DefaultBashCompressMaxOutputCharsStderr,
			BashCompressOutputMode:            cfg.Tools.BashCompress.OutputMode,
			BashCompressTailChars:             cfg.Tools.BashCompress.TailOutputChars,
			BashCompressTailStderrChars:       cfg.Tools.BashCompress.TailOutputCharsStderr,
		},
		Hooks: HooksSettings{
			DuplicateToolCallEnabled:       cfg.DuplicateToolCallHookEnabled(),
			DuplicateToolCallWindowSeconds: cfg.DuplicateToolCallWindowSeconds(),
			ToolResultEnabled:              cfg.ToolResultHookEnabled(),
			ToolResultSpillThresholdTokens: cfg.ToolResultSpillThresholdTokens(),
			InjectTodayDateEnabled:         cfg.InjectTodayDateHookEnabled(),
		},
		AvailableToolGroups: cfg.AvailableAgentToolGroups(),
	}
}

// ApplyPatch 将 PATCH 合并进 cfg 副本并校验。
func ApplyPatch(cfg *config.Config, patch SettingsPatch) (*config.Config, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	out := *cfg
	// LLM profiles are the only map nested in Config. Feature patches may
	// synchronize multimodal settings into the active profile, and defaults
	// normalization rewrites profile values, so isolate this map before applying
	// any patch. A shallow Config copy would otherwise mutate the live config
	// even when a later validation or persistence step fails.
	if cfg.LLM.Profiles != nil {
		out.LLM.Profiles = make(map[string]config.LLMProfileConfig, len(cfg.LLM.Profiles))
		for id, profile := range cfg.LLM.Profiles {
			out.LLM.Profiles[id] = profile
		}
	}
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
		if err := applyFeaturesPatch(&out, *patch.Features); err != nil {
			return nil, err
		}
	}
	if patch.Compression != nil {
		if err := applyCompressionPatch(&out, *patch.Compression); err != nil {
			return nil, err
		}
	}
	if patch.Memory != nil {
		if err := applyMemoryPatch(&out, *patch.Memory); err != nil {
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
			var prevBudget *int
			if prev, ok := cfg.LLM.GetProfile(id); ok {
				prevThinking = prev.Thinking
				prevEffort = prev.ReasoningEffort
				prevBudget = prev.ThinkingBudget
			}
			modelChanged := false
			if prev, ok := cfg.LLM.GetProfile(id); ok {
				modelChanged = !strings.EqualFold(strings.TrimSpace(prev.Provider), strings.TrimSpace(item.Provider)) || strings.TrimSpace(prev.Model) != strings.TrimSpace(item.Model)
			}
			thinking := strings.TrimSpace(item.Thinking)
			if thinking == "" && !item.ClearThinking && !modelChanged {
				thinking = prevThinking
			}
			effort := strings.TrimSpace(item.ReasoningEffort)
			if effort == "" && !item.ClearReasoningEffort && !modelChanged {
				effort = prevEffort
			}
			budget := item.ThinkingBudget
			if budget == nil && !item.ClearThinkingBudget && !modelChanged {
				budget = prevBudget
			}
			if err := upsertProfileIntoMap(next, id, config.LLMProfileConfig{
				Provider:          item.Provider,
				BaseURL:           item.BaseURL,
				Model:             item.Model,
				APIKeyEnv:         item.APIKeyEnv,
				Mock:              item.Mock,
				Thinking:          thinking,
				ReasoningEffort:   effort,
				ThinkingBudget:    budget,
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
			ThinkingBudget:    p.ThinkingBudget,
			MultimodalEnabled: config.ProfileMultimodalEnabled(p),
		})
	}
	return out
}

func applyManagePatch(cfg *config.Config, p ManageSettingsPatch) error {
	if p.Enabled != nil {
		cfg.Manage.Enabled = *p.Enabled
	}
	if p.ClearNodeToken {
		cfg.Manage.NodeToken = ""
	} else if p.NodeToken != nil {
		if token := strings.TrimSpace(*p.NodeToken); token != "" {
			cfg.Manage.NodeToken = token
		}
	}
	if p.WorkgroupEnabled != nil {
		cfg.Manage.Workgroup.Enabled = boolPtr(*p.WorkgroupEnabled)
	}
	if p.RegistrationIntervalSeconds != nil {
		if *p.RegistrationIntervalSeconds <= 0 {
			return fmt.Errorf("manage.registration_interval_seconds must be > 0")
		}
		cfg.Manage.Registration.IntervalSeconds = *p.RegistrationIntervalSeconds
	}
	if p.RegistrationTTLSeconds != nil {
		if *p.RegistrationTTLSeconds <= 0 {
			return fmt.Errorf("manage.registration_ttl_seconds must be > 0")
		}
		cfg.Manage.Registration.TTLSeconds = *p.RegistrationTTLSeconds
	}
	if p.URL != nil {
		cfg.Manage.URL = strings.TrimSpace(*p.URL)
	}
	if p.Team != nil {
		cfg.Manage.Registration.Team = strings.TrimSpace(*p.Team)
	}
	if p.RegistrationBaseURL != nil {
		cfg.Manage.Registration.BaseURL = strings.TrimSpace(*p.RegistrationBaseURL)
	}
	if cfg.Manage.Enabled && strings.TrimSpace(cfg.Manage.URL) == "" {
		return fmt.Errorf("manage.url is required when manage is enabled")
	}
	return nil
}

func applyFeaturesPatch(cfg *config.Config, p FeatureSettingsPatch) error {
	if p.SkillsEnabled != nil {
		cfg.Skills.Enabled = *p.SkillsEnabled
	}
	if p.TriggersEnabled != nil {
		cfg.Triggers.Enabled = *p.TriggersEnabled
	}
	if p.ChildAgentsEnabled != nil {
		cfg.ChildAgents.Enabled = *p.ChildAgentsEnabled
	}
	if p.BrowserEnabled != nil {
		cfg.Browser.Enabled = boolPtr(*p.BrowserEnabled)
	}
	if p.WeComEnabled != nil {
		cfg.WeCom.Enabled = boolPtr(*p.WeComEnabled)
	}
	if p.MultimodalEnabled != nil {
		cfg.Multimodal.Enabled = boolPtr(*p.MultimodalEnabled)
		cfg.SyncActiveProfileMultimodalFromFlat()
	}
	if p.SkillsMaxInPrompt != nil {
		if *p.SkillsMaxInPrompt <= 0 {
			return fmt.Errorf("features.skills_max_in_prompt must be > 0")
		}
		cfg.Skills.MaxInPrompt = *p.SkillsMaxInPrompt
	}
	if p.TriggersPollSeconds != nil {
		if *p.TriggersPollSeconds <= 0 {
			return fmt.Errorf("features.triggers_poll_seconds must be > 0")
		}
		cfg.Triggers.PollSeconds = *p.TriggersPollSeconds
	}
	if p.RawMessageHistoryEnabled != nil {
		cfg.RawMessageHistory.Enabled = boolPtr(*p.RawMessageHistoryEnabled)
	}
	return nil
}

func applyCompressionPatch(cfg *config.Config, p CompressionSettings) error {
	if p.SilentTriggerTokens < 0 {
		return fmt.Errorf("compression.silent_trigger_tokens must be >= 0")
	}
	if p.BlockingTriggerTokens < 0 {
		return fmt.Errorf("compression.blocking_trigger_tokens must be >= 0")
	}
	if p.IdleNewConversationSeconds < 0 {
		return fmt.Errorf("compression.idle_new_conversation_seconds must be >= 0")
	}
	silent := p.SilentTriggerTokens
	blocking := p.BlockingTriggerTokens
	if silent > 0 && blocking > 0 && blocking < silent {
		return fmt.Errorf("compression.blocking_trigger_tokens must be >= silent_trigger_tokens when both are enabled")
	}
	cfg.Compression.SilentTriggerTokens = silent
	cfg.Compression.BlockingTriggerTokens = blocking
	cfg.Compression.IdleNewConversationSeconds = p.IdleNewConversationSeconds
	return nil
}

func applyMemoryPatch(cfg *config.Config, p MemorySettings) error {
	if p.CandidateQueueSize < 0 {
		return fmt.Errorf("memory.candidate_queue_size must be >= 0")
	}
	if p.MaxCandidates < 0 {
		return fmt.Errorf("memory.max_candidates must be >= 0")
	}
	if p.CoreBudgetTokens < 0 {
		return fmt.Errorf("memory.core_budget_tokens must be >= 0")
	}
	cfg.Memory.AutoExtract = p.AutoExtract
	if p.CandidateQueueSize > 0 {
		cfg.Memory.CandidateQueueSize = p.CandidateQueueSize
	}
	if p.MaxCandidates > 0 {
		cfg.Memory.MaxCandidates = p.MaxCandidates
	}
	if p.CoreBudgetTokens > 0 {
		cfg.Memory.CoreBudgetTokens = p.CoreBudgetTokens
	}
	return nil
}

func applyRuntimePatch(cfg *config.Config, p RuntimeSettings) error {
	if id := strings.TrimSpace(p.NodeID); id != "" {
		cfg.NodeID = id
	}
	// runtime_root 写死不可配置，忽略 PATCH 中的值。
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
	}
	cfg.Onboarding.NodeProfileCompleted = p.NodeProfileCompleted
	return nil
}

func applyChildAgentsPatch(cfg *config.Config, p ChildAgentsLimits) error {
	if p.DefaultTTLSeconds < 0 || p.MaxTTLSeconds < 0 || p.MaxActivePerParent < 0 {
		return fmt.Errorf("child_agents limits must be >= 0")
	}
	if p.DefaultTTLSeconds > 0 {
		cfg.ChildAgents.DefaultTTLSeconds = p.DefaultTTLSeconds
	}
	if p.MaxTTLSeconds > 0 {
		cfg.ChildAgents.MaxTTLSeconds = p.MaxTTLSeconds
	}
	if p.MaxActivePerParent > 0 {
		cfg.ChildAgents.MaxActivePerParent = p.MaxActivePerParent
	}
	return nil
}

func applyBrowserPatch(cfg *config.Config, p BrowserSettings) error {
	// Browser runs as a Node-owned local sidecar. The service endpoint and
	// attach-to-external-Chrome mode are fixed implementation details, not
	// settings exposed to Desktop/Web UI.
	cfg.Browser.ServiceURL = config.DefaultBrowserServiceURL
	cfg.Browser.CDPURL = ""
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

func applyToolsPatch(cfg *config.Config, p ToolsSettingsPatch) error {
	bash := &cfg.Tools.BashCompress
	if p.ResetBashCompressLimits {
		if p.BashCompressMaxOutputChars != nil || p.BashCompressMaxStderrChars != nil {
			return fmt.Errorf("tools.reset_bash_compress_limits cannot be combined with explicit limits")
		}
		bash.MaxOutputChars = config.DefaultBashCompressMaxOutputChars
		bash.MaxOutputCharsStderr = config.DefaultBashCompressMaxOutputCharsStderr
	}
	if p.BashCompressMaxOutputChars != nil {
		if *p.BashCompressMaxOutputChars <= 0 {
			return fmt.Errorf("tools.bash_compress_max_output_chars must be > 0")
		}
		bash.MaxOutputChars = *p.BashCompressMaxOutputChars
	}
	if p.BashCompressMaxStderrChars != nil {
		if *p.BashCompressMaxStderrChars <= 0 {
			return fmt.Errorf("tools.bash_compress_max_stderr_chars must be > 0")
		}
		bash.MaxOutputCharsStderr = *p.BashCompressMaxStderrChars
	}
	if p.BashOutputEncoding != nil {
		cfg.Tools.BashOutputEncoding = strings.TrimSpace(*p.BashOutputEncoding)
	}
	if p.FileEncoding != nil {
		cfg.Tools.FileEncoding = strings.TrimSpace(*p.FileEncoding)
	}
	if p.BashCompressEnabled != nil {
		bash.Enabled = boolPtr(*p.BashCompressEnabled)
	}
	if p.BashCompressOutputMode != nil {
		bash.OutputMode = strings.TrimSpace(*p.BashCompressOutputMode)
	}
	if p.BashCompressTailChars != nil {
		if *p.BashCompressTailChars <= 0 {
			return fmt.Errorf("tools.bash_compress_tail_chars must be > 0")
		}
		bash.TailOutputChars = *p.BashCompressTailChars
	}
	if p.BashCompressTailStderrChars != nil {
		if *p.BashCompressTailStderrChars <= 0 {
			return fmt.Errorf("tools.bash_compress_tail_stderr_chars must be > 0")
		}
		bash.TailOutputCharsStderr = *p.BashCompressTailStderrChars
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
