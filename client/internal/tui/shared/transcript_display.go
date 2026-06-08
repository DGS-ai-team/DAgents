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

// FormatTranscriptLineForDisplay 为 viewport 渲染单行：彩色圆点 + usage 右对齐。
func FormatTranscriptLineForDisplay(line string, width int) string {
	if width <= 0 {
		width = 80
	}
	switch {
	case strings.HasPrefix(line, "[assistant] "):
		body := strings.TrimPrefix(line, "[assistant] ")
		content, usage := SplitStoredUsage(body)
		return formatRoleLineWithUsage(roleDotAssistant, content, usage, width)
	case strings.HasPrefix(line, "[user] "):
		return roleDotUser + strings.TrimPrefix(line, "[user] ")
	case strings.HasPrefix(line, "[reasoning] "):
		return roleDotReasoning + strings.TrimPrefix(line, "[reasoning] ")
	case strings.HasPrefix(line, "[tool]"):
		rest := strings.TrimPrefix(line, "[tool]")
		rest = strings.TrimLeft(rest, " ")
		return roleDotTool + rest
	case strings.HasPrefix(line, "[system]"):
		return roleDotSystem + strings.TrimPrefix(line, "[system]")
	default:
		return line
	}
}

const (
	roleDotUser       = "\033[34m●\033[0m "
	roleDotAssistant  = "\033[32m●\033[0m "
	roleDotReasoning  = "\033[33m●\033[0m "
	roleDotTool       = "\033[36m●\033[0m "
	roleDotSystem     = "\033[90m●\033[0m "
)

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

	lastW := runewidth.StringWidth(lastLine)
	usageW := runewidth.StringWidth(usagePlain)

	var out strings.Builder
	out.WriteString(prefix)
	if head != "" {
		out.WriteString(head)
		out.WriteByte('\n')
		out.WriteString(prefixDotPadding(prefix))
	}

	if lastW+usageW <= contentWidth {
		pad := contentWidth - lastW - usageW
		if pad < 1 {
			pad = 1
		}
		out.WriteString(lastLine)
		out.WriteString(strings.Repeat(" ", pad))
		out.WriteString(styledUsage)
		return out.String()
	}

	out.WriteString(lastLine)
	out.WriteByte('\n')
	out.WriteString(prefixDotPadding(prefix))
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
