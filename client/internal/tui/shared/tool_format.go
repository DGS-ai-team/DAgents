package shared

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	toolPreviewMaxRunes = 240
	toolDetailMaxLines  = 8
)

// NormalizedToolCall 为 UI 使用的 tool call 扁平结构。
type NormalizedToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// FormatToolEvent 将 tool_call / tool_result SSE 格式化为用户可读的多行文本。
func FormatToolEvent(eventType string, data map[string]any, verbose bool) []string {
	switch eventType {
	case "tool_call":
		return formatToolCallEvent(data, verbose)
	case "tool_result":
		return formatToolResultEvent(data, verbose)
	default:
		return []string{fmt.Sprintf("[tool] %s", eventType)}
	}
}

func formatToolCallEvent(data map[string]any, verbose bool) []string {
	calls := extractToolCalls(data)
	if len(calls) == 0 {
		if verbose {
			return []string{fmt.Sprintf("[tool] 调用 (原始) %v", data)}
		}
		return []string{"[tool] 调用 (无 tool_calls 详情)"}
	}
	lines := make([]string, 0, len(calls))
	for _, call := range calls {
		title := ToolDisplayName(call.Name, call.Arguments)
		lines = append(lines, "[tool] ▶ 调用 "+title)
		if verbose {
			if detail := formatArgsDetail(call.Arguments, call.RawJSON); detail != "" {
				lines = append(lines, indentLines("    ", detail)...)
			}
		}
	}
	return lines
}

func formatToolResultEvent(data map[string]any, verbose bool) []string {
	name := trimDisplayField(data["tool_name"])
	if name == "" {
		name = trimDisplayField(data["name"])
	}
	if name == "" {
		name = "tool"
	}
	rejected, _ := data["rejected"].(bool)
	content := strings.TrimSpace(trimDisplayField(data["content"]))
	if content == "" {
		content = strings.TrimSpace(trimDisplayField(data["output"]))
	}

	head := resultHeadline(name, rejected, content)
	lines := []string{"[tool] " + head}
	if content == "" {
		return lines
	}
	if verbose {
		lines = append(lines, indentLines("    ", content)...)
		return lines
	}
	preview := summarizeToolResultContent(name, content)
	if preview != "" {
		lines = append(lines, indentLines("    ", preview)...)
	}
	return lines
}

func resultHeadline(name string, rejected bool, content string) string {
	if rejected {
		return "✗ " + name + " 已拒绝"
	}
	switch {
	case strings.HasPrefix(content, "[BASH_RESULT]"):
		exit := parseBashExitCode(content)
		if exit == 0 {
			return "✓ " + name + " 完成 (exit=0)"
		}
		return fmt.Sprintf("✗ %s 失败 (exit=%d)", name, exit)
	case strings.HasPrefix(content, "[TOOL_BACKGROUND_DONE]"):
		return "✓ " + name + " 后台任务结束"
	case strings.HasPrefix(content, "ERROR:"):
		return "✗ " + name + " 错误"
	case strings.HasPrefix(content, `{`) || strings.HasPrefix(content, `[`):
		if hint := summarizeJSONResult(content); hint != "" {
			return "✓ " + name + " · " + hint
		}
	}
	return "✓ " + name + " 完成"
}

func summarizeToolResultContent(name, content string) string {
	switch {
	case strings.HasPrefix(content, "[BASH_RESULT]"):
		return summarizeBashOutput(content)
	case strings.HasPrefix(content, "[TOOL_BACKGROUND_DONE]"):
		if idx := strings.Index(content, "---\n"); idx >= 0 {
			return truncatePreview(strings.TrimSpace(content[idx+4:]), toolPreviewMaxRunes)
		}
		return truncatePreview(content, toolPreviewMaxRunes)
	case strings.HasPrefix(content, "ERROR:"):
		return truncatePreview(content, toolPreviewMaxRunes)
	default:
		if hint := summarizeJSONResult(content); hint != "" && !strings.Contains(content, "\n") {
			return hint
		}
		return truncatePreviewLines(content, toolDetailMaxLines, toolPreviewMaxRunes)
	}
}

func summarizeBashOutput(content string) string {
	if stdout := extractSection(content, "--- STDOUT ---", "--- STDERR ---"); stdout != "" {
		return truncatePreviewLines(stdout, toolDetailMaxLines, toolPreviewMaxRunes)
	}
	if stderr := extractSection(content, "--- STDERR ---", ""); stderr != "" {
		return truncatePreviewLines(stderr, toolDetailMaxLines, toolPreviewMaxRunes)
	}
	// 仅元数据时不再重复 cwd/timeout 噪音。
	return ""
}

func parseBashExitCode(content string) int {
	first := strings.Split(content, "\n")[0]
	if !strings.Contains(first, "exit_code=") {
		return -1
	}
	var code int
	if _, err := fmt.Sscanf(first, "[BASH_RESULT] shell_type=%*s status=%*s exit_code=%d", &code); err == nil {
		return code
	}
	parts := strings.Split(first, "exit_code=")
	if len(parts) < 2 {
		return -1
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &code); err == nil {
		return code
	}
	return -1
}

func extractSection(content, startMark, endMark string) string {
	start := strings.Index(content, startMark)
	if start < 0 {
		return ""
	}
	body := content[start+len(startMark):]
	if endMark != "" {
		if end := strings.Index(body, endMark); end >= 0 {
			body = body[:end]
		}
	}
	return strings.TrimSpace(body)
}

