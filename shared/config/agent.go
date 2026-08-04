package config

import "strings"

// AgentConfig 描述本 Node 在 Manage Registry 上的展示身份（历史字段名 agent，语义已是 Node）。
// Role 仅为可选元数据，不再驱动 expose / inbox / handler。
type AgentConfig struct {
	Name         string         `yaml:"name"` // Node 展示名（Manage Console / peers）
	Description  string         `yaml:"description"`
	Role         string         `yaml:"role"` // deprecated: 勿用于门控；保留写入 card.metadata
	Capabilities []string       `yaml:"capabilities"`
	Metadata     map[string]any `yaml:"metadata"`
}

// AgentRole 返回可选元数据角色字符串；空串表示未设。
func (c *Config) AgentRole() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Agent.Role)
}

// NodeDisplayName 返回 Manage / peers 展示名；空则回退 node_id。
func (c *Config) NodeDisplayName() string {
	return c.AgentDisplayName()
}

// AgentDisplayName 同 NodeDisplayName（兼容旧调用点）。
func (c *Config) AgentDisplayName() string {
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

