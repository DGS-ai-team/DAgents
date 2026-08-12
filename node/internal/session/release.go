package session

import (
	"context"
	"fmt"
	"strings"
)

// Release 将 session 卸出内存：persist → stop consumer → 移出 map；**不**删除 SQLite（F-NM1）。
func (m *Manager) Release(sessionID string) (bool, error) {
	if m == nil {
		return false, fmt.Errorf("manager is nil")
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return false, fmt.Errorf("session_id is required")
	}
	m.mu.Lock()
	rt, ok := m.sessions[sid]
	m.mu.Unlock()
	if !ok {
		return false, nil
	}
	if rt.isChildSession() {
		return false, fmt.Errorf("cannot release child session")
	}
	rt.persist(context.Background())
	m.mu.Lock()
	rt, ok = m.sessions[sid]
	if !ok {
		m.mu.Unlock()
		return false, nil
	}
	delete(m.sessions, sid)
	m.mu.Unlock()
	rt.stop()
	m.logger.Info("session released from memory", "session_id", sid)
	if m.OnReleased != nil {
		m.OnReleased(sid)
	}
	return true, nil
}
