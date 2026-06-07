package shared

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	toolCreateTemporaryAgent  = "create_temporary_agent"
	toolWaitTemporaryAgents   = "wait_temporary_agents"
	toolTemporaryAgentStatus  = "temporary_agent_status"
	toolCancelTemporaryAgent  = "cancel_temporary_agent"
	childResultPreviewMaxRunes = 72
)

type temporaryAgentResult struct {
	ChildSessionID string   `json:"child_session_id"`
	Status         string   `json:"status"`
	Summary        string   `json:"summary"`
	TurnCount      int      `json:"turn_count"`
	Error          string   `json:"error"`
	Artifacts      []string `json:"artifacts"`
}

// IsTemporaryAgentTool 判断是否为临时 Agent 管理工具。
func IsTemporaryAgentTool(name string) bool {
	switch strings.TrimSpace(name) {
	case toolCreateTemporaryAgent, toolWaitTemporaryAgents, toolTemporaryAgentStatus, toolCancelTemporaryAgent:
		return true
	default:
		return false
	}
}

// FormatTemporaryAgentToolTitle 生成临时 Agent 工具调用的短标题。
func FormatTemporaryAgentToolTitle(name string, args map[string]any) string {
	switch strings.TrimSpace(name) {
	case toolCreateTemporaryAgent:
		purpose := firstNonEmptyField(args, "purpose")
		if purpose == "" {
			purpose = "—"
		}
		wait, _ := args["wait"].(bool)
		if wait {
			return "创建临时 Agent · " + purpose + " (wait)"
		}
		return "创建临时 Agent · " + purpose
	case toolWaitTemporaryAgents:
		n := len(stringSliceField(args, "child_session_ids"))
		if n == 0 {
			return "等待临时 Agent"
		}
		title := fmt.Sprintf("等待 %d 个临时 Agent", n)
		if timeout := intField(args["timeout_seconds"]); timeout > 0 {
			title += fmt.Sprintf(" · %ds", timeout)
		}
		return title
	case toolTemporaryAgentStatus:
		n := len(stringSliceField(args, "child_session_ids"))
		if n == 0 {
			return "查询临时 Agent 状态"
		}
		return fmt.Sprintf("查询 %d 个临时 Agent 状态", n)
	case toolCancelTemporaryAgent:
		short := shortenChildSessionID(firstNonEmptyField(args, "child_session_id"))
		if short == "" {
			return "取消临时 Agent"
		}
		return "取消临时 Agent · " + short
	default:
		return name + "()"
	}
}

func formatTemporaryAgentToolResult(name, content string, verbose bool) (headline string, body string, handled bool) {
	if !IsTemporaryAgentTool(name) {
		return "", "", false
	}
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "ERROR:") {
		return "✗ " + name + " 错误", truncatePreview(content, toolPreviewMaxRunes), true
	}
	if strings.TrimSpace(name) == toolTemporaryAgentStatus && strings.HasPrefix(content, "[") {
		var arr []any
		if err := json.Unmarshal([]byte(content), &arr); err == nil {
			results := parseTemporaryAgentResults(arr)
			head := summarizeTemporaryAgentBatch(toolTemporaryAgentStatus, results, false)
			body := formatTemporaryAgentResultsBody(results, verbose)
			return head, body, true
		}
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return "", "", false
	}

	switch strings.TrimSpace(name) {
	case toolCreateTemporaryAgent:
		return formatCreateTemporaryAgentResult(raw)
	case toolWaitTemporaryAgents:
		return formatWaitTemporaryAgentsResult(raw, verbose)
	case toolTemporaryAgentStatus:
		return formatTemporaryAgentStatusResult(raw, verbose)
	case toolCancelTemporaryAgent:
		return formatCancelTemporaryAgentResult(raw)
	default:
		return "", "", false
	}
}

func formatCreateTemporaryAgentResult(raw map[string]any) (string, string, bool) {
	kind := strings.TrimSpace(fmt.Sprint(raw["kind"]))
	switch kind {
	case "result":
		res := parseTemporaryAgentResult(raw)
		head := fmt.Sprintf("✓ 临时 Agent 完成 · %s · %s", shortenChildSessionID(res.ChildSessionID), res.Status)
		body := formatSingleTemporaryAgentBody(res, false)
		return head, body, true
	case "handle", "":
		id := shortenChildSessionID(firstNonEmptyField(raw, "child_session_id"))
		purpose := firstNonEmptyField(raw, "purpose")
		parts := []string{"✓ 已创建临时 Agent"}
		if id != "" {
			parts = append(parts, id)
		}
		if purpose != "" {
			parts = append(parts, purpose)
		}
		if maxTurns := intField(raw["max_turns"]); maxTurns > 0 {
			parts = append(parts, fmt.Sprintf("max_turns=%d", maxTurns))
		}
		if skills := stringSliceField(raw, "loaded_skills"); len(skills) > 0 {
			parts = append(parts, "skills="+strings.Join(skills, ","))
		}
		return strings.Join(parts, " · "), "", true
	default:
		return "✓ create_temporary_agent 完成", truncatePreview(fmt.Sprint(raw), toolPreviewMaxRunes), true
	}
}

func formatWaitTemporaryAgentsResult(raw map[string]any, verbose bool) (string, string, bool) {
	results := parseTemporaryAgentResults(raw["results"])
	timedOut, _ := raw["timed_out"].(bool)
	head := summarizeTemporaryAgentBatch("wait_temporary_agents", results, timedOut)
	body := formatTemporaryAgentResultsBody(results, verbose)
	return head, body, true
}

