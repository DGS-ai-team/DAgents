// Package config 提供 Agent Node 与 Client 共用的 YAML 配置模型与加载逻辑。
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	DefaultListenHost = "127.0.0.1"
	DefaultListenPort = 18765
)

// Config 为 Node 与 Client 共享的配置根结构；Client 仅使用 local 等子集。
type Config struct {
	AgentID       string       `yaml:"agent_id"`
	Listen        ListenConfig `yaml:"listen"`
	Local         LocalConfig  `yaml:"local"`
	ExposeToPeers bool         `yaml:"expose_to_peers"`
	Groups        []string     `yaml:"groups"`
	FSRoot        string       `yaml:"fs_root"`
	LLM           LLMConfig    `yaml:"llm"`
	Manage        ManageConfig `yaml:"manage"`
	Skills        SkillsConfig       `yaml:"skills"`
	Compression   CompressionConfig  `yaml:"compression"`
	Triggers          TriggersConfig          `yaml:"triggers"`
	RawMessageHistory RawMessageHistoryConfig `yaml:"raw_message_history"`
	ChildAgents       ChildAgentsConfig       `yaml:"child_agents"`
	Log               LogConfig               `yaml:"log"`
	Tools             ToolsConfig             `yaml:"tools"`
	Hooks             HooksConfig             `yaml:"hooks"`
	UI                UIConfig                `yaml:"ui"`
}

// UIConfig 控制 Node 内嵌 Web UI（/ui/）是否挂载。
type UIConfig struct {
	// Enabled 为 nil 时默认 true。
	Enabled *bool `yaml:"enabled"`
}

// UIEnabled 是否挂载浏览器 Web UI。
func (c *Config) UIEnabled() bool {
	if c == nil || c.UI.Enabled == nil {
		return true
	}
	return *c.UI.Enabled
}

// ToolsConfig 控制内置工具行为（如 bash_run 输出解码与压缩）。
type ToolsConfig struct {
	// EnabledGroups 为内置工具组允许列表（见 shared/config AllBuiltinToolGroupNames）；省略或为空表示启用全部。
	EnabledGroups []string `yaml:"enabled_groups"`
	// Enabled 已废弃；请改用 enabled_groups。
	Enabled []string `yaml:"enabled,omitempty"`
	// BashOutputEncoding 为 bash_run 捕获的子进程 stdout/stderr 字节编码（解码为 UTF-8 后交给 LLM）。
	// 留空时默认 utf-8；单次 bash_run 可用 output_encoding 覆盖。
	BashOutputEncoding string             `yaml:"bash_output_encoding"`
	// FileEncoding 为 read_file/write_file/search_replace/grep_* 读写磁盘文件的默认字节编码。
	// 留空时默认 utf-8；GBK 遗留文件仍可通过字节检测或 encoding 参数读取。
	FileEncoding string             `yaml:"file_encoding"`
	BashCompress BashCompressConfig `yaml:"bash_compress"`
}

// BashCompressConfig 控制 bash_run 输出清洗（ANSI、重复行）；长度限制由 tool.after_each 落盘摘要负责。
type BashCompressConfig struct {
	// Enabled 为 nil 时默认 true。
	Enabled              *bool `yaml:"enabled"`
	MaxOutputChars       int   `yaml:"max_output_chars"`
	MaxOutputCharsStderr int   `yaml:"max_output_chars_stderr"`
}

const EnvRawMessageHistoryEnabled = "AGENT_RAW_MESSAGE_HISTORY_ENABLED"

// RawMessageHistoryConfig 控制原始消息 JSONL 审计侧车。
type RawMessageHistoryConfig struct {
	Enabled *bool `yaml:"enabled"`
}

// ChildAgentsConfig 控制临时子 Agent（见 docs/architecture/child-agent-tools.md）。
type ChildAgentsConfig struct {
	Enabled                   bool `yaml:"enabled"`
	DefaultTTLSeconds         int  `yaml:"default_ttl_seconds"`
	MaxTTLSeconds             int  `yaml:"max_ttl_seconds"`
	DefaultMaxTurns           int  `yaml:"default_max_turns"`
	MaxMaxTurns               int  `yaml:"max_max_turns"`
	MaxActivePerParent        int  `yaml:"max_active_per_parent"`
	DefaultWaitTimeoutSeconds int  `yaml:"default_wait_timeout_seconds"`
}

