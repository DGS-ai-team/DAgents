package config

// AgentRoleCompliance 为历史 A2A 被调档常量；inbox callee 已退役，勿再以 role 驱动 expose / inbox。
const AgentRoleCompliance = "compliance"

// ExposeToPeersEffective 是否在 Registry 上标记 expose_to_peers（历史兼容）。
// A2A inbox callee 已退役后，新建 invoke 由 Manage 返回 410；此开关仅影响注册广告。
func (c *Config) ExposeToPeersEffective() bool {
	if c == nil || c.Manage.A2A.AcceptInbound == nil {
		return false
	}
	return *c.Manage.A2A.AcceptInbound
}

// ManageA2AEnabled 是否启动 A2A inbox long poll。
// Inbox callee 已退役：恒为 false（保留符号以免外部包编译失败）。
func (c *Config) ManageA2AEnabled() bool {
	return false
}

// ManageA2AInboxEnabled 同上（保留旧名调用点）。
func (c *Config) ManageA2AInboxEnabled() bool {
	return false
}

// ManageA2AInboxEnabledForRole 已废弃：恒 false。
func (c *Config) ManageA2AInboxEnabledForRole(role string) bool {
	_ = role
	return false
}

// ExposeToPeersForRole 已废弃：忽略 role；仅当 yamlOverride 非 nil 时返回其值，否则 false。
func ExposeToPeersForRole(role string, yamlOverride *bool) bool {
	_ = role
	if yamlOverride != nil {
		return *yamlOverride
	}
	return false
}
