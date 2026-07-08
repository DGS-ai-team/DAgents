// Package pending 维护 Shell 侧 session 级待办表（HITL + 未读回复，F-E2/E3/E10/E13）。
package pending

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Entry 为单个 session 的聚合待办态（D17：同 session 一条通知态）。
type Entry struct {
	SessionID string
	HITLItems int
	HasUnread bool
	EventType string
	UpdatedAt time.Time
}

// Active 该 session 是否仍有待办。
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

// SummaryLabel 为单 session 菜单/Toast 摘要。
func (e Entry) SummaryLabel() string {
	switch {
	case e.HITLItems > 0 && e.HasUnread:
		if e.HITLItems > 1 {
			return fmt.Sprintf("%s · %d 项 HITL + 新回复", shortSessionID(e.SessionID), e.HITLItems)
		}
		return shortSessionID(e.SessionID) + " · HITL + 新回复"
	case e.HITLItems > 1:
		return fmt.Sprintf("%s · %d 项待处理", shortSessionID(e.SessionID), e.HITLItems)
	case e.HITLItems == 1:
		return shortSessionID(e.SessionID) + " · 待处理"
	case e.HasUnread:
		return shortSessionID(e.SessionID) + " · 新回复"
	default:
		return shortSessionID(e.SessionID)
	}
}

// Summary 为托盘展示的待办聚合。
type Summary struct {
	SessionCount int
	ItemCount    int
	Label        string
}

// Store 为 session_id → 待办条目（由 Node GET /v1/sessions 同步）。
type Store struct {
	mu        sync.RWMutex
	bySession map[string]Entry
}

// NewStore 构造空待办表。
func NewStore() *Store {
	return &Store{
		bySession: make(map[string]Entry),
	}
}

// ReplaceFromNode 用 Node 同步结果替换本地待办表；有变化时返回 true。
func (s *Store) ReplaceFromNode(incoming map[string]Entry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mapsEqual(s.bySession, incoming) {
		return false
	}
	next := make(map[string]Entry, len(incoming))
	for id, e := range incoming {
		if e.Active() {
			next[id] = e
		}
	}
	s.bySession = next
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
	return a.SessionID == b.SessionID &&
		a.HITLItems == b.HITLItems &&
		a.HasUnread == b.HasUnread &&
		a.EventType == b.EventType
}

// Summary 返回当前待办聚合。
func (s *Store) Summary() Summary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.bySession) == 0 {
		return Summary{}
	}
	items := 0
	for _, e := range s.bySession {
		items += e.itemCount()
	}
	n := len(s.bySession)
	label := fmt.Sprintf("%d 个 session 待处理", n)
	if n == 1 {
		e := s.activeEntriesLocked()[0]
		label = e.SummaryLabel()
	} else if items > n {
		label = fmt.Sprintf("%d 个 session · %d 项待处理", n, items)
	}
	return Summary{
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

func (s *Store) activeEntriesLocked() []Entry {
	out := make([]Entry, 0, len(s.bySession))
	for _, e := range s.bySession {
		if e.Active() {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].SessionID < out[j].SessionID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out
}

func shortSessionID(id string) string {
	id = trim(id)
	if len(id) <= 12 {
		return id
	}
	return id[:8] + "…"
}

func trim(s string) string {
	return strings.TrimSpace(s)
}
