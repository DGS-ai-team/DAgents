package config

import (
	"fmt"
	"strings"
)

// BrowserConfig 控制内置 browser_* 工具（remote → dagents-browser + browser-use）。
type BrowserConfig struct {
	// Enabled 为 nil 时默认 false（须显式启用）。
	Enabled *bool `yaml:"enabled"`
	// Headed 为 nil 时默认 true（演示/回放场景）。
	Headed            *bool    `yaml:"headed"`
	DefaultTimeoutMS  int      `yaml:"default_timeout_ms"`
	OutputDir         string   `yaml:"output_dir"`
	ChromePath        string   `yaml:"chrome_path"`
	CDPURL            string   `yaml:"cdp_url"`
	DebugPort         int      `yaml:"debug_port"`
	MaxSessions       int      `yaml:"max_sessions"`
	IdleStopSeconds   int      `yaml:"idle_stop_seconds"`
	AllowedURLSchemes []string `yaml:"allowed_url_schemes"`
	// ServiceURL dagents-browser 基址；默认 http://127.0.0.1:18766。
	ServiceURL string `yaml:"service_url"`
	// IgnoreHTTPSErrors 为 nil 时默认 false。
	IgnoreHTTPSErrors *bool `yaml:"ignore_https_errors"`
}

const DefaultBrowserServicePort = 18766

// BrowserEnabled 是否启用 browser_* 工具。
func (c *Config) BrowserEnabled() bool {
	if c == nil || c.Browser.Enabled == nil {
		return false
	}
	return *c.Browser.Enabled
}

// BrowserHeaded 是否默认 headed 启动 Chrome。
func (c *Config) BrowserHeaded() bool {
	if c == nil || c.Browser.Headed == nil {
		return true
	}
	return *c.Browser.Headed
}

// BrowserOutputDir 返回相对 Agent workspace 的截图输出目录。
func (c *Config) BrowserOutputDir() string {
	if c == nil {
		return "browser"
	}
	dir := strings.TrimSpace(c.Browser.OutputDir)
	if dir == "" {
		return "browser"
	}
	return strings.Trim(dir, "/")
}

// BrowserDefaultTimeoutMS 返回默认超时毫秒。
func (c *Config) BrowserDefaultTimeoutMS() int {
	if c == nil || c.Browser.DefaultTimeoutMS <= 0 {
		return 30000
	}
	return c.Browser.DefaultTimeoutMS
}

// BrowserMaxSessions 返回并发 browser session 上限。
func (c *Config) BrowserMaxSessions() int {
	if c == nil || c.Browser.MaxSessions <= 0 {
		return 8
	}
	return c.Browser.MaxSessions
}

// BrowserAllowedURLSchemes 返回 navigate 允许的 URL scheme。
func (c *Config) BrowserAllowedURLSchemes() []string {
	if c == nil || len(c.Browser.AllowedURLSchemes) == 0 {
		return []string{"https", "http"}
	}
	out := make([]string, 0, len(c.Browser.AllowedURLSchemes))
	for _, raw := range c.Browser.AllowedURLSchemes {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"https", "http"}
	}
	return out
}

// BrowserIgnoreHTTPSErrors 是否忽略 HTTPS 证书错误（内网自签）。
func (c *BrowserConfig) BrowserIgnoreHTTPSErrors() bool {
	if c == nil || c.IgnoreHTTPSErrors == nil {
		return false
	}
	return *c.IgnoreHTTPSErrors
}

func (c *Config) applyBrowserDefaults() {
	if c == nil {
		return
	}
	if c.Browser.DefaultTimeoutMS <= 0 {
		c.Browser.DefaultTimeoutMS = 30000
	}
	if strings.TrimSpace(c.Browser.OutputDir) == "" {
		c.Browser.OutputDir = "browser"
	}
	if c.Browser.MaxSessions <= 0 {
		// 伴生方案：每主 Agent 一个 Chrome；默认允许多会话并发。
		c.Browser.MaxSessions = 8
	}
	if c.Browser.DebugPort <= 0 {
		c.Browser.DebugPort = 9222
	}
	if len(c.Browser.AllowedURLSchemes) == 0 {
		c.Browser.AllowedURLSchemes = []string{"https", "http"}
	}
	if strings.TrimSpace(c.Browser.ServiceURL) == "" {
		c.Browser.ServiceURL = fmt.Sprintf("http://127.0.0.1:%d", DefaultBrowserServicePort)
	}
}

func validateBrowserConfig(c *Config) error {
	if c == nil || !c.BrowserEnabled() {
		return nil
	}
	if strings.TrimSpace(c.Browser.ServiceURL) == "" {
		return fmt.Errorf("browser.service_url is required when browser.enabled=true")
	}
	for _, scheme := range c.BrowserAllowedURLSchemes() {
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("browser.allowed_url_schemes: unsupported scheme %q (v1 only http/https)", scheme)
		}
	}
	return nil
}
