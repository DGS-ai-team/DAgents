package config

import "strings"

const AgentRoleCompliance = "compliance"

// ExposeToPeersForRole 根据 agent.role 推导是否可作为 A2A 被调目标。
func ExposeToPeersForRole(role string, yamlOverride *bool) bool {
	if yamlOverride != nil {
		return *yamlOverride
	}
	return strings.EqualFold(strings.TrimSpace(role), AgentRoleCompliance)
}

// ManageA2AInboxEnabledForRole 是否启动 inbox long poll。
// 显式 manage.a2a.enabled 优先；否则 role=compliance 时默认开启。
func (c *Config) ManageA2AInboxEnabledForRole(role string) bool {
	if c == nil || !c.Manage.Enabled {
		return false
	}
	if c.Manage.A2A.Enabled != nil {
		return *c.Manage.A2A.Enabled
	}
	return ExposeToPeersForRole(role, nil)
}
