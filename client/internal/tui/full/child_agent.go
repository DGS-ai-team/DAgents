package full

import (
	"fmt"
	"strings"
	"sync"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

// childAgentEntry 跟踪单个子 Agent 的 TUI 展示状态。
type childAgentEntry struct {
	Purpose          string
	AwaitingApproval bool
}

// childAgentTracker 维护父 session 下活跃子 Agent 计数（SSE 驱动，HTTP 可对齐）。
type childAgentTracker struct {
	mu       sync.Mutex
	entries  map[string]*childAgentEntry
	suppress *tuishared.ChildLifecycleSuppress
}

func newChildAgentTracker() *childAgentTracker {
	return &childAgentTracker{
		entries:  make(map[string]*childAgentEntry),
		suppress: tuishared.NewChildLifecycleSuppress(),
	}
}

func (t *childAgentTracker) reset() {
	t.mu.Lock()
	t.entries = make(map[string]*childAgentEntry)
	t.mu.Unlock()
	if t.suppress != nil {
		t.suppress.Reset()
	}
}

func (t *childAgentTracker) onCreated(data map[string]any) {
	id := strings.TrimSpace(fmt.Sprint(data["child_session_id"]))
	if id == "" {
		return
	}
	t.mu.Lock()
	t.entries[id] = &childAgentEntry{
		Purpose: strings.TrimSpace(fmt.Sprint(data["purpose"])),
	}
	t.mu.Unlock()
}

func (t *childAgentTracker) onFinished(childID string) {
	childID = strings.TrimSpace(childID)
	if childID == "" {
		return
	}
	t.mu.Lock()
	delete(t.entries, childID)
	t.mu.Unlock()
}

func (t *childAgentTracker) setAwaitingApproval(childID string, on bool) {
	childID = strings.TrimSpace(childID)
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.entries[childID]
	if !ok {
		if !on {
			return
		}
		e = &childAgentEntry{}
		t.entries[childID] = e
	}
	e.AwaitingApproval = on
}

func (t *childAgentTracker) counts() (active, pendingApproval int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, e := range t.entries {
		active++
		if e.AwaitingApproval {
			pendingApproval++
		}
	}
	return active, pendingApproval
}

func (t *childAgentTracker) awaitingApprovalMap() map[string]bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]bool)
	for id, e := range t.entries {
		if e != nil && e.AwaitingApproval {
			out[id] = true
		}
	}
	return out
}

func (t *childAgentTracker) replaceFromAPI(items []nodeapi.ChildAgentListItem) {
	t.mu.Lock()
	defer t.mu.Unlock()
	next := make(map[string]*childAgentEntry, len(items))
	for _, it := range items {
		next[it.ChildSessionID] = &childAgentEntry{
			Purpose: it.Purpose,
		}
	}
	t.entries = next
}

func (t *childAgentTracker) noteToolCall(data map[string]any) {
	if t.suppress != nil {
		t.suppress.NoteToolCallEvent(data)
	}
}

func (t *childAgentTracker) noteToolResult(data map[string]any) {
	if t.suppress == nil {
		return
	}
	name := strings.TrimSpace(fmt.Sprint(data["tool_name"]))
	if name == "" {
		name = strings.TrimSpace(fmt.Sprint(data["name"]))
	}
	content := strings.TrimSpace(fmt.Sprint(data["content"]))
	if content == "" {
		content = strings.TrimSpace(fmt.Sprint(data["output"]))
	}
	t.suppress.NoteToolResult(name, content)
}

func (t *childAgentTracker) shouldSuppressLifecycle(childID, eventType string) bool {
	if t.suppress == nil {
		return false
	}
	return t.suppress.ShouldSuppressLifecycle(childID, eventType)
}
