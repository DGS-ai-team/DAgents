package shared

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	toolPreviewMaxRunes       = 240
	toolDetailMaxLines        = 8
	toolDetailLinePrefix      = "[tool-detail|"
	toolPreviewLinePrefix     = "[tool-preview|"
	toolPendingLinePrefix     = "[tool-pending|"
	toolCallCodeLinePrefix    = "[tool-call-code|"
	bashInlineCommandMaxRunes = 36
	UserInformationToolName   = "ask_user_information"
	userInformationDisplayName = "Agent 询问"
	// CallPurposeKey 与 Node tools.CallPurposeKey 对齐，供 Client 展示工具调用首行。
	CallPurposeKey = "call_purpose"
)

// NormalizedToolCall 为 UI 使用的 tool call 扁平结构。
type NormalizedToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

// ToolEventID 从 SSE data 提取 tool 块 ID。
func ToolEventID(data map[string]any) string {
	for _, k := range []string{"tool_call_id", "call_id", "id"} {
		if v := trimDisplayField(data[k]); v != "" {
			return v
		}
	}
	return ""
}

// IsToolDetailLine 是否为可折叠 tool 详情行。
func IsToolDetailLine(line string) bool {
	return strings.HasPrefix(line, toolDetailLinePrefix)
}

// IsToolPreviewLine 是否为 tool 折叠预览行。
func IsToolPreviewLine(line string) bool {
	return strings.HasPrefix(line, toolPreviewLinePrefix)
}

// IsToolCallCodeLine 是否为 tool_call 执行中代码预览行。
func IsToolCallCodeLine(line string) bool {
	return strings.HasPrefix(line, toolCallCodeLinePrefix)
}

// ToolBlockIDFromMetaLine 从 preview/detail/pending 行解析块 ID。
func ToolBlockIDFromMetaLine(line string) string {
	for _, prefix := range []string{toolDetailLinePrefix, toolPreviewLinePrefix, toolPendingLinePrefix, toolCallCodeLinePrefix} {
		if strings.HasPrefix(line, prefix) {
			rest := strings.TrimPrefix(line, prefix)
			id, _, ok := strings.Cut(rest, "]")
			if ok {
				return strings.TrimSpace(id)
			}
		}
	}
	return ""
}

func formatToolMetaLine(prefix, id, body string) string {
	return prefix + id + "] " + body
}

// FormatToolEvent 将 tool_call / tool_result SSE 格式化为用户可读的多行文本。
func FormatToolEvent(eventType string, data map[string]any, verbose bool) []string {
	id := ToolEventID(data)
	return FormatToolEventWithID(eventType, data, id, verbose)
}

// RegisterToolCallsFromEvent 将 tool_call SSE 中的调用登记为 pending（供耗时展示）。
func RegisterToolCallsFromEvent(data map[string]any, pending *ToolPendingTracker) {
	if pending == nil {
		return
	}
	for _, call := range extractToolCalls(data) {
		if call.Name == UserInformationToolName {
			continue
		}
		id := strings.TrimSpace(call.ID)
		if id == "" {
			continue
		}
		pending.Register(id, ToolDisplayName(call.Name, call.Arguments))
	}
}

// FormatToolElapsed 将工具执行秒数格式化为可读耗时（对齐 Python `_format_tool_elapsed`）。
func FormatToolElapsed(elapsed float64) string {
	safe := elapsed
	if safe < 0 {
		safe = 0
	}
	if safe < 1.0 {
		return fmt.Sprintf("%.0fms", safe*1000)
	}
	if safe < 60.0 {
		return fmt.Sprintf("%.1fs", safe)
	}
	minutes := int(safe / 60)
	seconds := safe - float64(minutes*60)
	return fmt.Sprintf("%dm%.0fs", minutes, seconds)
}

// FormatToolEventWithID 带块 ID 的 tool 事件格式化（详情行供展开过滤）。
// elapsedSec 可选：tool_result 时追加标题耗时（秒）；省略或负值时不展示。
func FormatToolEventWithID(eventType string, data map[string]any, id string, verbose bool, elapsedSec ...float64) []string {
	var elapsed float64 = -1
	if len(elapsedSec) > 0 {
		elapsed = elapsedSec[0]
	}
	switch eventType {
	case "tool_call":
		return formatToolCallEvent(data, id, verbose)
	case "tool_result":
		return formatToolResultEvent(data, id, verbose, elapsed)
	default:
		return []string{fmt.Sprintf("[tool] %s", eventType)}
	}
}

