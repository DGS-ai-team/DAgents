package tools

import (
	"fmt"
	"strings"
	"sync"
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

// ToolJobCounts 为某 session 当前可由 UI 终止的同步 bash 数量与调用 ID。
type ToolJobCounts struct {
	Running        int      `json:"running"`
	RunningCallIDs []string `json:"running_call_ids"`
}

// SessionToolJobCounts 返回指定 session 的工具执行计数。
func (r *Registry) SessionToolJobCounts(sessionID string) ToolJobCounts {
	if r == nil {
		return ToolJobCounts{}
	}
	return ToolJobCounts{
		Running:        r.syncShells.countSession(sessionID),
		RunningCallIDs: r.syncShells.callIDsSession(sessionID),
	}
}

// ErrSyncShellNotFound 表示没有可控制的同步 bash。
var ErrSyncShellNotFound = fmt.Errorf("sync bash tool call not found")

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
			if entry.gate.RequestCancel() {
				return nil
			}
		}
	}
	return ErrSyncShellNotFound
}

// CancelAllSessionJobs 取消该 session 下全部同步 bash。
func (r *Registry) CancelAllSessionJobs(sessionID string) int {
	if r == nil {
		return 0
	}
	counts := r.SessionToolJobCounts(sessionID)
	ids := counts.RunningCallIDs
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
		}
	}
	return n
}
