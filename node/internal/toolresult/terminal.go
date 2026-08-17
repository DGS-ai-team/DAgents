package toolresult

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// DefaultTerminalHistoryMaxBytes bounds one terminal output projection written
// to the model history. The live PTY stream and its replay buffer use separate
// limits and are intentionally not affected by this value.
const DefaultTerminalHistoryMaxBytes = 12_000

// IsTerminalOutputTool reports whether a tool result contains a terminal byte
// stream that needs cursor metadata preserved while its output is compacted.
func IsTerminalOutputTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "terminal_read", "terminal_terminate":
		return true
	default:
		return false
	}
}

// TerminalCompressionStats describes the model-facing projection. These
// fields are also embedded in the returned JSON so the model can understand
// that output was intentionally shortened without parsing a side channel.
type TerminalCompressionStats struct {
	Mode         string
	RawBytes     int
	KeptBytes    int
	OmittedBytes int
	LineCount    int
}

// PackageTerminal returns a readable projection for the client/UI and makes a
// bounded, deterministic projection for model history. Terminal control
// sequences are removed from both projections; the live PTY/WebSocket path is
// kept separate and still receives the original bytes for xterm to interpret.
// It only replaces the output field; terminal_id, next_seq, replay_gap, exited
// and exit remain available so a subsequent terminal_read can continue legally.
//
// Invalid or non-terminal-shaped JSON is passed through unchanged. This keeps
// the hook fail-open and avoids turning a display optimization into a tool
// execution failure.
func PackageTerminal(raw string, maxBytes int) (client, history string, stats TerminalCompressionStats) {
	client = raw
	history = raw
	if maxBytes <= 0 {
		maxBytes = DefaultTerminalHistoryMaxBytes
	}

	var payload map[string]any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil || payload == nil {
		return client, history, stats
	}
	output, ok := payload["output"].(string)
	if !ok {
		return client, history, stats
	}

	normalized, lineCount := normalizeTerminalText(output)
	payload["output"] = normalized
	clientBytes, err := json.Marshal(payload)
	if err != nil {
		return client, history, stats
	}
	client = string(clientBytes)
	history = client
	if len([]byte(output)) <= maxBytes {
		return client, history, stats
	}

	compact := compactTerminalText(normalized, maxBytes)
	stats = TerminalCompressionStats{
		Mode:         "compressed",
		RawBytes:     len([]byte(output)),
		KeptBytes:    len([]byte(compact)),
		OmittedBytes: len([]byte(output)) - len([]byte(compact)),
		LineCount:    lineCount,
	}
	if stats.OmittedBytes < 0 {
		stats.OmittedBytes = 0
	}
	payload["output"] = compact
	payload["output_mode"] = stats.Mode
	payload["raw_bytes"] = stats.RawBytes
	payload["kept_bytes"] = stats.KeptBytes
	payload["omitted_bytes"] = stats.OmittedBytes
	payload["line_count"] = stats.LineCount

	encoded, err := json.Marshal(payload)
	if err != nil {
		return client, raw, TerminalCompressionStats{}
	}
	return client, string(encoded), stats
}

func normalizeTerminalText(text string) (string, int) {
	lineCount := 1
	if text != "" {
		lineCount += strings.Count(text, "\n")
	}

	text = stripANSI(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if index := strings.LastIndexByte(line, '\b'); index >= 0 {
			line = removeBackspaces(line)
		}
		lines[i] = strings.Map(func(r rune) rune {
			switch {
			case r == '\n' || r == '\t':
				return r
			case r < 0x20 || r == 0x7f:
				return -1
			default:
				return r
			}
		}, line)
	}

	collapsed := make([]string, 0, len(lines))
	for index := 0; index < len(lines); {
		line := lines[index]
		end := index + 1
		for end < len(lines) && lines[end] == line {
			end++
		}
		count := end - index
		if count >= 3 && strings.TrimSpace(line) != "" {
			collapsed = append(collapsed, fmt.Sprintf("%s [repeated %d times]", line, count))
		} else {
			collapsed = append(collapsed, lines[index:end]...)
		}
		index = end
	}
	return strings.Join(collapsed, "\n"), lineCount
}

