package config

// PlacementConfig 控制本 Node 是否接受同组其他 Node 的 Placement 能力。
// 与 A2A expose（manage.a2a.accept_inbound）无关。
type PlacementConfig struct {
	// AllowPeerCreate 为 nil 时默认 true（允许被远端创建 Agent）。
	AllowPeerCreate *bool `yaml:"allow_peer_create"`
	// AllowScreenView 为 nil 时默认 true（允许旁观屏幕；无 GUI 时仍由 home 返回 unavailable）。
	AllowScreenView *bool `yaml:"allow_screen_view"`
}

// AllowPeerCreateEffective 同组 owner 是否可在本 Node 创建 Agent。
func (c *Config) AllowPeerCreateEffective() bool {
	if c == nil || c.Placement.AllowPeerCreate == nil {
		return true
	}
	return *c.Placement.AllowPeerCreate
}

// AllowScreenViewEffective 是否允许经 Edge 旁观本机屏幕。
func (c *Config) AllowScreenViewEffective() bool {
	if c == nil || c.Placement.AllowScreenView == nil {
		return true
	}
	return *c.Placement.AllowScreenView
}