func formatToolCallEvent(data map[string]any, blockID string, verbose bool) []string {
	calls := extractToolCalls(data)
	if len(calls) == 0 {
		if verbose {
			return []string{fmt.Sprintf("[tool] 调用 (原始) %v", data)}
		}
		return []string{"[tool] 调用 (无 tool_calls 详情)"}
	}
	lines := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.Name == UserInformationToolName {
			// 问题正文在 user_information_required 时与「Agent 询问」合并为单条展示。
			continue
		}
		title := ToolDisplayName(call.Name, call.Arguments)
		block := call.ID
		if block == "" {
			block = blockID
		}
		_, codePreview := ToolCallParts(call.Name, call.Arguments)
		if block != "" {
			lines = append(lines, formatToolPendingLine(block, "调用 "+title))
			if codePreview != "" && !verbose {
				lines = append(lines, splitToolCallCodeLines(block, codePreview)...)
			}
		} else {
			lines = append(lines, "[tool] ▶ 调用 "+title)
		}
		if verbose {
			if detail := formatArgsDetail(call.Arguments, call.RawJSON); detail != "" {
				lines = append(lines, indentLines("    ", detail)...)
			}
		}
	}
	return lines
}

func formatToolPendingLine(blockID, title string) string {
	if blockID == "" {
		return "[tool] ▶ " + title
	}
	return formatToolMetaLine(toolPendingLinePrefix, blockID, "▶ "+title)
}

func splitToolCallCodeLines(blockID, code string) []string {
	code = strings.TrimRight(code, "\n")
	if strings.TrimSpace(code) == "" {
		return nil
	}
	parts := strings.Split(code, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimRight(p, "\r")
		if p == "" {
			continue
		}
		out = append(out, formatToolMetaLine(toolCallCodeLinePrefix, blockID, p))
	}
	return out
}

// ToolCallParts 解析 tool_call 短标题与可选代码预览（对齐 Python `_tool_call_parts_from_call`）。
func ToolCallParts(name string, args map[string]any) (summary, codeContent string) {
	summary = ToolDisplayName(name, args)
	switch strings.TrimSpace(name) {
	case "bash_run":
		cmd := strings.TrimSpace(trimDisplayField(args["command"]))
		if ToolCallPurpose(args) != "" {
			if cmd != "" {
				return summary, cmd
			}
			return summary, ""
		}
		_, full := bashCommandParts(cmd)
		return summary, full
	case "write_file":
		content := trimDisplayField(args["content"])
		if content != "" {
			return summary, content
		}
	}
	return summary, ""
}

func bashCommandParts(command string) (title, fullCommand string) {
	raw := strings.TrimSpace(command)
	if raw == "" {
		return "bash(—)", ""
	}
	cmd := sanitizeInlineToolArg(raw)
	if len([]rune(cmd)) <= bashInlineCommandMaxRunes {
		return "bash(" + cmd + ")", ""
	}
	preview := truncatePreview(cmd, bashInlineCommandMaxRunes-1)
	return "bash(" + preview + ")", raw
}

func sanitizeInlineToolArg(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.Join(strings.Fields(s), " ")
}

