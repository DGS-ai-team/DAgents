package tools

import (
	"fmt"
	"strings"
	"sync"
)

// syncShellGate 允许 UI 在同步等待期间请求终止或转后台。
type syncShellGate struct {
	cancelOnce sync.Once
	bgOnce     sync.Once
	cancelCh   chan struct{}
	bgCh       chan struct{}
}

func newSyncShellGate() *syncShellGate {
	return &syncShellGate{
		cancelCh: make(chan struct{}),
		bgCh:     make(chan struct{}),
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

func (g *syncShellGate) RequestBackground() bool {
	if g == nil {
		return false
	}
	ok := false
	g.bgOnce.Do(func() {
		close(g.bgCh)
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

// ErrSyncShellNotFound 表示没有可控制的同步/后台 bash。
var ErrSyncShellNotFound = fmt.Errorf("sync bash tool call not found")

// ErrSyncShellNotBash 预留：非 bash 工具暂不支持。
var ErrSyncShellNotBash = fmt.Errorf("only bash_run supports cancel/background")

// CancelSyncBash 终止仍在同步等待或已转后台的 bash_run（按 tool_call_id）。
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
	return r.cancelBackgroundBashByToolCall(sessionID, toolCallID)
}

func (r *Registry) cancelBackgroundBashByToolCall(sessionID, toolCallID string) error {
	if r == nil || r.bgJobs == nil {
		return ErrSyncShellNotFound
	}
	job, ok := r.bgJobs.findRunningByToolCallID(sessionID, toolCallID)
	if !ok || job == nil {
		return ErrSyncShellNotFound
	}
	if job.toolName != "" && job.toolName != "bash_run" {
		return ErrSyncShellNotBash
	}
	_ = job.cancelJob()
	// collector 在 status=cancelled 时不会 notifyDone；此处一律回灌（幂等）。
	r.bgJobs.notifyJobDone(job)
	return nil
}

// BackgroundSyncBash 将仍在同步等待的 bash_run 转为后台任务。
func (r *Registry) BackgroundSyncBash(sessionID, toolCallID string) error {
	if r == nil || r.syncShells == nil {
		return ErrSyncShellNotFound
	}
	entry, ok := r.syncShells.get(toolCallID)
	if !ok || entry == nil || entry.gate == nil {
		return ErrSyncShellNotFound
	}
	if sid := strings.TrimSpace(sessionID); sid != "" && entry.sessionID != "" && entry.sessionID != sid {
		return ErrSyncShellNotFound
	}
	if entry.job != nil && entry.job.toolName != "" && entry.job.toolName != "bash_run" {
		return ErrSyncShellNotBash
	}
	if !entry.gate.RequestBackground() {
		return ErrSyncShellNotFound
	}
	return nil
}
