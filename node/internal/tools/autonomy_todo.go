package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
)

func autonomyTodoToolDefs() []ToolDef {
	return []ToolDef{
		{Type: "function", Function: FunctionDef{Name: "todo_list", Description: "列出当前 Auto Agent 自己的待办事项。", Parameters: injectCallPurposeParam(map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false})}},
		{Type: "function", Function: FunctionDef{Name: "todo_create", Description: "创建当前 Auto Agent 的待办事项。", Parameters: injectCallPurposeParam(map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}, "required": []string{"text"}, "additionalProperties": false})}},
		{Type: "function", Function: FunctionDef{Name: "todo_update", Description: "按版本并发更新当前 Auto Agent 的待办事项。", Parameters: injectCallPurposeParam(map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "expected_revision": map[string]any{"type": "integer"}, "text": map[string]any{"type": "string"}, "status": map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed"}}}, "required": []string{"id", "expected_revision"}, "additionalProperties": false})}},
		{Type: "function", Function: FunctionDef{Name: "todo_delete", Description: "按版本删除当前 Auto Agent 的待办事项。", Parameters: injectCallPurposeParam(map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "expected_revision": map[string]any{"type": "integer"}}, "required": []string{"id", "expected_revision"}, "additionalProperties": false})}},
	}
}

func (r *Registry) SetAutonomyTodoStore(s *autonomy.Store) {
	if r != nil {
		r.autonomyTodoStore = s
	}
}

func (r *Registry) autonomyTodoReady() (*autonomy.Store, error) {
	if r == nil || !r.autonomyEnabled || r.autonomyTodoStore == nil || strings.TrimSpace(r.agentID) == "" {
		return nil, fmt.Errorf("autonomy todos are unavailable")
	}
	return r.autonomyTodoStore, nil
}

func parseTodoArgs(raw json.RawMessage, allowed map[string]bool) (map[string]any, error) {
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("invalid todo arguments: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return nil, fmt.Errorf("trailing todo arguments")
	} else if err != io.EOF {
		return nil, fmt.Errorf("invalid todo arguments: %w", err)
	}
	if purpose, present := m[CallPurposeKey]; present {
		p, ok := purpose.(string)
		if !ok || strings.TrimSpace(p) == "" {
			return nil, fmt.Errorf("call_purpose must be a non-empty string")
		}
	}
	for k := range m {
		if k != CallPurposeKey && !allowed[k] {
			return nil, fmt.Errorf("unknown todo argument %q", k)
		}
	}
	return m, nil
}

func todoString(m map[string]any, key string, required bool) (string, bool, error) {
	v, exists := m[key]
	if !exists {
		if required {
			return "", false, fmt.Errorf("%s is required", key)
		}
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return "", true, fmt.Errorf("%s must be a non-empty string", key)
	}
	return strings.TrimSpace(s), true, nil
}
func todoRevision(m map[string]any) (int64, error) {
	n, ok := m["expected_revision"].(json.Number)
	if !ok {
		return 0, fmt.Errorf("expected_revision is required")
	}
	v, err := n.Int64()
	if err != nil || v < 1 {
		return 0, fmt.Errorf("expected_revision is required")
	}
	return v, nil
}

func (r *Registry) execTodoList(ctx context.Context, raw json.RawMessage) (string, error) {
	st, err := r.autonomyTodoReady()
	if err != nil {
		return "", err
	}
	if _, err = parseTodoArgs(raw, map[string]bool{}); err != nil {
		return "", err
	}
	b, err := json.Marshal(st.ListTodos(r.agentID))
	return string(b), err
}
func (r *Registry) execTodoCreate(ctx context.Context, raw json.RawMessage) (string, error) {
	st, err := r.autonomyTodoReady()
	if err != nil {
		return "", err
	}
	m, err := parseTodoArgs(raw, map[string]bool{"text": true})
	if err != nil {
		return "", err
	}
	text, _, err := todoString(m, "text", true)
	if err != nil {
		return "", err
	}
	t, err := st.CreateTodo(r.agentID, text)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(t)
	return string(b), err
}
func (r *Registry) execTodoUpdate(ctx context.Context, raw json.RawMessage) (string, error) {
	st, err := r.autonomyTodoReady()
	if err != nil {
		return "", err
	}
	m, err := parseTodoArgs(raw, map[string]bool{"id": true, "expected_revision": true, "text": true, "status": true})
	if err != nil {
		return "", err
	}
	id, _, err := todoString(m, "id", true)
	if err != nil {
		return "", err
	}
	rev, err := todoRevision(m)
	if err != nil {
		return "", err
	}
	text, hasText, err := todoString(m, "text", false)
	if err != nil {
		return "", err
	}
	status, hasStatus, err := todoString(m, "status", false)
	if err != nil {
		return "", err
	}
	if !hasText && !hasStatus {
		return "", fmt.Errorf("todo update is empty")
	}
	if !hasText {
		text = ""
	}
	if !hasStatus {
		status = ""
	}
	t, err := st.UpdateTodoCAS(r.agentID, id, rev, text, status)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(t)
	return string(b), err
}
func (r *Registry) execTodoDelete(ctx context.Context, raw json.RawMessage) (string, error) {
	st, err := r.autonomyTodoReady()
	if err != nil {
		return "", err
	}
	m, err := parseTodoArgs(raw, map[string]bool{"id": true, "expected_revision": true})
	if err != nil {
		return "", err
	}
	id, _, err := todoString(m, "id", true)
	if err != nil {
		return "", err
	}
	rev, err := todoRevision(m)
	if err != nil {
		return "", err
	}
	if err := st.DeleteTodoCAS(r.agentID, id, rev); err != nil {
		return "", err
	}
	return `{"deleted":true}`, nil
}
