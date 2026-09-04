// Package webui 构造 Web UI URL 并在系统浏览器中打开（F-U1–U3）。
package webui

import (
	"net/url"
	"strings"
)

// AgentURL 构造带 Agent 深链的 Web UI 地址（F-U3）。
// agentID 为 Agent 实例 UUID。
func AgentURL(endpoint, agentID string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/ui/"
	if aid := strings.TrimSpace(agentID); aid != "" {
		return base + "agents/" + url.PathEscape(aid)
	}
	return base
}

// ConsoleURL 返回控制台首页 URL。
func ConsoleURL(endpoint string) string {
	return AgentURL(endpoint, "")
}

// SettingsAboutURL 返回设置 › 关于页 URL（F-N9 / F-X8）。
func SettingsAboutURL(endpoint string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/ui/settings/about"
	return base
}
