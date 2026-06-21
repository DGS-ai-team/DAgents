package turn

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type asyncSourceContext struct {
	OriginalToolCallID string
	CallPurpose        string
	ParamsSummary      string
}

func asyncCallbackToolCallID(jobID string) string {
	return "async-job-" + strings.TrimSpace(jobID)
}

func isGeneratedAsyncCallbackToolCallID(id string) bool {
	return strings.HasPrefix(strings.TrimSpace(id), "async-job-")
}

func lookupAsyncSourceFromHistory(messages []llm.Message, toolName, jobID, payloadToolCallID string) asyncSourceContext {
	if id := strings.TrimSpace(payloadToolCallID); id != "" && !isGeneratedAsyncCallbackToolCallID(id) {
		if ctx := asyncSourceFromToolCallID(messages, id); ctx.OriginalToolCallID != "" {
			return ctx
		}
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return asyncSourceContext{}
	}
	for i := len(messages) - 1; i >= 0; i-- {
		m := messages[i]
		if m.Role != "tool" || !strings.Contains(m.Content, jobID) {
			continue
		}
		if ctx := asyncSourceFromToolCallID(messages, m.ToolCallID); ctx.OriginalToolCallID != "" {
			return ctx
		}
	}
	_ = toolName
	return asyncSourceContext{}
}

func asyncSourceFromToolCallID(messages []llm.Message, toolCallID string) asyncSourceContext {
	args, ok := toolCallArgsByID(messages, toolCallID)
	if !ok {
		return asyncSourceContext{}
	}
	toolName := toolCallNameByID(messages, toolCallID)
	purpose, summary := summarizeAsyncToolParams(toolName, args)
	return asyncSourceContext{
		OriginalToolCallID: strings.TrimSpace(toolCallID),
		CallPurpose:        purpose,
		ParamsSummary:      summary,
	}
}

func toolCallArgsByID(messages []llm.Message, toolCallID string) (map[string]any, bool) {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return nil, false
	}
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == toolCallID {
				return parseJSONArgs(tc.Function.Arguments), true
			}
		}
	}
	return nil, false
}

func toolCallNameByID(messages []llm.Message, toolCallID string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID == toolCallID {
				return strings.TrimSpace(tc.Function.Name)
			}
		}
	}
	return ""
}

func summarizeAsyncToolParams(toolName string, args map[string]any) (purpose, summary string) {
	if args == nil {
		return "", ""
	}
	purpose = strings.TrimSpace(fmt.Sprint(args["call_purpose"]))
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash_run":
		summary = clipAsyncParam(fmt.Sprint(args["command"]), 160)
	case "read_file", "write_file", "search_replace", "grep_file":
		summary = clipAsyncParam(firstNonEmpty(args, "path", "file_path"), 160)
	case "glob_files", "grep_files":
		summary = clipAsyncParam(firstNonEmpty(args, "directory", "path"), 120)
		if pat := strings.TrimSpace(fmt.Sprint(args["glob_pattern"])); pat != "" {
			summary = strings.TrimSpace(summary + " pattern=" + clipAsyncParam(pat, 80))
		} else if pat := strings.TrimSpace(fmt.Sprint(args["pattern"])); pat != "" {
			summary = strings.TrimSpace(summary + " pattern=" + clipAsyncParam(pat, 80))
		}
	case "background_job_status", "background_job_cancel":
		summary = strings.TrimSpace(fmt.Sprint(args["job_id"]))
	case "agent_invoke":
		summary = clipAsyncParam(fmt.Sprint(args["content"]), 120)
	default:
		summary = clipAsyncParam(firstNonEmpty(args, "command", "path", "content", "question", "task"), 120)
	}
	return purpose, strings.TrimSpace(summary)
}

func clipAsyncParam(s string, limit int) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "<nil>" {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

func formatAsyncToolUserMessage(toolName, jobID, status, purpose string) string {
	toolName = strings.TrimSpace(toolName)
	jobID = strings.TrimSpace(jobID)
	status = strings.TrimSpace(status)
	if status == "" {
		status = "succeeded"
	}
	meta := fmt.Sprintf("job_id=%s，status=%s", jobID, status)
	if p := strings.TrimSpace(purpose); p != "" {
		meta += fmt.Sprintf("，目的=%s", p)
	}
	return fmt.Sprintf("异步工具 %s 已完成（%s），请读取下方 tool 结果并继续任务。", toolName, meta)
}

func formatAsyncToolResultContent(toolName, jobID, status string, src asyncSourceContext, resultBody string) string {
	lines := []string{
		"[ASYNC_TOOL_RESULT]",
		fmt.Sprintf("tool_name=%s", strings.TrimSpace(toolName)),
		fmt.Sprintf("job_id=%s", strings.TrimSpace(jobID)),
		fmt.Sprintf("status=%s", strings.TrimSpace(status)),
	}
	if src.CallPurpose != "" {
		lines = append(lines, "call_purpose="+src.CallPurpose)
	}
	if src.ParamsSummary != "" {
		lines = append(lines, asyncParamLine(toolName, src.ParamsSummary))
	}
	if src.OriginalToolCallID != "" {
		lines = append(lines, "source_tool_call_id="+src.OriginalToolCallID)
	}
	lines = append(lines,
		fmt.Sprintf("工具%s执行已完成，job_id：%s，执行结果如下：%s", toolName, jobID, strings.TrimSpace(resultBody)),
	)
	return strings.Join(lines, "\n")
}

func asyncParamLine(toolName, summary string) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "bash_run":
		return "command=" + summary
	case "read_file", "write_file", "search_replace", "grep_file":
		return "path=" + summary
	default:
		return "params=" + summary
	}
}

func formatAsyncToolCallbackArgs(toolName, jobID, status string, src asyncSourceContext) string {
	payload := map[string]any{
		"job_id":    strings.TrimSpace(jobID),
		"tool_name": strings.TrimSpace(toolName),
		"status":    strings.TrimSpace(status),
	}
	if src.CallPurpose != "" {
		payload["call_purpose"] = src.CallPurpose
	}
	if src.ParamsSummary != "" {
		payload["params_summary"] = src.ParamsSummary
	}
	if src.OriginalToolCallID != "" {
		payload["source_tool_call_id"] = src.OriginalToolCallID
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func asyncToolResultAppliedInHistory(content, jobID string) bool {
	if !strings.Contains(content, "[ASYNC_TOOL_RESULT]") {
		return false
	}
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return false
	}
	return strings.Contains(content, "job_id="+jobID) || strings.Contains(content, "job_id："+jobID)
}
