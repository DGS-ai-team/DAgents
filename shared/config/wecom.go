package config

import (
	"fmt"
	"net/url"
	"strings"
)

const DefaultWeComAPIBase = "https://qyapi.weixin.qq.com"

// WeComConfig 控制企业微信「消息推送」（群机器人 webhook）内置工具。
type WeComConfig struct {
	// Enabled 为 nil 时默认 false（须显式启用）。
	Enabled *bool `yaml:"enabled"`
	// WebhookURL 完整 webhook 地址（含 key=）；与 WebhookKey 二选一即可。
	WebhookURL string `yaml:"webhook_url"`
	// WebhookKey 消息推送密钥（webhook URL 中的 key 参数）。
	WebhookKey string `yaml:"webhook_key"`
	// APIBase 企业微信 API 基址；空则用官方默认。
	APIBase string `yaml:"api_base"`
}

// WeComEnabled 是否启用企业微信消息推送工具。
func (c *Config) WeComEnabled() bool {
	if c == nil || c.WeCom.Enabled == nil {
		return false
	}
	return *c.WeCom.Enabled
}

// WeComAPIBase 返回 API 基址（无尾斜杠）。
func (c *Config) WeComAPIBase() string {
	if c == nil {
		return DefaultWeComAPIBase
	}
	base := strings.TrimRight(strings.TrimSpace(c.WeCom.APIBase), "/")
	if base == "" {
		return DefaultWeComAPIBase
	}
	return base
}

// WeComWebhookKey 解析生效的 webhook key（优先 webhook_key，其次从 webhook_url 提取）。
func (c *Config) WeComWebhookKey() string {
	if c == nil {
		return ""
	}
	if key := strings.TrimSpace(c.WeCom.WebhookKey); key != "" {
		return key
	}
	return ExtractWeComWebhookKey(c.WeCom.WebhookURL)
}

// ExtractWeComWebhookKey 从完整 webhook URL 或裸 key 字符串中提取 key。
func ExtractWeComWebhookKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") && !strings.Contains(raw, "?") && !strings.Contains(raw, "/") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if key := strings.TrimSpace(u.Query().Get("key")); key != "" {
		return key
	}
	return ""
}

func (c *Config) applyWeComDefaults() {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.WeCom.APIBase) == "" {
		c.WeCom.APIBase = DefaultWeComAPIBase
	}
	// 若仅填了 URL，启动时规范化抽出 key，便于后续校验与工具使用。
	if strings.TrimSpace(c.WeCom.WebhookKey) == "" {
		if key := ExtractWeComWebhookKey(c.WeCom.WebhookURL); key != "" {
			c.WeCom.WebhookKey = key
		}
	}
}

func validateWeComConfig(c *Config) error {
	if c == nil || !c.WeComEnabled() {
		return nil
	}
	if c.WeComWebhookKey() == "" {
		return fmt.Errorf("wecom.webhook_url or wecom.webhook_key is required when wecom.enabled=true")
	}
	base := c.WeComAPIBase()
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("wecom.api_base is invalid")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("wecom.api_base scheme must be http or https")
	}
	return nil
}
