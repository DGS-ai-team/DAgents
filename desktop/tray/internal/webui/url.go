// Package webui 构造 Web UI URL 并在系统浏览器中打开（F-U1–U3）。
package webui

import (
	"net/url"
	"strings"
)

// SessionURL 构造带 session 深链的 Web UI 地址（F-U3）。
func SessionURL(endpoint, sessionID string) string {
	base := strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/ui/"
	u, err := url.Parse(base)
	if err != nil {
		return base
	}
	if sid := strings.TrimSpace(sessionID); sid != "" {
		q := u.Query()
		q.Set("session", sid)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// ConsoleURL 返回控制台首页 URL。
func ConsoleURL(endpoint string) string {
	return SessionURL(endpoint, "")
}
