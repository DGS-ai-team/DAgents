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
	seq := ev.AgentSeq
	if seq <= 0 {
		// Internal synthetic events in tests may not have a Hub cursor. The
		// production Hub always supplies AgentSeq for replayable Agent events.
		seq = ev.Seq
	}
	m.bumpNotifySeq(ev.AgentID, seq)
	m.publishNotificationChanged(ev.AgentID)
}

// publishNotificationChanged publishes the complete notification projection
// after the durable cursor/pending projection has changed. Shells consume this
// event directly; they only use GET /v1/agents for initial/reconnect hydrate.
// It is intentionally a separate event from the user-facing turn event so the
// tray does not need to infer unread/HITL state from tool semantics.
func (m *Manager) publishNotificationChanged(sessionID string) {
	if m == nil || m.hub == nil || trimSessionID(sessionID) == "" {
		return
	}
	state := m.NotificationState(sessionID)
	m.hub.Publish(sessionID, "notification_changed", map[string]any{
		"notify_seq":         state.NotifySeq,
		"ack_seq":            state.AckSeq,
		"has_unread":         state.HasUnread,
		"has_pending_hitl":   state.HasPendingHITL,
		"pending_hitl_items": state.PendingHITLItems,
	})
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

// AckSession 更新 ack_seq；agentSeq 取各 Client 上报的最大 Agent 游标。
func (m *Manager) AckSession(ctx context.Context, sessionID string, agentSeq int) (*NotificationState, error) {
	sessionID = trimSessionID(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if agentSeq <= 0 {
		return nil, fmt.Errorf("agent_seq must be positive")
	}
	if rt := m.getRuntime(sessionID); rt != nil {
		state, err := rt.ackSession(ctx, agentSeq)
		if err == nil {
			m.publishNotificationChanged(sessionID)
		}
		return state, err
	}
	if m.store == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	state, err := m.store.AckSession(ctx, sessionID, agentSeq)
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	var pending *turn.PendingHITL
	if lifecycle, projected, _, projectionErr := m.loadLifecycleProjection(context.Background(), sessionID, ""); projectionErr != nil {
		m.logger.Warn("load persisted turn lifecycle projection failed", "session_id", sessionID, "error", projectionErr)
	} else if projected {
		pending = pendingFromLifecycleSnapshot(lifecycle)
	}
	out := notificationFromRuntimeState(pending, state.NotifySeq, state.AckSeq)
	m.publishNotificationChanged(sessionID)
	return out, nil
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
	var pending *turn.PendingHITL
	if lifecycle, projected, _, projectionErr := m.loadLifecycleProjection(context.Background(), sessionID, rec.NodeID); projectionErr != nil {
		m.logger.Warn("load persisted turn lifecycle projection failed", "session_id", sessionID, "error", projectionErr)
	} else if projected {
		pending = pendingFromLifecycleSnapshot(lifecycle)
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

func (r *runtime) ackSession(ctx context.Context, agentSeq int) (*NotificationState, error) {
	r.mu.Lock()
	if agentSeq > r.ackSeq {
		r.ackSeq = agentSeq
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
