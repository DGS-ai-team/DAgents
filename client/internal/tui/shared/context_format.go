package shared

import (
	"fmt"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

// FormatSessionContext 将 GET context 响应格式化为只读文本（供 /context 或日志）。
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

// FormatSessionSkills 格式化 skills 列表响应。
func FormatSessionSkills(sk *nodeapi.SessionSkills) string {
	if sk == nil {
		return "(无 skills 数据)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "session=%s\n\nloaded:\n", sk.SessionID)
	writeSkillRows(&b, sk.LoadedSkills, "  (none)")
	b.WriteString("\navailable:\n")
	writeSkillRows(&b, sk.AvailableSkills, "  (none)")
	return strings.TrimRight(b.String(), "\n")
}

func writeSkillRows(b *strings.Builder, rows []any, empty string) {
	if len(rows) == 0 {
		b.WriteString(empty + "\n")
		return
	}
	for _, raw := range rows {
		m, ok := raw.(map[string]any)
		if !ok {
			b.WriteString(fmt.Sprintf("  - %v\n", raw))
			continue
		}
		name := strings.TrimSpace(fmt.Sprint(m["skill_name"]))
		if name == "" {
			name = strings.TrimSpace(fmt.Sprint(m["name"]))
		}
		desc := skillDescriptionFromRow(m)
		if desc != "" {
			fmt.Fprintf(b, "  - %s · %s\n", name, desc)
		} else {
			fmt.Fprintf(b, "  - %s\n", name)
		}
	}
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

func wrapLines(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " ")
		for len(line) > width {
			out = append(out, line[:width])
			line = line[width:]
		}
		out = append(out, line)
	}
	return out
}