func summarizeJSONResult(content string) string {
	var v any
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		return ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return truncatePreview(content, 80)
	}
	if okVal, exists := m["ok"]; exists {
		if b, ok := okVal.(bool); ok && !b {
			if msg := trimDisplayField(m["error"]); msg != "" {
				return "失败: " + msg
			}
			return "失败"
		}
	}
	if id := firstNonEmptyField(m, "trigger_id", "id", "job_id"); id != "" {
		return "id=" + id
	}
	if msg := firstNonEmptyField(m, "message", "summary"); msg != "" {
		return truncatePreview(msg, 80)
	}
	return ""
}

func firstNonEmptyField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v := trimDisplayField(m[key]); v != "" {
			return v
		}
	}
	return ""
}

type parsedToolCall struct {
	NormalizedToolCall
	RawJSON string
}

func extractToolCalls(data map[string]any) []parsedToolCall {
	rawCalls, ok := data["tool_calls"].([]any)
	if !ok || len(rawCalls) == 0 {
		if name := trimDisplayField(data["name"]); name != "" || trimDisplayField(data["tool_name"]) != "" {
			n := NormalizeToolCallItem(data)
			return []parsedToolCall{{NormalizedToolCall: n, RawJSON: rawArgumentsJSON(data)}}
		}
		return nil
	}
	out := make([]parsedToolCall, 0, len(rawCalls))
	for _, raw := range rawCalls {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		n := NormalizeToolCallItem(m)
		if n.ID == "" && n.Name == "unknown" {
			continue
		}
		out = append(out, parsedToolCall{NormalizedToolCall: n, RawJSON: rawArgumentsJSON(m)})
	}
	return out
}

// NormalizeToolCallItem 将 SSE tool_call 项规范为 {id,name,arguments}。
func NormalizeToolCallItem(item map[string]any) NormalizedToolCall {
	if item == nil {
		return NormalizedToolCall{Name: "unknown", Arguments: map[string]any{}}
	}
	callID := trimDisplayField(item["id"])
	name := trimDisplayField(item["name"])
	argsRaw := item["arguments"]
	if fn, ok := item["function"].(map[string]any); ok {
		if name == "" {
			name = trimDisplayField(fn["name"])
		}
		if argsRaw == nil || trimDisplayField(argsRaw) == "" {
			argsRaw = fn["arguments"]
		}
	}
	args := parseToolArguments(argsRaw)
	if name == "" {
		name = "unknown"
	}
	return NormalizedToolCall{ID: callID, Name: name, Arguments: args}
}

func parseToolArguments(raw any) map[string]any {
	switch v := raw.(type) {
	case map[string]any:
		return v
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return map[string]any{}
		}
		var parsed any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return map[string]any{}
		}
		if m, ok := parsed.(map[string]any); ok {
			return m
		}
		return map[string]any{}
	default:
		return map[string]any{}
	}
}

// ToolDisplayName 生成工具调用短标题（对齐 Python `_tool_display_name`）。
func ToolDisplayName(name string, args map[string]any) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unknown"
	}
	switch name {
	case "bash_run":
		cmd := trimDisplayField(args["command"])
		if cmd == "" {
			return "bash(—)"
		}
		if len([]rune(cmd)) > 48 {
			cmd = string([]rune(cmd)[:47]) + "…"
		}
		return "bash(" + cmd + ")"
	case "trigger_create", "trigger_update", "trigger_delete", "trigger_fire":
		label := firstNonEmptyField(args, "name", "id")
		if label == "" {
			label = "—"
		}
		return name + "(" + label + ")"
	case "write_file", "read_file", "search_replace":
		path := firstNonEmptyField(args, "path", "file_path")
		if path == "" {
			path = "—"
		}
		return name + "(" + path + ")"
	default:
		if len(args) == 0 {
			return name + "()"
		}
		parts := make([]string, 0, len(args))
		for key, value := range args {
			text := truncatePreview(fmt.Sprint(value), 40)
			parts = append(parts, key+"="+text)
		}
		return name + "(" + strings.Join(parts, ", ") + ")"
	}
}

func rawArgumentsJSON(data map[string]any) string {
	if raw := trimDisplayField(data["raw_arguments"]); raw != "" {
		return raw
	}
	if args, ok := data["arguments"].(map[string]any); ok && len(args) > 0 {
		b, err := json.Marshal(args)
		if err == nil {
			return string(b)
		}
	}
	if fn, ok := data["function"].(map[string]any); ok {
		return trimDisplayField(fn["arguments"])
	}
	return trimDisplayField(data["arguments"])
}

func formatArgsDetail(args map[string]any, rawJSON string) string {
	if rawJSON != "" {
		var v any
		if err := json.Unmarshal([]byte(rawJSON), &v); err == nil {
			if b, err := json.MarshalIndent(v, "", "  "); err == nil {
				return string(b)
			}
		}
	}
	if len(args) == 0 {
		return ""
	}
	b, err := json.MarshalIndent(args, "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}

func indentLines(prefix, block string) []string {
	lines := strings.Split(block, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, prefix+line)
	}
	return out
}

func truncatePreview(text string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "…"
}

func truncatePreviewLines(text string, maxLines, maxRunes int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "…")
	}
	joined := strings.Join(lines, "\n")
	return truncatePreview(joined, maxRunes)
}
