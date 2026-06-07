package childagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// HandleWait 实现 wait_temporary_agents 工具。
func (m *Manager) HandleWait(ctx context.Context, parentSessionID, argsJSON string) (string, error) {
	ids, timeoutSec, failFast, err := parseWaitInput(argsJSON, m.cfg.DefaultWaitTimeoutSeconds)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}
	if len(ids) == 0 {
		return "ERROR: child_session_ids is required", nil
	}
	for _, id := range ids {
		if err := m.validateOwnership(parentSessionID, id); err != nil {
			return "ERROR: " + err.Error(), nil
		}
	}
	if timeoutSec == 0 {
		return m.formatWaitResults(ids, false)
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	for {
		done, timedOut := m.allTerminalOrFailFast(ids, failFast)
		if done {
			return m.formatWaitResults(ids, timedOut)
		}
		if time.Now().After(deadline) {
			return m.formatWaitResults(ids, true)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// HandleStatus 实现 temporary_agent_status 工具。
func (m *Manager) HandleStatus(parentSessionID, argsJSON string) (string, error) {
	ids, err := parseIDList(argsJSON)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}
	if len(ids) == 0 {
		return "ERROR: child_session_ids is required", nil
	}
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		if err := m.validateOwnership(parentSessionID, id); err != nil {
			return "ERROR: " + err.Error(), nil
		}
		res, err := m.GetResult(id)
		if err != nil {
			return "ERROR: " + err.Error(), nil
		}
		results = append(results, res)
	}
	body, _ := json.Marshal(results)
	return string(body), nil
}

// HandleCancelTool 实现 cancel_temporary_agent 工具。
func (m *Manager) HandleCancelTool(parentSessionID, argsJSON string) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return "ERROR: invalid json", nil
	}
	childID := strings.TrimSpace(fmt.Sprint(raw["child_session_id"]))
	reason := strings.TrimSpace(fmt.Sprint(raw["reason"]))
	if childID == "" || childID == "<nil>" {
		return "ERROR: child_session_id is required", nil
	}
	prev, err := m.cancelInternal(parentSessionID, childID, reason)
	if err != nil {
		return "ERROR: " + err.Error(), nil
	}
	body, _ := json.Marshal(map[string]any{
		"child_session_id": childID,
		"status":           StatusCancelled,
		"previous_status":  prev,
	})
	return string(body), nil
}

func (m *Manager) cancelInternal(parentSessionID, childID, reason string) (previous string, err error) {
	m.mu.Lock()
	rec, ok := m.records[childID]
	if !ok {
		m.mu.Unlock()
		return "", fmt.Errorf("child_session_id not found or not owned by parent")
	}
	if rec.ParentSessionID != parentSessionID {
		m.mu.Unlock()
		return "", fmt.Errorf("child_session_id not found or not owned by parent")
	}
	if rec.terminal() {
		prev := string(rec.Status)
		m.mu.Unlock()
		return prev, nil
	}
	prev := string(rec.Status)
	m.mu.Unlock()
	_, err = m.Cancel(parentSessionID, childID, reason)
	return prev, err
}

func (m *Manager) validateOwnership(parentSessionID, childID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rec, ok := m.records[childID]; ok {
		if rec.ParentSessionID != parentSessionID {
			return fmt.Errorf("child_session_id not found or not owned by parent")
		}
		return nil
	}
	if owner, ok := m.parentOf[childID]; ok {
		if owner != parentSessionID {
			return fmt.Errorf("child_session_id not found or not owned by parent")
		}
		return nil
	}
	return fmt.Errorf("child_session_id not found or not owned by parent")
}

func (m *Manager) allTerminalOrFailFast(ids []string, failFast bool) (done bool, timedOut bool) {
	allTerminal := true
	for _, id := range ids {
		m.mu.Lock()
		rec := m.records[id]
		terminal := rec == nil || rec.terminal()
		status := StatusActive
		if rec != nil {
			status = rec.Status
		}
		m.mu.Unlock()
		if !terminal {
			allTerminal = false
		}
		if failFast && (status == StatusFailed || status == StatusCancelled || status == StatusExpired) {
			return true, false
		}
	}
	return allTerminal, false
}

func (m *Manager) formatWaitResults(ids []string, timedOut bool) (string, error) {
	results := make([]Result, 0, len(ids))
	for _, id := range ids {
		res, err := m.GetResult(id)
		if err != nil {
			results = append(results, Result{ChildSessionID: id, Status: StatusCompleted})
			continue
		}
		results = append(results, res)
	}
	body, _ := json.Marshal(map[string]any{
		"timed_out": timedOut,
		"results":   results,
	})
	return string(body), nil
}

func parseWaitInput(argsJSON string, defaultTimeout int) (ids []string, timeoutSec int, failFast bool, err error) {
	var raw map[string]any
	if err = json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return nil, 0, false, err
	}
	ids, err = parseIDListFromMap(raw)
	if err != nil {
		return nil, 0, false, err
	}
	timeoutSec = defaultTimeout
	if v, ok := raw["timeout_seconds"].(float64); ok {
		timeoutSec = int(v)
	}
	failFast, _ = raw["fail_fast"].(bool)
	return ids, timeoutSec, failFast, nil
}

func parseIDList(argsJSON string) ([]string, error) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &raw); err != nil {
		return nil, err
	}
	return parseIDListFromMap(raw)
}

func parseIDListFromMap(raw map[string]any) ([]string, error) {
	arr, ok := raw["child_session_ids"].([]any)
	if !ok {
		return nil, fmt.Errorf("child_session_ids must be array")
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s := strings.TrimSpace(fmt.Sprint(item))
		if s != "" && s != "<nil>" {
			out = append(out, s)
		}
	}
	return out, nil
}

// HandleParentTool 分发父 Agent 临时 Agent 管理工具（非 A2A）。
func (m *Manager) HandleParentTool(ctx context.Context, parentSessionID, toolName, argsJSON string) (string, error) {
	switch toolName {
	case ToolCreateTemporaryAgent:
		return m.HandleCreate(ctx, parentSessionID, argsJSON)
	case ToolWaitTemporaryAgents:
		return m.HandleWait(ctx, parentSessionID, argsJSON)
	case ToolTemporaryAgentStatus:
		return m.HandleStatus(parentSessionID, argsJSON)
	case ToolCancelTemporaryAgent:
		return m.HandleCancelTool(parentSessionID, argsJSON)
	default:
		return "", fmt.Errorf("unknown temporary agent tool %q", toolName)
	}
}
