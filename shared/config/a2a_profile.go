package config

// AgentRoleCompliance 为历史 A2A 被调档常量；新架构请用 Manage.A2A.AcceptInbound，
// 勿再以 role 字符串驱动 expose / inbox / handler。
const AgentRoleCompliance = "compliance"

// ExposeToPeersEffective 是否可作为 A2A 被调目标。
// 仅看显式开关 manage.a2a.accept_inbound（nil/false → 不暴露）。
// 不再由 agent.role=compliance/ops 推导。
func (c *Config) ExposeToPeersEffective() bool {
	if c == nil || c.Manage.A2A.AcceptInbound == nil {
		return false
	}
	return *c.Manage.A2A.AcceptInbound
}

// ManageA2AEnabled 是否启动 inbox long poll。
// 须 manage.enabled；显式 manage.a2a.enabled 优先；否则跟随 accept_inbound。
func (c *Config) ManageA2AEnabled() bool {
	return c.ManageA2AInboxEnabled()
}

// ManageA2AInboxEnabled 同上（保留旧名调用点）。
func (c *Config) ManageA2AInboxEnabled() bool {
	if c == nil || !c.Manage.Enabled {
		return false
	}
	if c.Manage.A2A.Enabled != nil {
		return *c.Manage.A2A.Enabled
	}
	return c.ExposeToPeersEffective()
}

// ManageA2AInboxEnabledForRole 已废弃：忽略 role，行为同 ManageA2AInboxEnabled。
// 保留符号以免外部包编译失败；请改调 ManageA2AEnabled。
func (c *Config) ManageA2AInboxEnabledForRole(role string) bool {
	_ = role
	return c.ManageA2AInboxEnabled()
}

// ExposeToPeersForRole 已废弃：忽略 role；仅当 yamlOverride 非 nil 时返回其值，否则 false。
func ExposeToPeersForRole(role string, yamlOverride *bool) bool {
	_ = role
	if yamlOverride != nil {
		return *yamlOverride
	}
	return false
}
