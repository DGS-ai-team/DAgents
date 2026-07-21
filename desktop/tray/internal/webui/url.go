// Package webui 构造 Web UI URL 并在系统浏览器中打开（F-U1–U3）。
package webui

import (
	"net/url"
	"strings"
)

// AgentURL 构造带 Agent 深链的 Web UI 地址（F-U3）。
// agentID 为 Agent 实例 UUID（与历史 session_id 同源）。
func AgentURL(endpoint, agentID string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/ui/"
	if aid := strings.TrimSpace(agentID); aid != "" {
		return base + "agents/" + url.PathEscape(aid)
	}
	return base
}

// SessionURL 为 AgentURL 的历史别名（托盘 pending 键仍称 session_id）。
func SessionURL(endpoint, sessionID string) string {
	return AgentURL(endpoint, sessionID)
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
