package childagent

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const progressPreviewLimit = 240
const activityInputLimit = 180
const maxRecentToolActivities = 8

// ObserveChildEvent updates the manager-owned progress snapshot for one child.
// The snapshot is intentionally derived from the existing child SSE stream;
// no second transcript or event queue is introduced.
func (m *Manager) ObserveChildEvent(childSessionID, eventType string, data map[string]any) {
	if m == nil {
		return
	}
	m.mu.Lock()
	agent, ok := m.activeByID[childSessionID]
	if !ok || agent == nil || agent.isTerminal() {
		m.mu.Unlock()
		return
	}
	agent.mu.Lock()
	previous := agent.Progress
	next := previous
	next.RecentTools = cloneToolActivities(previous.RecentTools)
	next.PendingApprovalData = cloneMap(previous.PendingApprovalData)
	next.Status = agent.Status
	next.MaxTurns = agent.MaxTurns
	applyChildProgressEvent(&next, eventType, data)
	if progressEqual(previous, next) {
		agent.mu.Unlock()
		m.mu.Unlock()
		return
	}
	next.Revision = previous.Revision + 1
	next.UpdatedAt = time.Now().UTC()
	agent.Progress = next
	parentID := agent.ParentAgentID
	agent.mu.Unlock()
	m.mu.Unlock()

	if err := m.persistAgent(agent); err != nil {
		m.logger.Warn("persist child progress failed", "child_agent_id", childSessionID, "error", err)
	}
	m.publishProgress(parentID, agent, next)
}

func applyChildProgressEvent(progress *Progress, eventType string, data map[string]any) {
	if progress == nil {
		return
	}
	switch eventType {
	case "turn_state":
		if phase := progressString(data, "phase"); phase != "" {
			progress.Phase = phase
		}
		if step := progressInt(data, "step_index"); step > progress.TurnCount {
			progress.TurnCount = step
		}
		if exec := firstRunningExecution(data); exec != nil {
			progress.CurrentTool = progressString(exec, "tool_name")
			progress.CurrentToolCallID = progressString(exec, "tool_call_id")
			progress.CurrentToolStatus = progressString(exec, "status")
		}
		mergeToolExecutionViews(progress, data["tool_executions"])
		phase := strings.ToLower(strings.TrimSpace(progress.Phase))
		if phase == "tool_waiting" || phase == "waiting_user" ||
			strings.EqualFold(progressString(data, "interaction_kind"), "approval") {
			progress.PendingApproval = true
			progress.Phase = "waiting_approval"
		} else if phase == "tool_executing" || phase == "model_generating" {
			progress.PendingApproval = false
			progress.PendingApprovalData = nil
		}
	case "tool_call":
		if call := firstToolCall(data); call != nil {
			name := toolCallName(call)
			callID := toolCallID(call)
			progress.CurrentTool = name
			progress.CurrentToolCallID = callID
			upsertRecentTool(progress, ToolActivity{
				ToolCallID:   callID,
				ToolName:     name,
				Status:       "running",
				InputSummary: toolActivityInputSummary(name, call),
				StartedAt:    time.Now().UTC(),
			})
		}
		if id := progressString(data, "tool_call_id"); id != "" {
			progress.CurrentToolCallID = id
		}
		if progress.CurrentToolCallID != "" {
			upsertRecentTool(progress, ToolActivity{
				ToolCallID: progress.CurrentToolCallID,
				ToolName:   progress.CurrentTool,
				Status:     "running",
			})
		}
		progress.CurrentToolStatus = "running"
		progress.Phase = "tool_executing"
		progress.PendingApproval = false
		progress.PendingApprovalData = nil
	case "tool_result":
		if name := progressString(data, "tool_name"); name != "" {
			progress.CurrentTool = name
		}
		if id := progressString(data, "tool_call_id"); id != "" {
			progress.CurrentToolCallID = id
		}
		status := strings.ToLower(strings.TrimSpace(progressString(data, "status")))
		if status == "" {
			status = "completed"
		}
		progress.CurrentToolStatus = status
		progress.LastOutputPreview = truncateProgress(firstProgressLine(data["content"]))
		upsertRecentTool(progress, ToolActivity{
			ToolCallID:    progress.CurrentToolCallID,
			ToolName:      progress.CurrentTool,
			Status:        status,
			InputSummary:  toolActivityInputSummaryFromResult(progress.CurrentTool, data),
			OutputPreview: progress.LastOutputPreview,
			FinishedAt:    time.Now().UTC(),
		})
		progress.Phase = "tool_completed"
		progress.PendingApproval = false
		progress.PendingApprovalData = nil
	case "assistant", "reasoning":
		progress.Phase = "model_generating"
		progress.PendingApproval = false
		progress.CurrentTool = ""
		progress.CurrentToolCallID = ""
		progress.CurrentToolStatus = ""
		progress.PendingApprovalData = nil
	case "hitl_required":
		progress.Phase = "waiting_approval"
		progress.PendingApproval = true
		progress.PendingApprovalData = cloneMap(data)
	}
}

