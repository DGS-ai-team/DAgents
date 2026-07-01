package config

import "strings"

// AgentConfig 描述 Agent 对外身份与 A2A 角色（原 agent-card.json 内容）。
type AgentConfig struct {
	Name           string         `yaml:"name"`
	Description    string         `yaml:"description"`
	Role           string         `yaml:"role"`
	CompliancePeer string         `yaml:"compliance_peer"`
	Capabilities   []string       `yaml:"capabilities"`
	Metadata       map[string]any `yaml:"metadata"`
}

// AgentRole 返回 A2A 角色（如 compliance、ops）；空串表示非 A2A 专用角色。
func (c *Config) AgentRole() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Agent.Role)
}

// AgentDisplayName 返回 Manage 展示名；空则回退 agent_id。
func (c *Config) AgentDisplayName() string {
	if c == nil {
		return ""
	}
	if n := strings.TrimSpace(c.Agent.Name); n != "" {
		return n
	}
	return strings.TrimSpace(c.AgentID)
}

// AgentDescription 返回 Agent 简介。
func (c *Config) AgentDescription() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Agent.Description)
}

// CompliancePeer 返回 agent_invoke 默认目标 Agent ID。
func (c *Config) CompliancePeer() string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(c.Agent.CompliancePeer)
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

// ExposeToPeersEffective 是否可作为 A2A 被调目标（role=compliance）。
func (c *Config) ExposeToPeersEffective() bool {
	return ExposeToPeersForRole(c.AgentRole(), nil)
}

// ManageA2AEnabled 是否启动 inbox long poll（须 manage.enabled；默认随 role=compliance）。
func (c *Config) ManageA2AEnabled() bool {
	return c.ManageA2AInboxEnabledForRole(c.AgentRole())
}
