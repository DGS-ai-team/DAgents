// Package uifocus 记录 Web UI 当前聚焦的 Agent（F-E9），用于抑制同 Agent 新 Toast。
// 线协议字段仍为 session_id，值为 Agent 实例 UUID。
package uifocus

import (
	"strings"
	"sync"
	"time"
)

// DefaultTTL 为 focus 上报默认有效期；Web UI 应周期性续期。
const DefaultTTL = 90 * time.Second

// Store 线程安全地保存 UI 聚焦 session 及过期时间。
type Store struct {
	mu        sync.Mutex
	sessionID string
	expiresAt time.Time
}

// NewStore 构造 focus 存储。
func NewStore() *Store {
	return &Store{}
}

// Report 设置或清除聚焦 session；sessionID 为空表示清除。
func (s *Store) Report(sessionID string, ttl time.Duration) {
	if s == nil {
		return
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	sessionID = strings.TrimSpace(sessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if sessionID == "" {
		s.sessionID = ""
		s.expiresAt = time.Time{}
		return
	}
	s.sessionID = sessionID
	s.expiresAt = time.Now().Add(ttl)
}

// IsFocused 判断 session 是否处于 UI 聚焦抑制窗口内。
func (s *Store) IsFocused(sessionID string) bool {
	if s == nil {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID == "" || s.sessionID != sessionID {
		return false
	}
	return time.Now().Before(s.expiresAt)
}

// FocusedSession 返回当前聚焦 session（测试用）；过期时返回空串。
func (s *Store) FocusedSession() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionID == "" || time.Now().After(s.expiresAt) {
		return ""
	}
	return s.sessionID
}
