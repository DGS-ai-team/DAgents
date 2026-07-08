// Package webui 构造 Web UI URL 并在系统浏览器中打开（F-U1–U3）。
package webui

import (
	"net/url"
	"strings"
)

// SessionURL 构造带 session 深链的 Web UI 地址。
func SessionURL(endpoint, sessionID string, focusHitl bool) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/ui/"
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	q := u.Query()
	if sid := strings.TrimSpace(sessionID); sid != "" {
		q.Set("session", sid)
	}
	if focusHitl {
		q.Set("focus", "hitl")
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ConsoleURL 返回控制台首页 URL。
func ConsoleURL(endpoint string) string {
	return SessionURL(endpoint, "", false)
}