func removeBackspaces(text string) string {
	runes := make([]rune, 0, len(text))
	for _, r := range text {
		if r == '\b' {
			if len(runes) > 0 {
				runes = runes[:len(runes)-1]
			}
			continue
		}
		runes = append(runes, r)
	}
	return string(runes)
}

// stripANSI removes CSI/OSC and simple ESC sequences from the model view.
// The original bytes remain available to the terminal UI/replay path.
func stripANSI(text string) string {
	var b strings.Builder
	for index := 0; index < len(text); {
		if text[index] != 0x1b {
			b.WriteByte(text[index])
			index++
			continue
		}
		index++
		if index >= len(text) {
			break
		}
		switch text[index] {
		case '[':
			index++
			for index < len(text) {
				current := text[index]
				index++
				if current >= 0x40 && current <= 0x7e {
					break
				}
			}
		case ']':
			index++
			for index < len(text) {
				if text[index] == 0x07 {
					index++
					break
				}
				if text[index] == 0x1b && index+1 < len(text) && text[index+1] == '\\' {
					index += 2
					break
				}
				index++
			}
		default:
			index++
		}
	}
	return b.String()
}

func compactTerminalText(text string, maxBytes int) string {
	if len([]byte(text)) <= maxBytes {
		return text
	}
	marker := "\n[… terminal output omitted …]\n"
	if maxBytes <= len([]byte(marker))+64 {
		return takePrefixBytes(text, maxBytes)
	}

	budget := maxBytes - len([]byte(marker))
	headBudget := budget / 4
	tailBudget := budget / 2
	middleBudget := budget - headBudget - tailBudget
	head := takePrefixBytes(text, headBudget)
	tail := takeSuffixBytes(text, tailBudget)
	middle := importantLines(text, middleBudget)
	parts := make([]string, 0, 5)
	if strings.TrimSpace(head) != "" {
		parts = append(parts, head)
	}
	parts = append(parts, marker)
	if strings.TrimSpace(middle) != "" {
		parts = append(parts, middle)
	}
	if strings.TrimSpace(tail) != "" {
		parts = append(parts, tail)
	}
	result := strings.Join(parts, "")
	if len([]byte(result)) > maxBytes {
		result = takePrefixBytes(result, maxBytes)
	}
	return result
}

func importantLines(text string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	keywords := []string{
		"ERROR", "ERR ", "WARN", "WARNING", "FAIL", "FAILED", "EXCEPTION",
		"TRACEBACK", "PANIC", "FATAL", "DENIED", "NOT FOUND", "COMMAND NOT FOUND",
		"EXIT CODE", "SEGMENTATION FAULT", "TIMEOUT", "PASSED", "SUCCESS",
	}
	lines := strings.Split(text, "\n")
	selected := make([]string, 0)
	used := 0
	for _, line := range lines {
		upper := strings.ToUpper(line)
		matched := false
		for _, keyword := range keywords {
			if strings.Contains(upper, keyword) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		candidate := line + "\n"
		if used+len([]byte(candidate)) > maxBytes {
			break
		}
		selected = append(selected, candidate)
		used += len([]byte(candidate))
	}
	return strings.Join(selected, "")
}

func takePrefixBytes(text string, maxBytes int) string {
	if maxBytes <= 0 || text == "" {
		return ""
	}
	if len([]byte(text)) <= maxBytes {
		return text
	}
	index := maxBytes
	for index > 0 && !utf8.RuneStart(text[index]) {
		index--
	}
	return strings.TrimRight(text[:index], "\r\n")
}

func takeSuffixBytes(text string, maxBytes int) string {
	if maxBytes <= 0 || text == "" {
		return ""
	}
	if len([]byte(text)) <= maxBytes {
		return text
	}
	index := len(text) - maxBytes
	for index < len(text) && !utf8.RuneStart(text[index]) {
		index++
	}
	return strings.TrimLeft(text[index:], "\r\n")
}
