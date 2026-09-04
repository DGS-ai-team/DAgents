package hooks

import "time"

// DuplicateConfig 控制重复 tool call 检测（仅 rule+auto 路径）。
type DuplicateConfig struct {
	// Enabled 为 nil 时表示未配置，使用默认值 true；非 nil 时尊重显式开关。
	Enabled       *bool
	WindowSeconds int
}

// DefaultDuplicateConfig 返回默认配置（60 秒窗口，启用）。
func DefaultDuplicateConfig() DuplicateConfig {
	enabled := true
	return DuplicateConfig{
		Enabled:       &enabled,
		WindowSeconds: 60,
	}
}

// DuplicateConfigOrDefault 将 TurnOptions 传入的配置与默认值合并（零值 struct 视为未配置）。
func DuplicateConfigOrDefault(c DuplicateConfig) DuplicateConfig {
	if c.Enabled == nil && c.WindowSeconds == 0 {
		return DefaultDuplicateConfig()
	}
	out := DefaultDuplicateConfig()
	if c.Enabled != nil {
		enabled := *c.Enabled
		out.Enabled = &enabled
	}
	if c.WindowSeconds > 0 {
		out.WindowSeconds = c.WindowSeconds
	}
	return out
}

// IsEnabled 返回是否启用。nil 仅在调用方传入未归一化配置时出现，此时采用默认值。
func (c DuplicateConfig) IsEnabled() bool {
	return c.Enabled == nil || *c.Enabled
}

func (c DuplicateConfig) windowDuration() time.Duration {
	sec := c.WindowSeconds
	if sec <= 0 {
		sec = 60
	}
	return time.Duration(sec) * time.Second
}

// InjectTodayDateConfig 控制 request-only ContextInjection 中的当天日期。
type InjectTodayDateConfig struct {
	// Enabled 为 nil 时默认 true。
	Enabled *bool
}

// DefaultInjectTodayDateConfig 返回默认配置（启用）。
func DefaultInjectTodayDateConfig() InjectTodayDateConfig {
	enabled := true
	return InjectTodayDateConfig{Enabled: &enabled}
}

// InjectTodayDateConfigOrDefault 合并默认值。
func InjectTodayDateConfigOrDefault(c InjectTodayDateConfig) InjectTodayDateConfig {
	if c.Enabled == nil {
		return DefaultInjectTodayDateConfig()
	}
	enabled := *c.Enabled
	return InjectTodayDateConfig{Enabled: &enabled}
}

// IsEnabled 返回是否启用（nil 视为 true）。
func (c InjectTodayDateConfig) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}