func (m *Manager) publishProgress(parentID string, agent *ActiveAgent, progress Progress) {
	if m == nil || m.hub == nil || agent == nil {
		return
	}
	snapshot := agent.Snapshot()
	payload := map[string]any{
		"child_agent_id":        snapshot.ChildAgentID,
		"parent_agent_id":       parentID,
		"tool_call_id":          snapshot.ToolCallID,
		"purpose":               snapshot.Purpose,
		"status":                progress.Status,
		"phase":                 progress.Phase,
		"turn_count":            progress.TurnCount,
		"max_turns":             progress.MaxTurns,
		"current_tool":          progress.CurrentTool,
		"current_tool_call_id":  progress.CurrentToolCallID,
		"current_tool_status":   progress.CurrentToolStatus,
		"last_output_preview":   progress.LastOutputPreview,
		"recent_tools":          cloneToolActivities(progress.RecentTools),
		"pending_approval":      progress.PendingApproval,
		"pending_approval_data": progress.PendingApprovalData,
		"summary":               progress.Summary,
		"error":                 progress.Error,
		"updated_at":            progress.UpdatedAt,
		"revision":              progress.Revision,
	}
	m.hub.Publish(parentID, EventTemporaryAgentProgress, payload)
}

func progressEqual(a, b Progress) bool {
	return a.Status == b.Status &&
		a.Phase == b.Phase &&
		a.TurnCount == b.TurnCount &&
		a.MaxTurns == b.MaxTurns &&
		a.CurrentTool == b.CurrentTool &&
		a.CurrentToolCallID == b.CurrentToolCallID &&
		a.CurrentToolStatus == b.CurrentToolStatus &&
		a.LastOutputPreview == b.LastOutputPreview &&
		toolActivitiesEqual(a.RecentTools, b.RecentTools) &&
		a.PendingApproval == b.PendingApproval &&
		jsonEqual(a.PendingApprovalData, b.PendingApprovalData) &&
		a.Summary == b.Summary &&
		a.Error == b.Error
}

func mergeToolExecutionViews(progress *Progress, raw any) {
	if progress == nil {
		return
	}
	for _, item := range anySlice(raw) {
		execution, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := progressString(execution, "tool_name")
		callID := progressString(execution, "tool_call_id")
		status := strings.ToLower(strings.TrimSpace(progressString(execution, "status")))
		if name == "" && callID == "" {
			continue
		}
		upsertRecentTool(progress, ToolActivity{ToolCallID: callID, ToolName: name, Status: status})
	}
}

func upsertRecentTool(progress *Progress, next ToolActivity) {
	if progress == nil || strings.TrimSpace(next.ToolName) == "" {
		return
	}
	next.ToolName = strings.TrimSpace(next.ToolName)
	next.ToolCallID = strings.TrimSpace(next.ToolCallID)
	next.Status = strings.ToLower(strings.TrimSpace(next.Status))
	if next.Status == "" {
		next.Status = "running"
	}
	next.InputSummary = truncateActivityInput(next.InputSummary)
	next.OutputPreview = truncateProgress(next.OutputPreview)

	index := -1
	for i := len(progress.RecentTools) - 1; i >= 0; i-- {
		item := progress.RecentTools[i]
		if next.ToolCallID != "" && item.ToolCallID == next.ToolCallID {
			index = i
			break
		}
		if next.ToolCallID != "" && item.ToolCallID == "" && item.ToolName == next.ToolName && !toolActivityTerminal(item.Status) {
			index = i
			break
		}
		if next.ToolCallID == "" && item.ToolCallID == "" && item.ToolName == next.ToolName && !toolActivityTerminal(item.Status) {
			index = i
			break
		}
	}
	if index < 0 {
		if len(progress.RecentTools) >= maxRecentToolActivities {
			progress.RecentTools = append([]ToolActivity(nil), progress.RecentTools[len(progress.RecentTools)-maxRecentToolActivities+1:]...)
		}
		if next.StartedAt.IsZero() {
			next.StartedAt = time.Now().UTC()
		}
		progress.RecentTools = append(progress.RecentTools, next)
		return
	}

	item := progress.RecentTools[index]
	if item.ToolCallID == "" {
		item.ToolCallID = next.ToolCallID
	}
	if item.ToolName == "" {
		item.ToolName = next.ToolName
	}
	if !toolActivityTerminal(item.Status) || toolActivityTerminal(next.Status) {
		item.Status = next.Status
	}
	if next.InputSummary != "" {
		item.InputSummary = next.InputSummary
	}
	if next.OutputPreview != "" {
		item.OutputPreview = next.OutputPreview
	}
	if item.StartedAt.IsZero() {
		item.StartedAt = next.StartedAt
		if item.StartedAt.IsZero() {
			item.StartedAt = time.Now().UTC()
		}
	}
	if !next.FinishedAt.IsZero() {
		item.FinishedAt = next.FinishedAt
	}
	progress.RecentTools[index] = item
}

func toolActivityTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "succeeded", "completed", "failed", "error", "denied", "rejected", "cancelled", "canceled", "timed_out", "interrupted":
		return true
	default:
		return false
	}
}

func toolActivitiesEqual(a, b []ToolActivity) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func toolActivityInputSummaryFromResult(toolName string, data map[string]any) string {
	if data == nil {
		return ""
	}
	if args, ok := data["arguments"]; ok {
		if summary := toolActivityInputSummaryFromArgs(toolName, activityArgs(args)); summary != "" {
			return summary
		}
	}
	if raw := progressString(data, "raw_arguments"); raw != "" {
		return toolActivityInputSummaryFromArgs(toolName, activityArgs(raw))
	}
	return ""
}

func toolActivityInputSummary(toolName string, call map[string]any) string {
	if call == nil {
		return ""
	}
	return toolActivityInputSummaryFromArgs(toolName, activityArgsFromCall(call))
}

func toolActivityInputSummaryFromArgs(toolName string, args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	name := strings.ToLower(strings.TrimSpace(toolName))
	keys := []string{}
	switch name {
	case "bash_run", "terminal_command":
		keys = []string{"command", "cmd", "data"}
	case "terminal_input":
		keys = []string{"data", "command", "terminal_id"}
	case "read_file", "write_file", "search_replace", "glob_files", "grep_file", "grep_files":
		keys = []string{"path", "file_path", "directory", "pattern"}
	case "browser_run_task":
		keys = []string{"task", "url"}
	case "screen_capture", "computer_use":
		keys = []string{"action", "coordinate", "x", "y"}
	}
	for _, key := range keys {
		if value, ok := args[key]; ok {
			if summary := safeActivityValue(key, value); summary != "" {
				return summary
			}
		}
	}
	allKeys := make([]string, 0, len(args))
	for key := range args {
		if key == "call_purpose" || key == "purpose" {
			continue
		}
		allKeys = append(allKeys, key)
	}
	sort.Strings(allKeys)
	for _, key := range allKeys {
		if summary := safeActivityValue(key, args[key]); summary != "" {
			return key + "=" + summary
		}
	}
	return ""
}

func safeActivityValue(key string, value any) string {
	keyLower := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"password", "secret", "token", "private_key", "apikey", "api_key", "credential"} {
		if strings.Contains(keyLower, marker) {
			return "[已隐藏]"
		}
	}
	if value == nil {
		return ""
	}
	if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
		return truncateActivityInput(text)
	}
	return ""
}

func truncateActivityInput(text string) string {
	text = strings.TrimSpace(text)
	if len([]rune(text)) <= activityInputLimit {
		return text
	}
	runes := []rune(text)
	return string(runes[:activityInputLimit-3]) + "..."
}

func activityArgsFromCall(call map[string]any) map[string]any {
	if args, ok := call["arguments"]; ok {
		if parsed := activityArgs(args); len(parsed) > 0 {
			return parsed
		}
	}
	if fn, ok := call["function"].(map[string]any); ok {
		return activityArgs(fn["arguments"])
	}
	return nil
}

func activityArgs(raw any) map[string]any {
	switch value := raw.(type) {
	case map[string]any:
		return value
	case string:
		var parsed map[string]any
		if json.Unmarshal([]byte(value), &parsed) == nil {
			return parsed
		}
	}
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}

func jsonEqual(a, b any) bool {
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	return errA == nil && errB == nil && string(ra) == string(rb)
}

func firstRunningExecution(data map[string]any) map[string]any {
	raw, ok := data["tool_executions"]
	if !ok {
		return nil
	}
	for _, item := range anySlice(raw) {
		if execution, ok := item.(map[string]any); ok {
			status := strings.ToLower(strings.TrimSpace(progressString(execution, "status")))
			if status == "running" || status == "executing" || status == "pending" {
				return execution
			}
		}
	}
	return nil
}

func firstToolCall(data map[string]any) map[string]any {
	for _, item := range anySlice(data["tool_calls"]) {
		if call, ok := item.(map[string]any); ok {
			return call
		}
	}
	if _, ok := data["function"].(map[string]any); ok {
		return data
	}
	return nil
}

func toolCallName(call map[string]any) string {
	if name := progressString(call, "name"); name != "" {
		return name
	}
	fn, _ := call["function"].(map[string]any)
	return progressString(fn, "name")
}

func toolCallID(call map[string]any) string {
	return progressString(call, "id")
}

func anySlice(raw any) []any {
	switch value := raw.(type) {
	case []any:
		return value
	case []map[string]any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func progressString(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	value, ok := data[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

func progressInt(data map[string]any, key string) int {
	if data == nil {
		return 0
	}
	switch value := data[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func firstProgressLine(raw any) string {
	for _, line := range strings.Split(fmt.Sprint(raw), "\n") {
		if text := strings.TrimSpace(line); text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func truncateProgress(text string) string {
	text = strings.TrimSpace(text)
	if len(text) <= progressPreviewLimit {
		return text
	}
	return text[:progressPreviewLimit-3] + "..."
}
