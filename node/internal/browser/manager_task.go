package browser

import (
	"context"
	"strings"
)

// RunTask 向伴生 session 派发任务级浏览器自动化（sidecar browser_use.Agent）。
// 若 session 未启动则先 start；返回异步 task_id（detail.task_id / status）。
func (m *Manager) RunTask(ctx context.Context, sessionKey, task string, maxSteps int) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	task = strings.TrimSpace(task)
	if sessionKey == "" {
		return ToolResult{OK: false, Error: "session_key is required"}, nil
	}
	if task == "" {
		return ToolResult{OK: false, Error: "task is required"}, nil
	}
	if maxSteps < 0 {
		maxSteps = 0
	}

	m.mu.Lock()
	_, started := m.sessions[sessionKey]
	m.mu.Unlock()
	if !started {
		if out, err := m.Start(ctx, sessionKey, nil, 0, 0); err != nil {
			return out, err
		} else if !out.OK {
			return out, nil
		}
	}

	return m.call(ctx, Request{
		Op:         "run_task",
		SessionKey: sessionKey,
		Task:       task,
		MaxSteps:   maxSteps,
	})
}

// TaskStatus 查询伴生 session 上某任务（或最近任务）状态。
func (m *Manager) TaskStatus(ctx context.Context, sessionKey, taskID string) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ToolResult{OK: false, Error: "session_key is required"}, nil
	}
	return m.call(ctx, Request{
		Op:         "task_status",
		SessionKey: sessionKey,
		TaskID:     strings.TrimSpace(taskID),
	})
}

// TaskCancel 取消伴生 session 上运行中的任务。
func (m *Manager) TaskCancel(ctx context.Context, sessionKey, taskID string) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	if sessionKey == "" {
		return ToolResult{OK: false, Error: "session_key is required"}, nil
	}
	return m.call(ctx, Request{
		Op:         "task_cancel",
		SessionKey: sessionKey,
		TaskID:     strings.TrimSpace(taskID),
	})
}
