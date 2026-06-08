// Package config 提供 Agent Node 与 Client 共用的 YAML 配置模型与加载逻辑。
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

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
	PolicyFile    string             `yaml:"policy_file"`
	DataDir       string             `yaml:"data_dir"`
	Skills        SkillsConfig       `yaml:"skills"`
	Compression   CompressionConfig  `yaml:"compression"`
	Triggers          TriggersConfig          `yaml:"triggers"`
	RawMessageHistory RawMessageHistoryConfig `yaml:"raw_message_history"`
	ChildAgents       ChildAgentsConfig       `yaml:"child_agents"`
	Log               LogConfig               `yaml:"log"`
	Tools             ToolsConfig             `yaml:"tools"`
}

// ToolsConfig 控制内置工具行为（如 bash_run 输出解码与压缩）。
type ToolsConfig struct {
	// BashOutputEncoding 为 bash_run 捕获的子进程 stdout/stderr 字节编码（解码为 UTF-8 后交给 LLM）。
	// 留空时由 Node 按 OS/shell 类型自动选择（Windows cmd/powershell→gbk，bash→utf-8）。
	BashOutputEncoding string             `yaml:"bash_output_encoding"`
	BashCompress       BashCompressConfig `yaml:"bash_compress"`
}

// BashCompressConfig 控制 bash_run 输出压缩（P0：清洗 + rune 截断）。
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
	Enabled     bool   `yaml:"enabled"`
	PollSeconds int    `yaml:"poll_seconds"`
	StorePath   string `yaml:"store_path"`
}

// SkillsConfig 控制 session skills 扫描与 prompt 注入。
type SkillsConfig struct {
	Enabled     bool   `yaml:"enabled"`
	MaxInPrompt int    `yaml:"max_in_prompt"`
	Root        string `yaml:"root"`
}

// CompressionConfig 控制上下文压缩阈值。
type CompressionConfig struct {
	SilentTriggerTokens   int `yaml:"silent_trigger_tokens"`
	BlockingTriggerTokens int `yaml:"blocking_trigger_tokens"`
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
	Provider     string `yaml:"provider"`
	BaseURL      string `yaml:"base_url"`
	Model        string `yaml:"model"`
	APIKeyEnv    string `yaml:"api_key_env"`
	Mock         bool   `yaml:"mock"`
	MaxToolLoops int    `yaml:"max_tool_loops"`
}

// ManageConfig 控制是否向 Manage 注册；AC 阶段默认 enabled=false。
type ManageConfig struct {
	Enabled   bool   `yaml:"enabled"`
	URL       string `yaml:"url"`
	NodeToken string `yaml:"node_token"`
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
	if strings.TrimSpace(c.DataDir) == "" {
		c.DataDir = "./.runtime/data"
	}
	if c.LLM.MaxToolLoops <= 0 {
		c.LLM.MaxToolLoops = 16
	}
	if strings.TrimSpace(c.LLM.Provider) == "" {
		c.LLM.Provider = "openai"
	}
	if c.Skills.MaxInPrompt <= 0 {
		c.Skills.MaxInPrompt = 3
	}
	if strings.TrimSpace(c.Skills.Root) == "" {
		c.Skills.Root = defaultSkillsRoot(c.DataDir)
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
}

// RuntimeDir 返回 `.runtime` 目录路径（由 data_dir 的父目录推导）。
func (c *Config) RuntimeDir() string {
	base := strings.TrimRight(strings.TrimSpace(c.DataDir), "/")
	if base == "" {
		return "./.runtime"
	}
	return filepath.Dir(base)
}

func defaultSkillsRoot(dataDir string) string {
	base := strings.TrimSpace(dataDir)
	if base == "" {
		return "./.runtime/skills"
	}
	return filepath.Join(filepath.Dir(base), "skills")
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
	return nil
}

// ListenAddr 返回 host:port 监听地址字符串。
func (c *Config) ListenAddr() string {
	return fmt.Sprintf("%s:%d", c.Listen.Host, c.Listen.Port)
}

// SessionDBPath 返回 SQLite 会话库路径。
func (c *Config) SessionDBPath() string {
	return fmt.Sprintf("%s/sessions.db", strings.TrimRight(c.DataDir, "/"))
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

// TriggersStorePath 返回 triggers.json 路径。
func (c *Config) TriggersStorePath() string {
	if p := strings.TrimSpace(c.Triggers.StorePath); p != "" {
		return p
	}
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

// Capabilities 返回对外声明的能力列表。
func (c *Config) Capabilities() []string {
	caps := []string{"shell", "filesystem"}
	if c.Triggers.Enabled {
		caps = append(caps, "triggers")
	}
	return caps
}

// ManageRegistered 表示当前是否已向 Manage 完成注册（N0 恒为 false）。
func (c *Config) ManageRegistered() bool {
	return false
}
