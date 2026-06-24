package session

import (
	"context"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// StartIdleAutoCompressScanner 启动后台扫描；IdleAutoCompressSeconds <= 0 时不启动。
func (m *Manager) StartIdleAutoCompressScanner() {
	if m == nil || m.turn.IdleAutoCompressSeconds <= 0 {
		return
	}
	interval := time.Duration(m.turn.IdleAutoCompressPollSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	m.logger.Info("idle auto compress scanner started",
		"idle_seconds", m.turn.IdleAutoCompressSeconds,
		"poll_seconds", int(interval/time.Second),
		"min_tokens", m.turn.IdleAutoCompressMinTokens,
	)
	go m.runIdleAutoCompressLoop(interval)
}

func (m *Manager) runIdleAutoCompressLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.scanIdleAutoCompress(context.Background())
		}
	}
}

func (m *Manager) scanIdleAutoCompress(ctx context.Context) {
	if m.turn.IdleAutoCompressSeconds <= 0 {
		return
	}
	threshold := time.Duration(m.turn.IdleAutoCompressSeconds) * time.Second
	now := time.Now()

	for _, sess := range m.ListActiveUser() {
		rt := m.getRuntime(sess.ID)
		if rt == nil {
			continue
		}
		m.tryRuntimeIdleAutoCompress(ctx, rt, threshold, m.turn.IdleAutoCompressMinTokens, now)
	}

	if m.store == nil {
		return
	}
	persisted, err := m.store.List(ctx)
	if err != nil {
		m.logger.Warn("idle auto compress list sessions failed", "error", err)
		return
	}
	for _, sum := range persisted {
		m.mu.RLock()
		_, active := m.sessions[sum.SessionID]
		m.mu.RUnlock()
		if active {
			continue
		}
		m.tryPersistedIdleAutoCompress(ctx, sum, threshold, now)
	}
}

func (m *Manager) tryPersistedIdleAutoCompress(ctx context.Context, sum store.Summary, threshold time.Duration, now time.Time) {
	rec, err := m.store.Load(ctx, sum.SessionID)
	if err != nil || rec == nil {
		return
	}
	if rec.RuntimeState.IdleAutoCompressApplied {
		return
	}
	if rec.RuntimeState.Pending != nil || len(rec.Messages) == 0 {
		return
	}
	if now.Sub(rec.UpdatedAt) < threshold {
		return
	}
	if !idleAutoCompressMeetsMinTokens(rec.Messages, m.turn.IdleAutoCompressMinTokens) {
		return
	}
	rt, err := m.ensureRuntime(sum.SessionID)
	if err != nil || rt == nil {
		m.logger.Warn("idle auto compress restore session failed",
			"session_id", sum.SessionID,
			"error", err,
		)
		return
	}
	m.tryRuntimeIdleAutoCompress(ctx, rt, threshold, m.turn.IdleAutoCompressMinTokens, now)
}

func (m *Manager) ensureRuntime(sessionID string) (*runtime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sessions[sessionID]; ok {
		return existing, nil
	}
	msgs, loaded, pending, loopCount, hookStore, idleMarked, err := m.loadSessionData(sessionID)
	if err != nil {
		return nil, err
	}
	rt := newRuntime(sessionID, m.agentID, m.hub, m.llm, m.tools, m.policy, m.store, m.logger,
		msgs, loaded, pending, loopCount, hookStore, idleMarked, m.turn, m.triggerDelivery)
	m.sessions[sessionID] = rt
	m.attachUserChildTools(rt)
	rt.start(m.ctx)
	rt.orch.RunSessionLifecyclePhase(context.Background(), sessionID, "create")
	m.logger.Info("session restored for idle auto compress", "session_id", sessionID, "messages", len(msgs))
	return rt, nil
}

func (m *Manager) tryRuntimeIdleAutoCompress(ctx context.Context, rt *runtime, threshold time.Duration, minTokens int, now time.Time) {
	if !rt.eligibleForIdleAutoCompress(threshold, minTokens, now) {
		return
	}
	result := rt.compressContext(ctx)
	switch result.Status {
	case "applied", "noop":
		rt.markIdleAutoCompressApplied()
		m.logger.Info("idle auto compress completed",
			"session_id", rt.session.ID,
			"status", result.Status,
			"compressed_messages", result.CompressedMessageCount,
		)
	case "busy", "in_progress":
		return
	default:
		m.logger.Warn("idle auto compress failed",
			"session_id", rt.session.ID,
			"status", result.Status,
		)
	}
}

func (r *runtime) eligibleForIdleAutoCompress(idleThreshold time.Duration, minTokens int, now time.Time) bool {
	if r.isChildSession() {
		return false
	}
	r.mu.Lock()
	if r.idleAutoCompressApplied {
		r.mu.Unlock()
		return false
	}
	busy := r.state != turn.StateIdle || r.pending != nil || r.queue.Len() > 0
	msgs := append([]llm.Message(nil), r.messages...)
	r.mu.Unlock()
	if busy {
		return false
	}
	if r.store == nil {
		return false
	}
	rec, err := r.store.Load(context.Background(), r.session.ID)
	if err != nil || rec == nil {
		return false
	}
	if len(msgs) == 0 || now.Sub(rec.UpdatedAt) < idleThreshold {
		return false
	}
	return idleAutoCompressMeetsMinTokens(msgs, minTokens)
}

func idleAutoCompressMeetsMinTokens(messages []llm.Message, minTokens int) bool {
	if minTokens <= 0 {
		return true
	}
	return llm.EstimateMessageTokens(messages) >= minTokens
}

func (r *runtime) markIdleAutoCompressApplied() {
	r.mu.Lock()
	r.idleAutoCompressApplied = true
	r.mu.Unlock()
	r.persist(context.Background())
}

func (r *runtime) clearIdleAutoCompressMark() {
	r.mu.Lock()
	if !r.idleAutoCompressApplied {
		r.mu.Unlock()
		return
	}
	r.idleAutoCompressApplied = false
	r.mu.Unlock()
	r.persist(context.Background())
}
