package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// NotificationState 为 session 通知 cursor 与 HITL 聚合（F-E13）。
type NotificationState struct {
	NotifySeq        int
	AckSeq           int
	HasUnread        bool
	HasPendingHITL   bool
	PendingHITLItems int
}

// OnStreamEvent Hub 发布后推进 notify_seq（Node 为真相源）。
func (m *Manager) OnStreamEvent(ev stream.Event) {
	if !ShouldBumpNotifySeq(ev) {
		return
	}
	m.bumpNotifySeq(ev.SessionID, ev.Seq)
}

func (m *Manager) bumpNotifySeq(sessionID string, seq int) {
	if seq <= 0 {
		return
	}
	if rt := m.getRuntime(sessionID); rt != nil {
		rt.bumpNotifySeq(seq)
		return
	}
	if m.store != nil {
		_ = m.store.BumpNotifySeq(context.Background(), sessionID, seq)
	}
}

// AckSession 更新 ack_seq；sseSeq 取各 Client 上报的最大值。
func (m *Manager) AckSession(ctx context.Context, sessionID string, sseSeq int) (*NotificationState, error) {
	sessionID = trimSessionID(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if sseSeq <= 0 {
		return nil, fmt.Errorf("sse_seq must be positive")
	}
	if rt := m.getRuntime(sessionID); rt != nil {
		return rt.ackSession(ctx, sseSeq)
	}
	if m.store == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	state, err := m.store.AckSession(ctx, sessionID, sseSeq)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	return notificationFromRuntimeState(state.Pending, state.NotifySeq, state.AckSeq), nil
}

// NotificationState 返回 session 通知态（内存 runtime 或 DB）。
func (m *Manager) NotificationState(sessionID string) NotificationState {
	if rt := m.getRuntime(sessionID); rt != nil {
		return rt.notificationState()
	}
	if m.store == nil {
		return NotificationState{}
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil || rec == nil {
		return NotificationState{}
	}
	pending := rec.RuntimeState.Pending
	if lifecycle, projected, projectionErr := m.loadLifecycleProjection(context.Background(), sessionID, rec.NodeID); projectionErr != nil {
		m.logger.Warn("load persisted turn lifecycle projection failed", "session_id", sessionID, "error", projectionErr)
	} else if projected {
		pending = pendingFromLifecycleSnapshot(lifecycle, nil)
	}
	st := notificationFromRuntimeState(pending, rec.RuntimeState.NotifySeq, rec.RuntimeState.AckSeq)
	if st == nil {
		return NotificationState{}
	}
	return *st
}

func notificationFromRuntimeState(pending *turn.PendingHITL, notifySeq, ackSeq int) *NotificationState {
	items := 0
	hasPending := pending != nil
	if hasPending {
		items = len(pending.Items)
		if items <= 0 {
			items = 1
		}
	}
	rs := store.RuntimeState{NotifySeq: notifySeq, AckSeq: ackSeq}
	return &NotificationState{
		NotifySeq:        notifySeq,
		AckSeq:           ackSeq,
		HasUnread:        rs.HasUnread(),
		HasPendingHITL:   hasPending,
		PendingHITLItems: items,
	}
}

func (r *runtime) bumpNotifySeq(seq int) {
	if seq <= 0 {
		return
	}
	r.mu.Lock()
	if seq > r.notifySeq {
		r.notifySeq = seq
	}
	r.mu.Unlock()
	r.persist(context.Background())
}

func (r *runtime) ackSession(ctx context.Context, sseSeq int) (*NotificationState, error) {
	r.mu.Lock()
	if sseSeq > r.ackSeq {
		r.ackSeq = sseSeq
	}
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	r.persist(ctx)
	return notificationFromRuntimeState(pending, notifySeq, ackSeq), nil
}

func (r *runtime) notificationState() NotificationState {
	r.mu.Lock()
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	st := notificationFromRuntimeState(pending, notifySeq, ackSeq)
	if st == nil {
		return NotificationState{}
	}
	return *st
}

func trimSessionID(id string) string {
	return strings.TrimSpace(id)
}
