package shared

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

const usageStorageSep = "\x1e"

// SplitStoredUsage 从 transcript 存储串分离正文与 plain usage 后缀。
func SplitStoredUsage(body string) (content, usagePlain string) {
	i := strings.LastIndex(body, usageStorageSep)
	if i < 0 {
		return body, ""
	}
	return body[:i], body[i+len(usageStorageSep):]
}

// sanitizeTerminalText 清理不可见控制符，避免 Windows TUI 将 \t、\x1e 等渲染为方框压住正文。
//
// 保留 \n / \r；\t 展开为空格；其余 C0 控制符（含 usage 存储分隔符 \x1e）丢弃。
func sanitizeTerminalText(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '\n', '\r':
			b.WriteRune(r)
		case '\t':
			b.WriteString("    ")
		default:
			if r < 0x20 || r == 0x7f {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

// FormatTranscriptLineForDisplay 为 viewport 渲染单行：彩色圆点 + usage 右对齐。
func FormatTranscriptLineForDisplay(line string, width int) string {
	if width <= 0 {
		width = 80
	}
	switch {
	case strings.HasPrefix(line, "[assistant] "):
		body := strings.TrimPrefix(line, "[assistant] ")
		content, usage := SplitStoredUsage(body)
		return formatRoleLineWithUsage(roleDotAssistant, sanitizeTerminalText(content), sanitizeTerminalText(usage), width)
	case strings.HasPrefix(line, "[user] "):
		return roleDotUser + sanitizeTerminalText(strings.TrimPrefix(line, "[user] "))
	case strings.HasPrefix(line, "[reasoning] "):
		body := sanitizeTerminalText(strings.TrimPrefix(line, "[reasoning] "))
		return roleDotReasoning + panelDimStyle + body + panelReset
	case strings.HasPrefix(line, toolA2APendingLinePrefix):
		body := ToolA2ALineBody(line, toolA2APendingLinePrefix)
		return roleDotToolA2A + toolA2APendingStyle + sanitizeTerminalText(body) + panelReset
	case strings.HasPrefix(line, toolA2AResultLinePrefix):
		body := ToolA2ALineBody(line, toolA2AResultLinePrefix)
		return roleDotToolA2A + toolA2AResultStyle + sanitizeTerminalText(body) + panelReset
	case strings.HasPrefix(line, toolPendingLinePrefix):
		body := metaLineBody(line, toolPendingLinePrefix)
		return roleDotToolPending + toolPendingStyle + sanitizeTerminalText(body) + panelReset
	case strings.HasPrefix(line, toolCallCodeLinePrefix):
		body := metaLineBody(line, toolCallCodeLinePrefix)
		return toolCodeIndent + toolCodeStyle + sanitizeTerminalText(body) + panelReset
	case strings.HasPrefix(line, toolPreviewLinePrefix):
		body := metaLineBody(line, toolPreviewLinePrefix)
		return toolPreviewIndent + toolPreviewStyle + "└─ " + sanitizeTerminalText(body) + panelReset
	case strings.HasPrefix(line, toolDetailLinePrefix):
		body := metaLineBody(line, toolDetailLinePrefix)
		return toolDetailIndent + toolDimStyle + sanitizeTerminalText(body) + panelReset
	case strings.HasPrefix(line, "[tool]"):
		rest := strings.TrimPrefix(line, "[tool]")
		rest = strings.TrimLeft(rest, " ")
		dot, styled := formatToolResultLine(rest)
		return dot + styled
	case strings.HasPrefix(line, "[status] "):
		body := sanitizeTerminalText(strings.TrimPrefix(line, "[status] "))
		return roleDotStatus + statusActiveStyle + body + panelReset
	case strings.HasPrefix(line, "[system]"):
		return roleDotSystem + sanitizeTerminalText(strings.TrimPrefix(line, "[system]"))
	case strings.HasPrefix(line, sysPanelTitlePrefix):
		title := strings.TrimPrefix(line, sysPanelTitlePrefix)
		return roleDotSystem + panelTitleStyle + title + panelReset
	case strings.HasPrefix(line, sysPanelBodyPrefix):
		body := strings.TrimPrefix(line, sysPanelBodyPrefix)
		return panelBodyIndent + formatPanelBodyLine(sanitizeTerminalText(body))
	default:
		return line
	}
}

const (
	roleDotUser          = "\033[34m●\033[0m "
	roleDotAssistant     = "\033[32m●\033[0m "
	roleDotReasoning     = "\033[90m●\033[0m "
	roleDotTool          = "\033[36m●\033[0m "
	roleDotToolPending   = "\033[33m●\033[0m "
	roleDotToolA2A       = "\033[96;5m●\033[0m "
	roleDotToolSuccess   = "\033[32m●\033[0m "
	roleDotToolFailure   = "\033[31m●\033[0m "
	roleDotSystem        = "\033[90m●\033[0m "
	roleDotStatus        = "\033[33;5m●\033[0m "
	statusActiveStyle    = "\033[33m"

	panelTitleStyle   = "\033[1;36m"
	panelSectionStyle = "\033[36m"
	panelLabelStyle   = "\033[90m"
	panelLoadedStyle  = "\033[32m"
	panelCurrentStyle = "\033[1;33m"
	panelDimStyle     = "\033[90m"
	panelReset        = "\033[0m"

	toolPendingStyle  = "\033[33m"
	toolA2APendingStyle = "\033[96m"
	toolA2AResultStyle  = "\033[96;1m"
	toolPreviewStyle  = "\033[90m"
	toolCodeStyle     = "\033[37m"
	toolDimStyle      = "\033[90m"
	toolPreviewIndent = "  "
	toolDetailIndent  = "     "
	toolCodeIndent    = "  "

	panelBodyIndent = "  "
)

func metaLineBody(line, prefix string) string {
	body := strings.TrimPrefix(line, prefix)
	if i := strings.Index(body, "] "); i >= 0 {
		return body[i+2:]
	}
	return body
}

func formatToolResultLine(rest string) (dot, styled string) {
	rest = sanitizeTerminalText(rest)
	switch {
	case strings.HasPrefix(rest, "✓"):
		return roleDotToolSuccess, rest
	case strings.HasPrefix(rest, "✗"):
		return roleDotToolFailure, rest
	case strings.HasPrefix(rest, "▶"):
		return roleDotToolPending, toolPendingStyle + rest + panelReset
	default:
		return roleDotTool, rest
	}
}

func formatRoleLineWithUsage(prefix, body, usagePlain string, width int) string {
	if usagePlain == "" {
		return prefix + body
	}
	styledUsage := StyleInlineUsage(usagePlain)
	body = strings.TrimRight(body, "\n\r")
	contentWidth := width - runewidth.StringWidth(stripANSI(prefix))
	if contentWidth < 12 {
		contentWidth = 12
	}

	lines := strings.Split(body, "\n")
	lastLine := lines[len(lines)-1]
	head := strings.Join(lines[:len(lines)-1], "\n")

	var out strings.Builder
	out.WriteString(prefix)
	if head != "" {
		out.WriteString(head)
		out.WriteByte('\n')
		out.WriteString(prefixDotPadding(prefix))
	}
	out.WriteString(lastLine)
	// usage 独占一行右对齐，避免 viewport / Rich fold 在 usage 字符串中间硬折行。
	out.WriteByte('\n')
	out.WriteString(prefixDotPadding(prefix))
	usageW := runewidth.StringWidth(usagePlain)
	pad := contentWidth - usageW
	if pad < 0 {
		pad = 0
	}
	out.WriteString(strings.Repeat(" ", pad))
	out.WriteString(styledUsage)
	return out.String()
}

func prefixDotPadding(prefix string) string {
	// 续行与圆点列对齐（圆点占 2 列：● + 空格）。
	if strings.HasPrefix(prefix, roleDotAssistant) {
		return "  "
	}
	return strings.Repeat(" ", runewidth.StringWidth(stripANSI(prefix)))
}

func formatPanelBodyLine(encoded string) string {
	kind, rest, ok := strings.Cut(encoded, "|")
	if !ok {
		return panelDimStyle + encoded + panelReset
	}
	switch kind {
	case panelKindSection:
		return panelSectionStyle + rest + panelReset
	case panelKindKV:
		label, value, _ := strings.Cut(rest, "|")
		return formatPanelKV(label, value)
	case panelKindLoaded:
		name, desc, _ := strings.Cut(rest, "|")
		line := panelLoadedStyle + "● " + name + panelReset
		if desc != "" {
			line += panelDimStyle + " · " + desc + panelReset
		}
		return line
	case panelKindAvailable:
		name, desc, _ := strings.Cut(rest, "|")
		line := panelDimStyle + "○ " + name + panelReset
		if desc != "" {
			line += panelDimStyle + " · " + desc + panelReset
		}
		return line
	case panelKindSessCur:
		id, state, meta := splitPanelTriple(rest)
		return panelCurrentStyle + "* " + id + panelReset +
			panelDimStyle + "  [" + state + "]  " + meta + panelReset
	case panelKindSess:
		id, state, meta := splitPanelTriple(rest)
		return panelDimStyle + "- " + id + "  [" + state + "]  " + meta + panelReset
	case panelKindPreview:
		return panelDimStyle + "    " + rest + panelReset
	case panelKindEmpty:
		return panelDimStyle + "  " + rest + panelReset
	case panelKindNote:
		return panelSectionStyle + rest + panelReset
	case panelKindChild:
		return panelLoadedStyle + rest + panelReset
	case panelKindDetail:
		return panelDimStyle + "   " + rest + panelReset
	case panelKindHelp:
		cmd, desc, _ := strings.Cut(rest, "|")
		return panelLabelStyle + cmd + panelReset + panelDimStyle + "  " + desc + panelReset
	default:
		return panelDimStyle + encoded + panelReset
	}
}

func formatPanelKV(label, value string) string {
	padded := label
	if len(label) < 10 {
		padded = label + strings.Repeat(" ", 10-len(label))
	}
	return panelLabelStyle + padded + panelReset + "  " + value
}

func stripANSI(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	inESC := false
	for i := 0; i < len(s); i++ {
		if s[i] == '\x1b' {
			inESC = true
			continue
		}
		if inESC {
			if s[i] == 'm' {
				inESC = false
			}
			continue
		}
		out.WriteByte(s[i])
	}
	return out.String()
}
