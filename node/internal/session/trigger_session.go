package session

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TriggerSessionResolver 供 trigger fire 解析 latest_active 会话。
type TriggerSessionResolver interface {
	ResolveLatestActiveUserSessionID(ctx context.Context) (string, error)
}

// ResolveLatestActiveUserSessionID 在内存活跃用户会话中选 updated_at 最新者。
func (m *Manager) ResolveLatestActiveUserSessionID(ctx context.Context) (string, error) {
	active := m.ListActiveUser()
	if len(active) == 0 {
		return "", fmt.Errorf("no active user sessions")
	}
	bestID := ""
	var bestTime time.Time
	for _, sess := range active {
		if sess == nil {
			continue
		}
		updated := time.Time{}
		if m.store != nil {
			rec, err := m.store.Load(ctx, sess.ID)
			if err != nil {
				return "", err
			}
			if rec != nil {
				updated = rec.UpdatedAt
			}
		}
		if bestID == "" || updated.After(bestTime) || (updated.Equal(bestTime) && strings.Compare(sess.ID, bestID) > 0) {
			bestID = sess.ID
			bestTime = updated
		}
	}
	if bestID == "" {
		return "", fmt.Errorf("no active user sessions")
	}
	return bestID, nil
}
