package shared

import (
	"fmt"
	"os/user"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/DGS-ai-team/DAgents/client/internal/probe"
)

// FormatLLMThinkingSummary 格式化 thinking 运行时状态（供欢迎区与 /status）。
func FormatLLMThinkingSummary(llm probe.LLMInfo) string {
	if !llm.ThinkingSupported {
		return "—"
	}
	switch strings.ToLower(strings.TrimSpace(llm.Thinking)) {
	case "disabled", "off":
		return "关闭"
	case "enabled", "on":
		effort := strings.TrimSpace(llm.ReasoningEffort)
		if effort == "" {
			effort = "high"
		}
		return "开启 · " + effort
	default:
		return "—"
	}
}

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

func skillsBloatWarningTexts(tokens, maxBody, threshold int) []string {
	if threshold <= 0 {
		threshold = 4000
	}
	display := tokens
	if maxBody > display {
		display = maxBody
	}
	if tokens <= threshold && maxBody <= threshold {
		return nil
	}
	return []string{
		fmt.Sprintf("skills 目录估算约 %d tokens（超过 %d）", display, threshold),
		"skills 过于臃肿，请精简 skill 描述、缩短 SKILL 正文或清理无用的 skills",
	}
}

// SkillsBloatWarningTexts 返回纯文本告警（供 REPL stderr 等）。
func SkillsBloatWarningTexts(ctx *nodeapi.SessionContext) []string {
	if ctx == nil {
		return nil
	}
	return skillsBloatWarningTexts(
		ctx.SkillsCatalogEstimatedTokens,
		ctx.SkillsCatalogMaxBodyEstimatedTokens,
		ctx.SkillsCatalogBloatThreshold,
	)
}

// SkillsBloatWarningLines 根据 GET /context 的 skills 估算 token 生成欢迎区告警行。
func SkillsBloatWarningLines(ctx *nodeapi.SessionContext) []string {
	texts := SkillsBloatWarningTexts(ctx)
	if len(texts) == 0 {
		return nil
	}
	lines := make([]string, 0, len(texts))
	for _, text := range texts {
		lines = append(lines, panelLine(panelKindNote, text))
	}
	return lines
}

// WelcomePanelTitle 返回欢迎面板标题。
func WelcomePanelTitle(clientVersion string) string {
	if clientVersion == "" {
		clientVersion = "—"
	}
	return fmt.Sprintf("DAgents v%s", clientVersion)
}
