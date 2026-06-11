package shared

import (
	"fmt"
	"os/user"
)

// FormatWelcomePanelBody 构造启动欢迎面板正文（对齐 Python welcome_panel 信息密度）。
func FormatWelcomePanelBody(endpoint, agentID, clientVersion, sessionID string) []string {
	username := "user"
	if u, err := user.Current(); err == nil && u.Username != "" {
		username = u.Username
	}
	return []string{
		panelKV("用户", username),
		panelKV("backend", orDash(endpoint)),
		panelKV("agent", orDash(agentID)),
		panelKV("client", orDash(clientVersion)),
		panelKV("session", orDash(sessionID)),
		panelLine(panelKindNote, "Enter 发送 · /help 命令 · /context 上下文 · Esc 取消 turn"),
		panelLine(panelKindNote, "风险提示：工具可读写本机文件或执行命令；勿粘贴密钥等敏感信息。"),
	}
}

// WelcomePanelTitle 返回欢迎面板标题。
func WelcomePanelTitle(clientVersion string) string {
	if clientVersion == "" {
		clientVersion = "—"
	}
	return fmt.Sprintf("DAgents v%s", clientVersion)
}
