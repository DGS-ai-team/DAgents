package browser

import (
	"context"
	"strings"
	"time"
)

const (
	defaultTaskWaitTimeout = 300 * time.Second
	taskPollInterval       = 1 * time.Second
)

// RunTaskOpts 控制任务派发与可选等待。
type RunTaskOpts struct {
	MaxSteps       int
	Wait           bool // true：轮询至终态再返回
	WaitTimeoutSec int  // Wait 时超时秒数；<=0 用默认 300s
}

// RunTask 向伴生 session 派发任务级浏览器自动化（sidecar browser_use.Agent）。
// 若 session 未启动则先 start；默认异步返回 task_id（见 RunTaskWithOpts）。
func (m *Manager) RunTask(ctx context.Context, sessionKey, task string, maxSteps int) (ToolResult, error) {
	return m.RunTaskWithOpts(ctx, sessionKey, task, RunTaskOpts{MaxSteps: maxSteps, Wait: false})
}

// RunTaskWithOpts 派发任务；Wait=true 时阻塞至 completed/failed/cancelled 或超时。
func (m *Manager) RunTaskWithOpts(ctx context.Context, sessionKey, task string, opts RunTaskOpts) (ToolResult, error) {
	sessionKey = strings.TrimSpace(sessionKey)
	task = strings.TrimSpace(task)
	if sessionKey == "" {
		return ToolResult{OK: false, Error: "session_key is required"}, nil
	}
	if task == "" {
		return ToolResult{OK: false, Error: "task is required"}, nil
	}
	if opts.MaxSteps < 0 {
		opts.MaxSteps = 0
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

	out, err := m.call(ctx, Request{
		Op:         "run_task",
		SessionKey: sessionKey,
		Task:       task,
		MaxSteps:   opts.MaxSteps,
	})
	if err != nil || !out.OK || !opts.Wait {
		return out, err
	}
	taskID, _ := out.Detail["task_id"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return out, nil
	}
	return m.waitTask(ctx, sessionKey, taskID, opts.WaitTimeoutSec)
}

func (m *Manager) waitTask(ctx context.Context, sessionKey, taskID string, timeoutSec int) (ToolResult, error) {
	timeout := defaultTaskWaitTimeout
	if timeoutSec > 0 {
		timeout = time.Duration(timeoutSec) * time.Second
	}
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(taskPollInterval)
	defer ticker.Stop()

	var last ToolResult
	for {
		st, err := m.TaskStatus(ctx, sessionKey, taskID)
		if err != nil {
			return st, err
		}
		last = st
		status, _ := st.Detail["status"].(string)
		switch strings.ToLower(strings.TrimSpace(status)) {
		case "completed", "failed", "cancelled":
			if st.Detail == nil {
				st.Detail = map[string]any{}
			}
			st.Detail["waited"] = true
			return st, nil
		}
		if time.Now().After(deadline) {
			if last.Detail == nil {
				last.Detail = map[string]any{}
			}
			last.Detail["waited"] = true
			last.Detail["wait_timed_out"] = true
			if last.Error == "" {
				last.Error = "browser task wait timed out; use browser_task_status to continue polling"
			}
			// 超时仍返回 ok=true + 当前状态，便于主 Agent 拿 task_id 继续查
			last.OK = true
			return last, nil
		}
		select {
		case <-ctx.Done():
			if last.Detail == nil {
				last.Detail = map[string]any{}
			}
			last.Detail["waited"] = true
			last.OK = false
			last.Error = ctx.Err().Error()
			return last, nil
		case <-ticker.C:
		}
	}
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
