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

// FocusHITL 深链是否带 focus=hitl。
func (e Entry) FocusHITL() bool {
	return e.HITLItems > 0
}

// Summary 为托盘展示的待办聚合。
type Summary struct {
	SessionCount int
	ItemCount    int
	Label        string
}

// Store 为 session_id → 待办条目。
type Store struct {
	mu        sync.RWMutex
	bySession map[string]Entry
	consumed  map[string]struct{}
}

// NewStore 构造空待办表。
func NewStore() *Store {
	return &Store{
		bySession: make(map[string]Entry),
		consumed:  make(map[string]struct{}),
	}
}

// MarkHITL 标记 session 有 HITL 待办。
func (s *Store) MarkHITL(sessionID string, itemCount int, eventType string) {
	sessionID = trim(sessionID)
	if sessionID == "" {
		return
	}
	if itemCount <= 0 {
		itemCount = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.bySession[sessionID]
	e.SessionID = sessionID
	e.HITLItems = itemCount
	e.EventType = trim(eventType)
	e.UpdatedAt = time.Now()
	s.bySession[sessionID] = e
	delete(s.consumed, sessionID)
}

// MarkUnread 标记 session 有未读 assistant 回复（F-E13）。
func (s *Store) MarkUnread(sessionID string) {
	sessionID = trim(sessionID)
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.consumed[sessionID]; ok {
		return
	}
	e := s.bySession[sessionID]
	e.SessionID = sessionID
	e.HasUnread = true
	e.UpdatedAt = time.Now()
	s.bySession[sessionID] = e
	s.pruneLocked(sessionID)
}

// MarkConsumed 用户已通过 Shell 打开 UI 消费该 session（清除未读，F-E13）。
func (s *Store) MarkConsumed(sessionID string) {
	sessionID = trim(sessionID)
	if sessionID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consumed[sessionID] = struct{}{}
	e, ok := s.bySession[sessionID]
	if !ok {
		return
	}
	e.HasUnread = false
	s.bySession[sessionID] = e
	s.pruneLocked(sessionID)
}

// ClearHITL 清除 session 的 HITL 待办。
func (s *Store) ClearHITL(sessionID string) bool {
	sessionID = trim(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.bySession[sessionID]
	if !ok || e.HITLItems <= 0 {
		return false
	}
	e.HITLItems = 0
	s.bySession[sessionID] = e
	s.pruneLocked(sessionID)
	return true
}

// ClearSession 清除 session 全部待办。
func (s *Store) ClearSession(sessionID string) bool {
	sessionID = trim(sessionID)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.bySession[sessionID]; !ok {
		return false
	}
	delete(s.bySession, sessionID)
	delete(s.consumed, sessionID)
	return true
}

// ClearHITLForSessions 批量清除 HITL（F-E10 sync）。
func (s *Store) ClearHITLForSessions(sessionIDs map[string]struct{}) {
	if len(sessionIDs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range sessionIDs {
		e, ok := s.bySession[id]
		if !ok || e.HITLItems <= 0 {
			continue
		}
		e.HITLItems = 0
		s.bySession[id] = e
		s.pruneLocked(id)
	}
}

func (s *Store) pruneLocked(sessionID string) {
	e, ok := s.bySession[sessionID]
	if !ok || !e.Active() {
		delete(s.bySession, sessionID)
	}
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
