package config

import "strings"

// AgentConfig 描述本 Node 在 Manage Registry 上的展示身份（历史字段名 agent，语义已是 Node）。
type AgentConfig struct {
	Name         string         `yaml:"name"` // Node 展示名（Manage Console / peers）
	Description  string         `yaml:"description"`
	Capabilities []string       `yaml:"capabilities"`
	Metadata     map[string]any `yaml:"metadata"`
}

// NodeDisplayName 返回 Manage / peers 展示名；空则回退 node_id。
func (c *Config) NodeDisplayName() string {
	if c == nil {
		return ""
	}
	if n := strings.TrimSpace(c.Agent.Name); n != "" {
		return n
	}
	return strings.TrimSpace(c.NodeID)
}

// AgentDescription 返回 Agent 简介。
func (c *Config) AgentDescription() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Agent.Description)
}

// PreferredName 返回本机使用者称呼；空串表示未设。
func (c *Config) PreferredName() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.User.PreferredName)
}

// NodeProfileCompleted 是否已完成首次 Node 身份配置。
func (c *Config) NodeProfileCompleted() bool {
	if c == nil {
		return true
	}
	return c.Onboarding.NodeProfileCompleted
}

// RegistrationCapabilities 返回注册 Manage 时的 capabilities；agent.capabilities 非空时覆盖 config 默认。
func (c *Config) RegistrationCapabilities() []string {
	if c == nil {
		return nil
	}
	caps := c.Capabilities()
	if len(c.Agent.Capabilities) == 0 {
		return caps
	}
	return append([]string(nil), c.Agent.Capabilities...)
}