// LogConfig 控制 Node 进程 stderr 结构化日志级别。
type LogConfig struct {
	Level string `yaml:"level"`
}

// TriggersConfig 控制触发器存储与调度器。
type TriggersConfig struct {
	Enabled     bool `yaml:"enabled"`
	PollSeconds int  `yaml:"poll_seconds"`
}

// SkillsConfig 控制 session skills 扫描与 prompt 注入。
type SkillsConfig struct {
	Enabled     bool `yaml:"enabled"`
	MaxInPrompt int  `yaml:"max_in_prompt"`
}

// CompressionConfig 控制上下文压缩阈值。
type CompressionConfig struct {
	SilentTriggerTokens          int `yaml:"silent_trigger_tokens"`
	BlockingTriggerTokens        int `yaml:"blocking_trigger_tokens"`
	IdleAutoCompressSeconds      int `yaml:"idle_auto_compress_seconds"`
	IdleAutoCompressPollSeconds  int `yaml:"idle_auto_compress_poll_seconds"`
	IdleAutoCompressMinTokens    int `yaml:"idle_auto_compress_min_tokens"`
}

// HooksConfig 控制 Node turn Hook 行为。
type HooksConfig struct {
	Plugins           []HookPluginConfig          `yaml:"plugins"`
	Host              HookHostConfig              `yaml:"host"`
	DuplicateToolCall DuplicateToolCallHookConfig `yaml:"duplicate_tool_call"`
	ToolResult        ToolResultHookConfig        `yaml:"tool_result"`
}

// HookPluginConfig 为 in-process Go plugin（.so）配置。
type HookPluginConfig struct {
	Path      string   `yaml:"path"`
	Phases    []string `yaml:"phases"`
	Priority  int      `yaml:"priority"`
	OnError   string   `yaml:"on_error"`
	TimeoutMS int      `yaml:"timeout_ms"`
}

// HookHostConfig 控制 Hook Host 配额与可选 history 截断。
type HookHostConfig struct {
	MaxLLMCalls   int `yaml:"max_llm_calls"`
	HistoryWindow int `yaml:"history_window"` // ≤0 或不设：不截断 Context history
}

// ToolResultHookConfig 控制 tool.after_each 对列出的工具做落盘 + history 摘要（WS3）。
type ToolResultHookConfig struct {
	// Enabled 为 nil 时默认 true。
	Enabled *bool `yaml:"enabled"`
	// SpillThresholdTokens 单条 tool 结果超过该估算 token 数时落盘并对 history 头尾摘要；省略时 12000。
	// 作用于下方 tools 列表中的每个工具，非 bash_run 专用，也非整段 session history 上限。
	SpillThresholdTokens int `yaml:"spill_threshold_tokens"`
	// MaxHistoryTokens 已废弃，请改用 spill_threshold_tokens。
	MaxHistoryTokens int `yaml:"max_history_tokens,omitempty"`
	// MaxHistoryRunes 已废弃。
	MaxHistoryRunes int `yaml:"max_history_runes,omitempty"`
	// Tools 启用落盘摘要的工具名；省略时默认 bash + fs + a2a（见 defaultToolResultHookTools）。
	Tools []string `yaml:"tools"`
}

// DuplicateToolCallHookConfig 控制 rule+auto 路径的重复 tool call 检测。
type DuplicateToolCallHookConfig struct {
	// Enabled 为 nil 时默认 true。
	Enabled *bool `yaml:"enabled"`
	// WindowSeconds 为指纹重复判定窗口；省略或 ≤0 时默认 60。
	WindowSeconds int `yaml:"window_seconds"`
}

const defaultDuplicateToolCallWindowSeconds = 60

// HooksHostMaxLLMCalls 返回 turn 内 Hook LLM 调用配额（默认 2）。
func (c *Config) HooksHostMaxLLMCalls() int {
	if c == nil || c.Hooks.Host.MaxLLMCalls <= 0 {
		return 2
	}
	return c.Hooks.Host.MaxLLMCalls
}

// HooksHostHistoryWindow 返回 Hook Context history 窗口；≤0 表示不截断。
func (c *Config) HooksHostHistoryWindow() int {
	if c == nil {
		return 0
	}
	return c.Hooks.Host.HistoryWindow
}

