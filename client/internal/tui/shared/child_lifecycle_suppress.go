package shared

import (
	"encoding/json"
	"strings"
	"sync"
)

// ChildLifecycleSuppress 抑制已在 wait_temporary_agents 工具结果中展示过的 lifecycle 系统行。
type ChildLifecycleSuppress struct {
	mu          sync.Mutex
	pendingWait map[string]struct{}
	shownInTool map[string]struct{}
}

// NewChildLifecycleSuppress 创建 lifecycle 抑制器。
func NewChildLifecycleSuppress() *ChildLifecycleSuppress {
	return &ChildLifecycleSuppress{
		pendingWait: make(map[string]struct{}),
		shownInTool: make(map[string]struct{}),
	}
}

// Reset 清空状态（session 切换时调用）。
func (s *ChildLifecycleSuppress) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.pendingWait = make(map[string]struct{})
	s.shownInTool = make(map[string]struct{})
	s.mu.Unlock()
}

// NoteToolCallEvent 在 wait_temporary_agents 工具调用发出后登记待汇总 child id。
func (s *ChildLifecycleSuppress) NoteToolCallEvent(data map[string]any) {
	if s == nil {
		return
	}
	for _, call := range toolCallsFromEvent(data) {
		if call.Name != toolWaitTemporaryAgents {
			continue
		}
		s.mu.Lock()
		for _, id := range stringSliceField(call.Arguments, "child_session_ids") {
			s.pendingWait[id] = struct{}{}
		}
		s.mu.Unlock()
	}
}

// NoteToolResult 在 wait_temporary_agents 工具结果展示后标记已汇总 child id。
func (s *ChildLifecycleSuppress) NoteToolResult(toolName, content string) {
	if s == nil {
		return
	}
	toolName = strings.TrimSpace(toolName)
	if toolName != toolWaitTemporaryAgents {
		return
	}
	for _, id := range childSessionIDsInWaitToolResult(content) {
		if id == "" {
			continue
		}
		s.mu.Lock()
		s.shownInTool[id] = struct{}{}
		delete(s.pendingWait, id)
		s.mu.Unlock()
	}
}

// ShouldSuppressLifecycle 是否应隐藏 temporary_agent_completed/cancelled 系统行。
func (s *ChildLifecycleSuppress) ShouldSuppressLifecycle(childID, eventType string) bool {
	if s == nil {
		return false
	}
	switch strings.TrimSpace(eventType) {
	case "temporary_agent_completed", "temporary_agent_cancelled":
	default:
		return false
	}
	childID = strings.TrimSpace(childID)
	if childID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.shownInTool[childID]; ok {
		delete(s.shownInTool, childID)
		return true
	}
	if _, ok := s.pendingWait[childID]; ok {
		return true
	}
	return false
}

func childSessionIDsInWaitToolResult(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" || strings.HasPrefix(content, "ERROR:") {
		return nil
	}
	var raw map[string]any
	if err := jsonUnmarshalObject(content, &raw); err != nil {
		return nil
	}
	results := parseTemporaryAgentResults(raw["results"])
	ids := make([]string, 0, len(results))
	for _, res := range results {
		id := strings.TrimSpace(res.ChildSessionID)
		if id != "" && isTerminalChildAgentStatus(res.Status) {
			ids = append(ids, id)
		}
	}
	return ids
}

func isTerminalChildAgentStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "cancelled", "expired":
		return true
	default:
		return false
	}
}

func toolCallsFromEvent(data map[string]any) []NormalizedToolCall {
	rawCalls, ok := data["tool_calls"].([]any)
	if !ok || len(rawCalls) == 0 {
		if name := trimDisplayField(data["name"]); name != "" || trimDisplayField(data["tool_name"]) != "" {
			return []NormalizedToolCall{NormalizeToolCallItem(data)}
		}
		return nil
	}
	out := make([]NormalizedToolCall, 0, len(rawCalls))
	for _, raw := range rawCalls {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		n := NormalizeToolCallItem(m)
		if n.Name == "unknown" && n.ID == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

func jsonUnmarshalObject(content string, raw *map[string]any) error {
	return json.Unmarshal([]byte(content), raw)
}
