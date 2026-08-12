// Package pending 维护 Shell 侧 Agent 级待办表（HITL + 未读回复，F-E2/E3/E10/E13）。
package pending

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 为单个 Agent 的聚合待办态（同 Agent 一条通知态）。
type Entry struct {
	AgentID     string
	DisplayName string
	HITLItems   int
	HasUnread   bool
	EventType   string
	UpdatedAt   time.Time

	// SessionID 与 AgentID 同源（历史字段，菜单/焦点键仍可用）。
	SessionID string
}

// Active 该 Agent 是否仍有待办。
func (e Entry) Active() bool {
	return e.HITLItems > 0 || e.HasUnread
}

func (e Entry) itemCount() int {
	n := e.HITLItems
	if e.HasUnread {
		n++
	}
	if n <= 0 {
		return 0
	}
	return n
}

func (e Entry) id() string {
	if id := strings.TrimSpace(e.AgentID); id != "" {
		return id
	}
	return strings.TrimSpace(e.SessionID)
}

func (e Entry) displayLabel() string {
	if name := strings.TrimSpace(e.DisplayName); name != "" {
		return name
	}
	return shortAgentID(e.id())
}

// SummaryLabel 为单 Agent 菜单/Toast 摘要。
func (e Entry) SummaryLabel() string {
	label := e.displayLabel()
	switch {
	case e.HITLItems > 0 && e.HasUnread:
		if e.HITLItems > 1 {
			return fmt.Sprintf("%s · %d 项 HITL + 新回复", label, e.HITLItems)
		}
		return label + " · HITL + 新回复"
	case e.HITLItems > 1:
		return fmt.Sprintf("%s · %d 项待处理", label, e.HITLItems)
	case e.HITLItems == 1:
		return label + " · 待处理"
	case e.HasUnread:
		return label + " · 新回复"
	default:
		return label
	}
}

// Summary 为托盘展示的待办聚合。
type Summary struct {
	AgentCount   int
	SessionCount int // 与 AgentCount 同值（历史字段）
	ItemCount    int
	Label        string
}

// Store 为 agent_id → 待办条目（由 Node GET /v1/agents 同步）。
type Store struct {
	mu      sync.RWMutex
	byAgent map[string]Entry
}

// NewStore 构造空待办表。
func NewStore() *Store {
	return &Store{
		byAgent: make(map[string]Entry),
	}
}

// ReplaceFromNode 用 Node 同步结果替换本地待办表；有变化时返回 true。
func (s *Store) ReplaceFromNode(incoming map[string]Entry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mapsEqual(s.byAgent, incoming) {
		return false
	}
	next := make(map[string]Entry, len(incoming))
	for id, e := range incoming {
		if e.Active() {
			next[id] = e
		}
	}
	s.byAgent = next
	return true
}

func mapsEqual(a, b map[string]Entry) bool {
	if len(a) != len(b) {
		return false
	}
	for id, ea := range a {
		eb, ok := b[id]
		if !ok || !entriesEqual(ea, eb) {
			return false
		}
	}
	return true
}

func entriesEqual(a, b Entry) bool {
	return a.id() == b.id() &&
		a.DisplayName == b.DisplayName &&
		a.HITLItems == b.HITLItems &&
		a.HasUnread == b.HasUnread &&
		a.EventType == b.EventType
}

// Summary 返回当前待办聚合。
func (s *Store) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.byAgent) == 0 {
		return Summary{}
	}
	items := 0
	for _, e := range s.byAgent {
		items += e.itemCount()
	}
	n := len(s.byAgent)
	label := fmt.Sprintf("%d 个 Agent 待处理", n)
	if n == 1 {
		e := s.activeEntriesLocked()[0]
		label = e.SummaryLabel()
	} else if items > n {
		label = fmt.Sprintf("%d 个 Agent · %d 项待处理", n, items)
	}
	return Summary{
		AgentCount:   n,
		SessionCount: n,
		ItemCount:    items,
		Label:        label,
	}
}

// Entries 返回按更新时间倒序的快照。
func (s *Store) Entries() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeEntriesLocked()
}

// HasPendingHITL 是否仍有任一 Agent 处于 HITL 待办。
func (s *Store) HasPendingHITL() bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.byAgent {
		if e.HITLItems > 0 {
			return true
		}
	}
	return false
}

func (s *Store) activeEntriesLocked() []Entry {
	out := make([]Entry, 0, len(s.byAgent))
	for _, e := range s.byAgent {
		if e.Active() {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].id() < out[j].id()
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func shortAgentID(id string) string {
	id = trim(id)
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…"
}

func trim(s string) string {
	return strings.TrimSpace(s)
}
