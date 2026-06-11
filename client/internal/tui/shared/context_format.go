package shared

import (
	"fmt"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/mattn/go-runewidth"
)

// FormatSessionContextPanelBody 构造 /context 面板正文行（panel 编码）。
func FormatSessionContextPanelBody(ctx *nodeapi.SessionContext) []string {
	if ctx == nil {
		return []string{panelLine(panelKindEmpty, "无 context 数据")}
	}
	turn := orDash(ctx.TurnState)
	if turn == "-" && ctx.HasActiveTurn {
		turn = "active"
	}
	lines := []string{
		panelKV("session", ctx.SessionID),
		panelKV("turn", turn),
		panelKV("phase", orDash(ctx.RunTurnPhase)),
		panelKV("messages", fmt.Sprintf("%d", ctx.MessagesCount)),
		panelKV("pending_tools", fmt.Sprintf("%d", ctx.PendingToolCallsCount)),
		panelKV("tokens", fmt.Sprintf("%d", ctx.MessagesTotalTokens)),
		panelKV("tool_loop", fmt.Sprintf("%d", ctx.ToolLoopCount)),
		panelKV("queue", fmt.Sprintf("%d", ctx.QueuePending)),
		panelLine(panelKindSection, "system_prompt"),
	}
	if strings.TrimSpace(ctx.SystemPrompt) == "" {
		lines = append(lines, panelLine(panelKindPreview, "(none)"))
	} else {
		for _, part := range wrapLines(strings.TrimSpace(ctx.SystemPrompt), 72) {
			lines = append(lines, panelLine(panelKindPreview, part))
		}
	}
	lines = append(lines, panelLine(panelKindSection, "loaded_skills"))
	if len(ctx.LoadedSkills) == 0 {
		lines = append(lines, panelLine(panelKindEmpty, "(none)"))
	} else {
		for _, sk := range ctx.LoadedSkills {
			name := strings.TrimSpace(sk.SkillName)
			if name == "" {
				name = "-"
			}
			desc := strings.TrimSpace(sk.Description)
			lines = append(lines, panelLine(panelKindLoaded, name, desc))
		}
	}
	lines = append(lines, panelLine(panelKindSection, "recent_messages"))
	if len(ctx.RecentMessages) == 0 {
		lines = append(lines, panelLine(panelKindEmpty, "(none)"))
	} else {
		for i, msg := range ctx.RecentMessages {
			role := strings.TrimSpace(msg.Role)
			if role == "" {
				role = "unknown"
			}
			meta := fmt.Sprintf("%d. [%s]", i+1, role)
			if msg.ToolCallsCount > 0 {
				meta += fmt.Sprintf(" tool_calls=%d", msg.ToolCallsCount)
			}
			lines = append(lines, panelLine(panelKindPreview, meta))
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				content = "(empty)"
			}
			for _, part := range wrapLines(content, 72) {
				lines = append(lines, panelLine(panelKindPreview, "   "+part))
			}
		}
	}
	return lines
}

// FormatSessionContextPanel 将 context 格式化为带 ANSI 的面板文本（供全屏 viewport）。
func FormatSessionContextPanel(ctx *nodeapi.SessionContext) string {
	title := FormatTranscriptLineForDisplay(sysPanelTitlePrefix+"Session Context", 0)
	body := FormatSessionContextPanelBody(ctx)
	lines := []string{title}
	for _, line := range body {
		lines = append(lines, FormatTranscriptLineForDisplay(sysPanelBodyPrefix+line, 0))
	}
	return strings.Join(lines, "\n")
}

// FormatSessionContext 将 GET context 响应格式化为只读文本（供 REPL 或日志）。
func FormatSessionContext(ctx *nodeapi.SessionContext) string {
	if ctx == nil {
		return "(无 context 数据)"
	}
	lines := []string{
		"Context",
		"",
		fmt.Sprintf("session_id: %s", ctx.SessionID),
		fmt.Sprintf("turn_state: %s", orDash(ctx.TurnState)),
		fmt.Sprintf("run_turn_phase: %s", orDash(ctx.RunTurnPhase)),
		fmt.Sprintf("messages_count: %d", ctx.MessagesCount),
		fmt.Sprintf("pending_tool_calls_count: %d", ctx.PendingToolCallsCount),
		fmt.Sprintf("messages_total_tokens: %d", ctx.MessagesTotalTokens),
		fmt.Sprintf("tool_loop_count: %d", ctx.ToolLoopCount),
		fmt.Sprintf("queue_pending: %d", ctx.QueuePending),
		fmt.Sprintf("has_active_turn: %t", ctx.HasActiveTurn),
		"",
		"system_prompt:",
	}
	if strings.TrimSpace(ctx.SystemPrompt) == "" {
		lines = append(lines, "  (none)")
	} else {
		for _, part := range wrapLines(strings.TrimSpace(ctx.SystemPrompt), 72) {
			lines = append(lines, "  "+part)
		}
	}
	lines = append(lines, "", "loaded_skills:")
	if len(ctx.LoadedSkills) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for _, sk := range ctx.LoadedSkills {
			name := strings.TrimSpace(sk.SkillName)
			if name == "" {
				name = "-"
			}
			desc := strings.TrimSpace(sk.Description)
			if desc != "" {
				lines = append(lines, fmt.Sprintf("  - %s · %s", name, desc))
			} else {
				lines = append(lines, "  - "+name)
			}
		}
	}
	lines = append(lines, "", "recent_messages:")
	if len(ctx.RecentMessages) == 0 {
		lines = append(lines, "  (none)")
	} else {
		for i, msg := range ctx.RecentMessages {
			role := strings.TrimSpace(msg.Role)
			if role == "" {
				role = "unknown"
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				content = "(empty)"
			}
			meta := ""
			if msg.ToolCallsCount > 0 {
				meta += fmt.Sprintf(" tool_calls=%d", msg.ToolCallsCount)
			}
			if msg.HasReasoningContent {
				meta += " reasoning=yes"
			}
			if msg.ToolCallID != "" {
				meta += " tool_call_id=" + msg.ToolCallID
			}
			lines = append(lines, fmt.Sprintf("  %d. [%s]%s", i+1, role, meta))
			for _, part := range wrapLines(content, 72) {
				lines = append(lines, "     "+part)
			}
		}
	}
	lines = append(lines, "", "Esc 返回聊天记录")
	return strings.Join(lines, "\n")
}

func skillDescriptionFromRow(m map[string]any) string {
	if m == nil {
		return ""
	}
	raw, ok := m["description"]
	if !ok || raw == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(raw))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return strings.TrimSpace(s)
}

// wrapLines 按终端显示宽度折行；避免按字节截断破坏 UTF-8（中文等尾端乱码）。
func wrapLines(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " ")
		rest := line
		for runewidth.StringWidth(rest) > width {
			chunk := runewidth.Truncate(rest, width, "")
			if chunk == "" {
				break
			}
			out = append(out, chunk)
			rest = rest[len(chunk):]
		}
		out = append(out, rest)
	}
	return out
}