func formatToolResultEvent(data map[string]any, blockID string, verbose bool, elapsedSec float64) []string {
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
	if elapsedSec >= 0 {
		head += " · " + FormatToolElapsed(elapsedSec)
	}
	if pct := outputCompressSavedPct(data); pct > 0 {
		head += fmt.Sprintf(" · -%d%%", pct)
	}
	if IsTemporaryAgentTool(name) {
		if customHead, body, ok := formatTemporaryAgentToolResult(name, content, verbose); ok {
			head = customHead
			lines := []string{"[tool] " + head}
			if body != "" {
				lines = append(lines, indentLines("    ", body)...)
			}
			return lines
		}
	}
	lines := []string{"[tool] " + head}
	if content == "" {
		return lines
	}
	if blockID == "" {
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
	fullBody := content
	if verbose {
		lines = append(lines, splitToolDetailLines(blockID, fullBody)...)
		return lines
	}
	preview := summarizeToolResultContent(name, content)
	if preview != "" {
		lines = append(lines, formatToolMetaLine(toolPreviewLinePrefix, blockID, preview))
	}
	lines = append(lines, splitToolDetailLines(blockID, fullBody)...)
	return lines
}

func splitToolDetailLines(blockID, body string) []string {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	parts := strings.Split(body, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimRight(p, "\r")
		if p == "" {
			continue
		}
		out = append(out, formatToolMetaLine(toolDetailLinePrefix, blockID, p))
	}
	return out
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
	case name == "search_replace" || strings.HasPrefix(content, "成功:"):
		if hint := summarizeSearchReplaceMeta(content); hint != "" {
			if strings.Contains(content, "成功: 否") {
				return "✗ search_replace · " + hint
			}
			return "✓ search_replace · " + hint
		}
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
	case name == "search_replace" || strings.HasPrefix(content, "成功:"):
		if diff := searchReplaceDiff(content); diff != "" {
			return truncatePreviewLines(diff, toolDetailMaxLines, toolPreviewMaxRunes)
		}
		if strings.Contains(content, "成功: 否") {
			return summarizeSearchReplaceMeta(content)
		}
		return ""
	default:
		if IsTemporaryAgentTool(name) {
			if _, body, ok := formatTemporaryAgentToolResult(name, content, false); ok && body != "" {
				return body
			}
		}
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
	if !strings.Contains(first, "exit") {
		return -1
	}
	var code int
	if strings.Contains(first, "exit=") {
		if _, err := fmt.Sscanf(first, "[BASH_RESULT] exit=%d", &code); err == nil {
			return code
		}
		if _, err := fmt.Sscanf(first, "[BASH_RESULT] exit=%d truncated", &code); err == nil {
			return code
		}
		parts := strings.Split(first, "exit=")
		if len(parts) >= 2 {
			field := strings.Fields(parts[1])[0]
			if _, err := fmt.Sscanf(field, "%d", &code); err == nil {
				return code
			}
		}
	}
	if !strings.Contains(first, "exit_code=") {
		return -1
	}
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

func outputCompressSavedPct(data map[string]any) int {
	if data == nil {
		return 0
	}
	switch v := data["output_compress_saved_pct"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func splitSearchReplaceResult(content string) (meta, diff string) {
	parts := strings.SplitN(content, "\n---\n", 2)
	meta = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		diff = strings.TrimSpace(parts[1])
	}
	return meta, diff
}

func parseSearchReplaceFields(meta string) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(meta, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		fields[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return fields
}

func summarizeSearchReplaceMeta(content string) string {
	meta, _ := splitSearchReplaceResult(content)
	fields := parseSearchReplaceFields(meta)
	if fields["成功"] == "否" {
		if err := fields["错误"]; err != "" {
			return err
		}
		return "失败"
	}
	path := fields["路径"]
	count := fields["替换次数"]
	if count != "" && path != "" {
		return count + " 处替换 · " + path
	}
	if path != "" {
		return path
	}
	if count != "" {
		return count + " 处替换"
	}
	return ""
}

func searchReplaceDiff(content string) string {
	_, diff := splitSearchReplaceResult(content)
	return diff
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
	if agents, ok := m["agents"].([]any); ok {
		return summarizeDiscoverAgents(agents)
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

func summarizeDiscoverAgents(agents []any) string {
	n := len(agents)
	if n == 0 {
		return "0 个 Agent"
	}
	names := make([]string, 0, 3)
	for _, item := range agents {
		am, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label := firstNonEmptyField(am, "name", "agent_id")
		if label == "" {
			continue
		}
		names = append(names, label)
		if len(names) >= 3 {
			break
		}
	}
	if len(names) == 0 {
		return fmt.Sprintf("%d 个 Agent", n)
	}
	out := strings.Join(names, ", ")
	if n > len(names) {
		out += fmt.Sprintf(" 等 %d 个", n)
	}
	return out
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

// ToolCallPurpose 从 arguments 读取调用目的（call_purpose）。
func ToolCallPurpose(args map[string]any) string {
	return truncatePreview(firstNonEmptyField(args, CallPurposeKey), 48)
}

// ToolDisplayName 生成工具调用短标题（对齐 Python `tool_display_name`）。
func ToolDisplayName(name string, args map[string]any) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unknown"
	}
	if name == UserInformationToolName {
		return userInformationDisplayName
	}
	if purpose := ToolCallPurpose(args); purpose != "" {
		return toolDisplayBaseName(name) + "(" + purpose + ")"
	}
	switch name {
	case "bash_run":
		title, _ := bashCommandParts(trimDisplayField(args["command"]))
		return title
	case "trigger_create":
		triggerName := strings.TrimSpace(trimDisplayField(args["name"]))
		if triggerName == "" {
			triggerName = "—"
		}
		return "trigger_create(" + triggerName + ")"
	case "write_file", "read_file", "search_replace":
		path := firstNonEmptyField(args, "path", "file_path")
		if path == "" {
			path = "—"
		}
		return name + "(" + path + ")"
	case toolCreateTemporaryAgent, toolWaitTemporaryAgents, toolTemporaryAgentStatus, toolCancelTemporaryAgent:
		return FormatTemporaryAgentToolTitle(name, args)
	default:
		if len(args) == 0 {
			return name + "()"
		}
		parts := toolDisplayGenericParts(args)
		if len(parts) == 0 {
			return name + "()"
		}
		return name + "(" + strings.Join(parts, ", ") + ")"
	}
}

func toolDisplayGenericParts(args map[string]any) []string {
	keys := make([]string, 0, len(args))
	for key := range args {
		if key == CallPurposeKey || key == "run_in_background" {
			continue
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+formatToolArgValue(args[key]))
	}
	return parts
}

// formatToolArgValue 近似 Python `value!r` 的参数展示。
func formatToolArgValue(v any) string {
	if v == nil {
		return "null"
	}
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("%q", x)
	case bool:
		if x {
			return "True"
		}
		return "False"
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case int:
		return fmt.Sprintf("%d", x)
	case int64:
		return fmt.Sprintf("%d", x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprintf("%q", fmt.Sprint(x))
		}
		return string(b)
	}
}

func toolDisplayBaseName(name string) string {
	switch name {
	case "bash_run":
		return "bash"
	default:
		return name
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
	block = strings.TrimRight(block, "\n")
	if block == "" {
		return nil
	}
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
