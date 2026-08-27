package store

import (
	"context"
	"fmt"
	"strings"
)

// BumpNotifySeq 将 Agent 的 notify_seq 推进至 max(当前, seq)；无记录时忽略。
func (s *SQLiteStore) BumpNotifySeq(ctx context.Context, agentID string, seq int) error {
	if s == nil || seq <= 0 {
		return nil
	}
	rec, err := s.Load(ctx, strings.TrimSpace(agentID))
	if err != nil {
		return err
	}
	if rec == nil {
		return nil
	}
	if seq <= rec.RuntimeState.NotifySeq {
		return nil
	}
	rec.RuntimeState.NotifySeq = seq
	return s.Save(ctx, *rec)
}

// AckSession 更新 ack_seq 并返回更新后的 RuntimeState；Agent 不存在时返回 nil, nil。
func (s *SQLiteStore) AckSession(ctx context.Context, agentID string, agentSeq int) (*RuntimeState, error) {
	if s == nil {
		return nil, fmt.Errorf("store is nil")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	if agentSeq <= 0 {
		return nil, fmt.Errorf("agent_seq must be positive")
	}
	rec, err := s.Load(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, nil
	}
	if agentSeq > rec.RuntimeState.AckSeq {
		rec.RuntimeState.AckSeq = agentSeq
		if err := s.Save(ctx, *rec); err != nil {
			return nil, err
		}
	}
	state := rec.RuntimeState
	return &state, nil
}
