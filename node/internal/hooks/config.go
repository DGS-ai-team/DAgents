package hooks

import "time"

// DuplicateConfig 控制重复 tool call 检测（仅 rule+auto 路径）。
type DuplicateConfig struct {
	Enabled       bool
	WindowSeconds int
}

// DefaultDuplicateConfig 返回默认配置（60 秒窗口，启用）。
func DefaultDuplicateConfig() DuplicateConfig {
	return DuplicateConfig{
		Enabled:       true,
		WindowSeconds: 60,
	}
}

// DuplicateConfigOrDefault 将 TurnOptions 传入的配置与默认值合并（零值 struct 视为未配置）。
func DuplicateConfigOrDefault(c DuplicateConfig) DuplicateConfig {
	if c == (DuplicateConfig{}) {
		return DefaultDuplicateConfig()
	}
	out := DefaultDuplicateConfig()
	out.Enabled = c.Enabled
	if c.WindowSeconds > 0 {
		out.WindowSeconds = c.WindowSeconds
	}
	return out
}

func (c DuplicateConfig) windowDuration() time.Duration {
	sec := c.WindowSeconds
	if sec <= 0 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}
