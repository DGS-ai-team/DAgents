package config

// PlacementConfig 遗留 YAML 字段（D5：产品能力已关闭，仅兼容旧配置反序列化）。
type PlacementConfig struct {
	// AllowPeerCreate 已废弃；Effective 恒为 false。
	AllowPeerCreate *bool `yaml:"allow_peer_create"`
	// AllowScreenView 已废弃；Effective 恒为 false。
	AllowScreenView *bool `yaml:"allow_screen_view"`
}

// AllowPeerCreateEffective D5：远程 Placement 创建已下线，恒为 false。
func (c *Config) AllowPeerCreateEffective() bool {
	return false
}

// AllowScreenViewEffective D5：Placement 旁观已下线，恒为 false。
func (c *Config) AllowScreenViewEffective() bool {
	return false
}
