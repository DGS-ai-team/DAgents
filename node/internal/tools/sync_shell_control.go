package tools

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// syncShellGate 允许 UI 在同步等待期间请求终止。
type syncShellGate struct {
	cancelOnce sync.Once
	cancelCh   chan struct{}
}

func newSyncShellGate() *syncShellGate {
	return &syncShellGate{
		cancelCh: make(chan struct{}),
	}
}

func (g *syncShellGate) RequestCancel() bool {
	if g == nil {
		return false
	}
	ok := false
	g.cancelOnce.Do(func() {
		close(g.cancelCh)
		ok = true
	})
	return ok
}

type syncShellEntry struct {
	sessionID  string
	toolCallID string
	job        *backgroundJob
	gate       *syncShellGate
}

type syncShellTracker struct {
	mu     sync.Mutex
	byCall map[string]*syncShellEntry
}

func newSyncShellTracker() *syncShellTracker {
	return &syncShellTracker{byCall: make(map[string]*syncShellEntry)}
}

func (t *syncShellTracker) put(entry *syncShellEntry) {
	if t == nil || entry == nil {
		return
	}
	id := strings.TrimSpace(entry.toolCallID)
	if id == "" {
		return
	}
	t.mu.Lock()
	t.byCall[id] = entry
	t.mu.Unlock()
}

func (t *syncShellTracker) remove(toolCallID string) {
	if t == nil {
		return
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return
	}
	t.mu.Lock()
	delete(t.byCall, id)
	t.mu.Unlock()
}

func (t *syncShellTracker) get(toolCallID string) (*syncShellEntry, bool) {
	if t == nil {
		return nil, false
	}
	id := strings.TrimSpace(toolCallID)
	if id == "" {
		return nil, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.byCall[id]
	return e, ok
}

func (t *syncShellTracker) countSession(sessionID string) int {
	if t == nil {
		return 0
	}
	sid := strings.TrimSpace(sessionID)
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, e := range t.byCall {
		if e != nil && (sid == "" || e.sessionID == sid) {
			n++
		}
	}
	return n
}

func (t *syncShellTracker) callIDsSession(sessionID string) []string {
	if t == nil {
		return nil
	}
	sid := strings.TrimSpace(sessionID)
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, 0, len(t.byCall))
	for id, e := range t.byCall {
		if e == nil {
			continue
		}
		if sid != "" && e.sessionID != "" && e.sessionID != sid {
			continue
		}
		out = append(out, id)
	}
	return out
}

// ToolJobCounts 为某 session 的同步执行中 / 后台 running 数量与 tool_call_id 列表。
type ToolJobCounts struct {
	Running           int      `json:"running"`
	Background        int      `json:"background"`
	RunningCallIDs    []string `json:"running_call_ids"`
	BackgroundCallIDs []string `json:"background_call_ids"`
}

// SessionToolJobCounts 返回指定 session 的工具执行计数。
func (r *Registry) SessionToolJobCounts(sessionID string) ToolJobCounts {
	if r == nil {
		return ToolJobCounts{}
	}
	return ToolJobCounts{
		Running:           r.syncShells.countSession(sessionID),
		Background:        r.bgJobs.countRunning(sessionID),
		RunningCallIDs:    r.syncShells.callIDsSession(sessionID),
		BackgroundCallIDs: r.bgJobs.runningCallIDs(sessionID),
	}
}

// ErrSyncShellNotFound 表示没有可控制的同步 bash。
var ErrSyncShellNotFound = fmt.Errorf("sync bash tool call not found")

// ErrSyncShellNotBash 预留：非 bash 工具暂不支持同步终止。
var ErrSyncShellNotBash = fmt.Errorf("only bash_run supports synchronous cancel")

// ErrBackgroundUnsupported 表示工具不支持后台执行。
var ErrBackgroundUnsupported = fmt.Errorf("background execution is unsupported")

// CancelSyncBash 终止仍在同步等待的 bash_run（按 tool_call_id）。
func (r *Registry) CancelSyncBash(sessionID, toolCallID string) error {
	if r == nil {
		return ErrSyncShellNotFound
	}
	if r.syncShells != nil {
		entry, ok := r.syncShells.get(toolCallID)
		if ok && entry != nil && entry.gate != nil {
			if sid := strings.TrimSpace(sessionID); sid != "" && entry.sessionID != "" && entry.sessionID != sid {
				return ErrSyncShellNotFound
			}
			if entry.job != nil && entry.job.toolName != "" && entry.job.toolName != "bash_run" {
				return ErrSyncShellNotBash
			}
			if entry.gate.RequestCancel() {
				return nil
			}
		}
	}
	return ErrSyncShellNotFound
}

// CancelAllSessionJobs 取消该 session 下全部同步 bash 和后台任务。
func (r *Registry) CancelAllSessionJobs(sessionID string) int {
	if r == nil {
		return 0
	}
	counts := r.SessionToolJobCounts(sessionID)
	ids := make([]string, 0, len(counts.RunningCallIDs)+len(counts.BackgroundCallIDs))
	ids = append(ids, counts.RunningCallIDs...)
	ids = append(ids, counts.BackgroundCallIDs...)
	n := 0
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if err := r.CancelSyncBash(sessionID, id); err == nil {
			n++
			continue
		}
		if err := r.cancelBackgroundJobByToolCall(sessionID, id); err == nil {
			n++
		}
	}
	return n
}

func (r *Registry) cancelBackgroundJobByToolCall(sessionID, toolCallID string) error {
	if r == nil || r.bgJobs == nil {
		return ErrSyncShellNotFound
	}
	job, ok := r.bgJobs.findRunningByToolCallID(sessionID, toolCallID)
	if !ok || job == nil {
		return ErrSyncShellNotFound
	}
	_ = job.cancelJob()
	job.waitDone(5 * time.Second)
	// collector 在 status=cancelled 时不会 notifyDone；此处一律回灌（幂等）。
	r.bgJobs.notifyJobDone(job)
	return nil
}