// DuplicateToolCallHookEnabled 是否启用重复 tool call 检测。
func (c *Config) DuplicateToolCallHookEnabled() bool {
	if c == nil || c.Hooks.DuplicateToolCall.Enabled == nil {
		return true
	}
	return *c.Hooks.DuplicateToolCall.Enabled
}

// DuplicateToolCallWindowSeconds 返回重复检测窗口秒数（默认 60）。
func (c *Config) DuplicateToolCallWindowSeconds() int {
	if c == nil || c.Hooks.DuplicateToolCall.WindowSeconds <= 0 {
		return defaultDuplicateToolCallWindowSeconds
	}
	return c.Hooks.DuplicateToolCall.WindowSeconds
}

const defaultToolResultSpillThresholdTokens = 12000

// ToolResultHookEnabled 是否启用 tool 结果摘要落盘。
func (c *Config) ToolResultHookEnabled() bool {
	if c == nil || c.Hooks.ToolResult.Enabled == nil {
		return true
	}
	return *c.Hooks.ToolResult.Enabled
}

// ToolResultSpillThresholdTokens 返回单条 tool 结果触发落盘摘要的 token 阈值（默认 12000）。
func (c *Config) ToolResultSpillThresholdTokens() int {
	if c == nil {
		return defaultToolResultSpillThresholdTokens
	}
	if c.Hooks.ToolResult.SpillThresholdTokens > 0 {
		return c.Hooks.ToolResult.SpillThresholdTokens
	}
	if c.Hooks.ToolResult.MaxHistoryTokens > 0 {
		return c.Hooks.ToolResult.MaxHistoryTokens
	}
	if c.Hooks.ToolResult.MaxHistoryRunes > 0 {
		return int(float64(c.Hooks.ToolResult.MaxHistoryRunes) * 0.6)
	}
	return defaultToolResultSpillThresholdTokens
}

// defaultToolResultHookTools 与 node/internal/toolresult.DefaultToolResultTools 保持一致。
func defaultToolResultHookTools() []string {
	return []string{
		"bash_run", "read_file", "grep_file", "grep_files",
		"search_replace", "glob_files", "agent_invoke", "agent_discover",
	}
}

// ToolResultHookTools 返回启用摘要的工具列表。
func (c *Config) ToolResultHookTools() []string {
	if c == nil || len(c.Hooks.ToolResult.Tools) == 0 {
		return defaultToolResultHookTools()
	}
	return append([]string(nil), c.Hooks.ToolResult.Tools...)
}

// ListenConfig 描述 Agent Node HTTP 监听地址。
type ListenConfig struct {
	Host string `yaml:"host"`
	Port int    `yaml:"port"`
}

// LocalConfig 描述 Client 连接本地 Node 的 endpoint；agent_id 可选，用于与 Node 响应交叉校验。
type LocalConfig struct {
	Endpoint string `yaml:"endpoint"`
	AgentID  string `yaml:"agent_id"`
}

// LLMConfig 为 turn loop 使用的模型配置。
type LLMConfig struct {
	Provider        string `yaml:"provider"`
	BaseURL         string `yaml:"base_url"`
	Model           string `yaml:"model"`
	APIKeyEnv       string `yaml:"api_key_env"`
	Mock            bool   `yaml:"mock"`
	MaxToolLoops    int    `yaml:"max_tool_loops"`
	Thinking        string `yaml:"thinking"`         // deepseek/qwen：enabled | disabled
	ReasoningEffort string `yaml:"reasoning_effort"` // thinking=enabled：high | max（qwen 映射为 thinking_budget）
}

// ManageConfig 控制是否向 Manage 注册；默认 enabled=false。
type ManageConfig struct {
	Enabled      bool                     `yaml:"enabled"`
	URL          string                   `yaml:"url"`
	NodeToken    string                   `yaml:"node_token"`
	Registration ManageRegistrationConfig `yaml:"registration"`
	A2A          ManageA2AConfig          `yaml:"a2a"`
	Update       ManageUpdateConfig       `yaml:"update"`
}

// ManageUpdateConfig 控制 Node 向 Manage Release Hub 查询更新。
type ManageUpdateConfig struct {
	Enabled              *bool  `yaml:"enabled"`
	CheckIntervalSeconds int    `yaml:"check_interval_seconds"`
	Channel              string `yaml:"channel"`
}