func formatTemporaryAgentStatusResult(raw map[string]any, verbose bool) (string, string, bool) {
	results := parseTemporaryAgentResults(raw)
	if len(results) == 0 {
		if arr, ok := raw["results"].([]any); ok {
			results = parseTemporaryAgentResults(arr)
		}
	}
	head := summarizeTemporaryAgentBatch("temporary_agent_status", results, false)
	body := formatTemporaryAgentResultsBody(results, verbose)
	return head, body, true
}

func formatCancelTemporaryAgentResult(raw map[string]any) (string, string, bool) {
	id := shortenChildSessionID(firstNonEmptyField(raw, "child_session_id"))
	status := firstNonEmptyField(raw, "status")
	if status == "" {
		status = "cancelled"
	}
	head := "✓ 已取消临时 Agent"
	if id != "" {
		head += " · " + id
	}
	head += " · " + status
	return head, "", true
}

func summarizeTemporaryAgentBatch(toolName string, results []temporaryAgentResult, timedOut bool) string {
	total := len(results)
	if total == 0 {
		return "✓ " + toolName + " · 无结果"
	}
	completed, failed := 0, 0
	for _, res := range results {
		switch strings.TrimSpace(res.Status) {
		case "completed":
			completed++
		case "failed", "cancelled", "expired":
			failed++
		}
	}
	head := fmt.Sprintf("✓ %s · %d/%d 已结束", toolName, completed+failed, total)
	if completed > 0 {
		head += fmt.Sprintf("（%d 成功", completed)
		if failed > 0 {
			head += fmt.Sprintf("，%d 异常", failed)
		}
		head += "）"
	} else if failed > 0 {
		head += fmt.Sprintf("（%d 异常）", failed)
	}
	if timedOut {
		head += " · 超时"
	}
	return head
}

func formatTemporaryAgentResultsBody(results []temporaryAgentResult, verbose bool) string {
	if len(results) == 0 {
		return ""
	}
	lines := make([]string, 0, len(results))
	for i, res := range results {
		prefix := fmt.Sprintf("[%d] %s · %s", i+1, shortenChildSessionID(res.ChildSessionID), displayTemporaryAgentStatus(res))
		if verbose {
			block := formatSingleTemporaryAgentBody(res, true)
			if block == "" {
				lines = append(lines, prefix)
				continue
			}
			lines = append(lines, prefix)
			lines = append(lines, indentBlock(block, "    ")...)
			continue
		}
		if hint := temporaryAgentResultHint(res); hint != "" {
			lines = append(lines, prefix+" · "+hint)
		} else {
			lines = append(lines, prefix)
		}
	}
	return strings.Join(lines, "\n")
}

func formatSingleTemporaryAgentBody(res temporaryAgentResult, verbose bool) string {
	var parts []string
	if msg := strings.TrimSpace(res.Error); msg != "" {
		parts = append(parts, "error: "+msg)
	}
	summary := strings.TrimSpace(res.Summary)
	if summary != "" {
		if verbose {
			parts = append(parts, summary)
		} else {
			parts = append(parts, firstNonEmptyLine(summary))
		}
	}
	if len(res.Artifacts) > 0 {
		parts = append(parts, "artifacts: "+strings.Join(res.Artifacts, ", "))
	}
	if res.TurnCount > 0 {
		parts = append(parts, fmt.Sprintf("turn_count=%d", res.TurnCount))
	}
	return strings.Join(parts, "\n")
}

func temporaryAgentResultHint(res temporaryAgentResult) string {
	if msg := strings.TrimSpace(res.Error); msg != "" {
		return truncatePreview(msg, childResultPreviewMaxRunes)
	}
	if summary := strings.TrimSpace(res.Summary); summary != "" {
		return truncatePreview(firstNonEmptyLine(summary), childResultPreviewMaxRunes)
	}
	return ""
}

func displayTemporaryAgentStatus(res temporaryAgentResult) string {
	status := strings.TrimSpace(res.Status)
	if status == "" {
		return "unknown"
	}
	return status
}

func parseTemporaryAgentResults(raw any) []temporaryAgentResult {
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]temporaryAgentResult, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, parseTemporaryAgentResult(m))
	}
	return out
}

func parseTemporaryAgentResult(raw map[string]any) temporaryAgentResult {
	artifacts := make([]string, 0)
	if arr, ok := raw["artifacts"].([]any); ok {
		for _, item := range arr {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" && s != "<nil>" {
				artifacts = append(artifacts, s)
			}
		}
	}
	return temporaryAgentResult{
		ChildSessionID: firstNonEmptyField(raw, "child_session_id"),
		Status:         firstNonEmptyField(raw, "status"),
		Summary:        trimDisplayField(raw["summary"]),
		TurnCount:      intField(raw["turn_count"]),
		Error:          firstNonEmptyField(raw, "error"),
		Artifacts:      artifacts,
	}
}

func shortenChildSessionID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if len(id) <= 16 {
		return id
	}
	return id[:16] + "…"
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(text)
}

func stringSliceField(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s := strings.TrimSpace(fmt.Sprint(item))
		if s != "" && s != "<nil>" {
			out = append(out, s)
		}
	}
	return out
}

func intField(raw any) int {
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	default:
		return 0
	}
}

func indentBlock(block, prefix string) []string {
	return indentLines(prefix, block)
}