// ManageA2AConfig 控制 Node 对 Manage A2A inbox 的 long poll sidecar。
type ManageA2AConfig struct {
	Enabled          *bool `yaml:"enabled"`
	InboxPollSeconds int   `yaml:"inbox_poll_seconds"`
	InboxWaitSeconds int   `yaml:"inbox_wait_seconds"`
}

// ManageRegistrationConfig 控制周期性 upsert/心跳参数。
// Agent Card（name/description/capabilities/metadata）固定从工作目录 ./agent-card.json 读取，不在此重复配置。
type ManageRegistrationConfig struct {
	BaseURL         string `yaml:"base_url"`
	IntervalSeconds int    `yaml:"interval_seconds"`
	TTLSeconds      int    `yaml:"ttl_seconds"`
	Team            string `yaml:"team"`
}

// LoadFile 从 YAML 文件加载配置并完成默认值填充与环境变量展开。

// 逻辑：
// 1. 读文件并 os.ExpandEnv；
// 2. yaml.Unmarshal 到 Config；
// 3. ApplyDefaults；
// 4. ResolveAgentID（`.runtime/agent/agent_id` 持久化）；
// 5. Validate 后返回。
//
// 异常：文件不存在、YAML 语法错误、校验失败均向上返回 error。
func LoadFile(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	expanded := os.ExpandEnv(string(raw))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	cfg.ApplyDefaults()
	if err := cfg.ResolveAgentID(); err != nil {
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ApplyDefaults 填充 listen/local 等缺省值；可重复调用。
//
// 副作用：修改接收者字段。
func (c *Config) ApplyDefaults() {
	if strings.TrimSpace(c.Listen.Host) == "" {
		c.Listen.Host = DefaultListenHost
	}
	if c.Listen.Port == 0 {
		c.Listen.Port = DefaultListenPort
	}
	if strings.TrimSpace(c.Local.Endpoint) == "" {
		c.Local.Endpoint = fmt.Sprintf("http://%s", c.ListenAddr())
	}
	if strings.TrimSpace(c.FSRoot) == "" {
		c.FSRoot = "./.runtime"
	}
	if c.LLM.MaxToolLoops <= 0 {
		c.LLM.MaxToolLoops = 16
	}
	if strings.TrimSpace(c.LLM.Provider) == "" {
		c.LLM.Provider = "openai"
	}
	if strings.TrimSpace(c.LLM.APIKeyEnv) == "" {
		c.LLM.APIKeyEnv = "OPENAI_API_KEY"
	}
	if c.Skills.MaxInPrompt <= 0 {
		c.Skills.MaxInPrompt = 3
	}
	if c.Triggers.PollSeconds <= 0 {
		c.Triggers.PollSeconds = 5
	}
	if strings.TrimSpace(c.Log.Level) == "" {
		c.Log.Level = "info"
	}
	if c.ChildAgents.DefaultTTLSeconds <= 0 {
		c.ChildAgents.DefaultTTLSeconds = 1800
	}
	if c.ChildAgents.MaxTTLSeconds <= 0 {
		c.ChildAgents.MaxTTLSeconds = 7200
	}
	if c.ChildAgents.DefaultMaxTurns <= 0 {
		c.ChildAgents.DefaultMaxTurns = 20
	}
	if c.ChildAgents.MaxMaxTurns <= 0 {
		c.ChildAgents.MaxMaxTurns = 50
	}
	if c.ChildAgents.MaxActivePerParent <= 0 {
		c.ChildAgents.MaxActivePerParent = 8
	}
	if c.ChildAgents.DefaultWaitTimeoutSeconds <= 0 {
		c.ChildAgents.DefaultWaitTimeoutSeconds = 300
	}
	if c.Manage.Registration.IntervalSeconds <= 0 {
		c.Manage.Registration.IntervalSeconds = 30
	}
	if c.Manage.Registration.TTLSeconds <= 0 {
		c.Manage.Registration.TTLSeconds = 60
	}
	if c.Manage.A2A.InboxPollSeconds <= 0 {
		c.Manage.A2A.InboxPollSeconds = c.Manage.Registration.IntervalSeconds
		if c.Manage.A2A.InboxPollSeconds <= 0 {
			c.Manage.A2A.InboxPollSeconds = 30
		}
	}
	if c.Manage.A2A.InboxWaitSeconds <= 0 {
		c.Manage.A2A.InboxWaitSeconds = 25
	}
	if c.Manage.Update.CheckIntervalSeconds <= 0 {
		c.Manage.Update.CheckIntervalSeconds = 6 * 3600
	}
	if strings.TrimSpace(c.Manage.Update.Channel) == "" {
		c.Manage.Update.Channel = "stable"
	}
	if c.Compression.IdleAutoCompressPollSeconds <= 0 {
		c.Compression.IdleAutoCompressPollSeconds = 60
	}
}

// IdleAutoCompressEnabled 是否在 session 无动作超过 idle_auto_compress_seconds 后自动压缩。
func (c *Config) IdleAutoCompressEnabled() bool {
	return c != nil && c.Compression.IdleAutoCompressSeconds > 0
}

// IdleAutoCompressPollInterval 返回 idle 自动压缩扫描间隔（默认 60s）。
func (c *Config) IdleAutoCompressPollInterval() time.Duration {
	if c == nil || c.Compression.IdleAutoCompressPollSeconds <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.Compression.IdleAutoCompressPollSeconds) * time.Second
}

// RuntimeDir 返回运行时根目录（与 `fs_root` 一致；子目录路径均相对此根硬编码）。
func (c *Config) RuntimeDir() string {
	root := strings.TrimRight(strings.TrimSpace(c.FSRoot), "/")
	if root == "" {
		return "./.runtime"
	}
	return root
}

// DataDir 返回临时工作区目录（`<fs_root>/data`）。
func (c *Config) DataDir() string {
	return filepath.Join(c.RuntimeDir(), "data")
}

// SkillsRoot 返回 skills 目录（`<fs_root>/skills`）。
func (c *Config) SkillsRoot() string {
	return filepath.Join(c.RuntimeDir(), "skills")
}

// Validate 校验 Node 启动所需的最小字段集。
func (c *Config) Validate() error {
	if strings.TrimSpace(c.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	if c.Listen.Port < 1 || c.Listen.Port > 65535 {
		return fmt.Errorf("listen.port must be 1-65535, got %d", c.Listen.Port)
	}
	if _, err := url.Parse(c.Local.Endpoint); err != nil {
		return fmt.Errorf("local.endpoint invalid: %w", err)
	}
	if c.Manage.Enabled && strings.TrimSpace(c.Manage.URL) == "" {
		return fmt.Errorf("manage.url is required when manage.enabled is true")
	}
	if c.Manage.Enabled {
		if base := strings.TrimSpace(c.Manage.Registration.BaseURL); base != "" {
			if _, err := url.Parse(base); err != nil {
				return fmt.Errorf("manage.registration.base_url invalid: %w", err)
			}
		}
	}
	if err := validateToolsEnabledConfig(&c.Tools); err != nil {
		return err
	}
	return nil
}

// DiscoveryGroups 返回 YAML groups 字段（**不**用于 Manage 注册；分组由 Manage 分配）。
func (c *Config) DiscoveryGroups() []string {
	seen := make(map[string]struct{}, len(c.Groups))
	out := make([]string, 0, len(c.Groups))
	for _, raw := range c.Groups {
		group := strings.TrimSpace(raw)
		if group == "" {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		out = append(out, group)
	}
	return out
}

// ManageRegistrationInterval 返回心跳/注册轮询间隔。
func (c *Config) ManageRegistrationInterval() time.Duration {
	if c == nil || c.Manage.Registration.IntervalSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.Manage.Registration.IntervalSeconds) * time.Second
}

// ManageRegistryBaseURL 返回上报 Manage Registry 的 base_url。
// 优先 manage.registration.base_url；留空则回退 local.endpoint。
func (c *Config) ManageRegistryBaseURL() string {
	if c == nil {
		return ""
	}
	if base := strings.TrimSpace(c.Manage.Registration.BaseURL); base != "" {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(strings.TrimSpace(c.Local.Endpoint), "/")
}

// ManageRegistryBaseURLIsLoopback 判断上报地址是否为 loopback（运维/A2A 可达性提示用）。
func (c *Config) ManageRegistryBaseURLIsLoopback() bool {
	raw := c.ManageRegistryBaseURL()
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := strings.ToLower(strings.Trim(u.Hostname(), "[]"))
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// ManageA2AEnabled 是否启动 A2A inbox long poll sidecar（须 manage.enabled=true；默认 true）。
func (c *Config) ManageA2AEnabled() bool {
	if c == nil || !c.Manage.Enabled {
		return false
	}
	if c.Manage.A2A.Enabled != nil {
		return *c.Manage.A2A.Enabled
	}
	return true
}

// ManageUpdateEnabled 是否向 Manage Release Hub 查询更新（须 manage.enabled=true；默认 true）。
func (c *Config) ManageUpdateEnabled() bool {
	if c == nil || !c.Manage.Enabled {
		return false
	}
	if c.Manage.Update.Enabled != nil {
		return *c.Manage.Update.Enabled
	}
	return true
}

// ManageUpdateCheckInterval 返回版本检查间隔（默认 6h）。
func (c *Config) ManageUpdateCheckInterval() time.Duration {
	if c == nil || c.Manage.Update.CheckIntervalSeconds <= 0 {
		return 6 * time.Hour
	}
	return time.Duration(c.Manage.Update.CheckIntervalSeconds) * time.Second
}

// ManageA2AInboxWait 返回 long poll wait 参数。
func (c *Config) ManageA2AInboxWait() time.Duration {
	if c == nil || c.Manage.A2A.InboxWaitSeconds <= 0 {
		return 25 * time.Second
	}
	return time.Duration(c.Manage.A2A.InboxWaitSeconds) * time.Second
}

// ManageA2AInboxPollInterval 返回断线降级后的短 poll 间隔。
func (c *Config) ManageA2AInboxPollInterval() time.Duration {
	if c == nil || c.Manage.A2A.InboxPollSeconds <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.Manage.A2A.InboxPollSeconds) * time.Second
}

// ListenAddr 返回 host:port 监听地址字符串。
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Listen.Host, c.Listen.Port)
}

// MemoryDir 返回持久化记忆目录（`<runtime>/memory`）。
func (c *Config) MemoryDir() string {
	return filepath.Join(c.RuntimeDir(), "memory")
}

// SessionDBPath 返回 SQLite 会话库路径（`<runtime>/memory/sessions.db`）。
func (c *Config) SessionDBPath() string {
	return filepath.Join(c.MemoryDir(), "sessions.db")
}

// RawMessageHistoryEnabled 返回是否写入原始消息 JSONL；环境变量优先于 YAML，默认 true。
func (c *Config) RawMessageHistoryEnabled() bool {
	if v, ok := os.LookupEnv(EnvRawMessageHistoryEnabled); ok {
		return parseEnvBool(v, true)
	}
	if c.RawMessageHistory.Enabled != nil {
		return *c.RawMessageHistory.Enabled
	}
	return true
}

// RawMessageHistoryDir 返回 JSONL 根目录（`<runtime>/history`）。
func (c *Config) RawMessageHistoryDir() string {
	return filepath.Join(c.RuntimeDir(), "history")
}

func parseEnvBool(value string, defaultVal bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultVal
	}
}

// TriggersStorePath 返回 triggers.json 路径（`<fs_root>/triggers/triggers.json`）。
func (c *Config) TriggersStorePath() string {
	return filepath.Join(c.RuntimeDir(), "triggers", "triggers.json")
}

// PolicyDir 返回策略目录路径（`<runtime>/policy`）。
func (c *Config) PolicyDir() string {
	return filepath.Join(c.RuntimeDir(), "policy")
}

// ToolPolicyPath 返回工具级策略文件路径。
func (c *Config) ToolPolicyPath() string {
	return filepath.Join(c.PolicyDir(), "tool.approval.txt")
}

// ShellPolicyDir 返回 shell 级策略目录。
func (c *Config) ShellPolicyDir() string {
	return filepath.Join(c.PolicyDir(), "shell")
}

// ExternalToolsDir 返回外置 CLI/工具目录（`<fs_root>/externaltools`）。
func (c *Config) ExternalToolsDir() string {
	return filepath.Join(c.RuntimeDir(), "externaltools")
}

// Capabilities 返回对外声明的能力列表。
func (c *Config) Capabilities() []string {
	caps := []string{"shell", "filesystem"}
	if c.Triggers.Enabled {
		caps = append(caps, "triggers")
	}
	return caps
}
