package session

import (
	"context"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// StartIdleAutoCompressScanner 启动 idle session 维护扫描（压缩 + 卸内存，F-NM2）。
func (m *Manager) StartIdleAutoCompressScanner() {
	if m == nil || m.turn.IdleAutoCompressSeconds <= 0 {
		return
	}
	interval := time.Duration(m.turn.IdleAutoCompressPollSeconds) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}
	m.logger.Info("idle session maintenance scanner started",
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
			m.scanIdleSessionMaintenance(context.Background())
		}
	}
}

// scanIdleSessionMaintenance 对 eligible session 顺序执行可选压缩 → Release（F-NM2–NM5）。
func (m *Manager) scanIdleSessionMaintenance(ctx context.Context) {
	if m.turn.IdleAutoCompressSeconds <= 0 {
		return
	}
	threshold := time.Duration(m.turn.IdleAutoCompressSeconds) * time.Second
	minTokens := m.turn.IdleAutoCompressMinTokens
	now := time.Now()

	for _, sess := range m.ListActiveUser() {
		rt := m.getRuntime(sess.ID)
		if rt == nil {
			continue
		}
		m.tryRuntimeIdleMaintenance(ctx, rt, threshold, minTokens, now)
	}

	if m.store == nil {
		return
	}
	persisted, err := m.store.List(ctx)
	if err != nil {
		m.logger.Warn("idle session maintenance list sessions failed", "error", err)
		return
	}
	for _, sum := range persisted {
		m.mu.RLock()
		_, active := m.sessions[sum.AgentID]
		m.mu.RUnlock()
		if active {
			continue
		}
		m.tryPersistedIdleMaintenance(ctx, sum, threshold, minTokens, now)
	}
}

func (m *Manager) tryPersistedIdleMaintenance(ctx context.Context, sum store.Summary, threshold time.Duration, minTokens int, now time.Time) {
	rec, err := m.store.Load(ctx, sum.AgentID)
	if err != nil || rec == nil || len(rec.Messages) == 0 {
		return
	}
	if now.Sub(rec.UpdatedAt) < threshold {
		return
	}
	rt, err := m.ensureRuntime(sum.AgentID)
	if err != nil || rt == nil {
		m.logger.Warn("idle session maintenance restore session failed",
			"agent_id", sum.AgentID,
			"error", err,
		)
		return
	}
	m.tryRuntimeIdleMaintenance(ctx, rt, threshold, minTokens, now)
}

func (m *Manager) ensureRuntime(sessionID string) (*runtime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sessions[sessionID]; ok {
		return existing, nil
	}
	msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, historyRevision, inputBoxState, err := m.loadSessionData(sessionID)
	if err != nil {
		return nil, err
	}
	rt := newRuntime(sessionID, m.agentID, m.hub, m.llm, m.tools, m.policy, m.store, m.logger,
		msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, m.turn, m.triggerDelivery)
	rt.restoreInputBoxState(inputBoxState)
	rt.historyRevision = historyRevision
	rt.reconcileRestoredInputBox()
	m.sessions[sessionID] = rt
	m.attachUserChildTools(rt)
	rt.start(m.ctx)
	rt.orch.RunSessionLifecyclePhase(context.Background(), sessionID, "create")
	m.logger.Info("session restored for idle maintenance", "session_id", sessionID, "messages", len(msgs))
	return rt, nil
}

func (m *Manager) tryRuntimeIdleMaintenance(ctx context.Context, rt *runtime, threshold time.Duration, minTokens int, now time.Time) {
	if rt == nil || rt.isChildSession() {
		return
	}
	sessionID := rt.session.ID
	if rt.isBusyForMaintenance() {
		m.logger.Debug("idle session maintenance skipped busy", "session_id", sessionID)
		return
	}
	if !rt.meetsIdleThreshold(threshold, now) {
		return
	}
	if rt.eligibleForIdleAutoCompress(threshold, minTokens, now) {
		m.tryRuntimeIdleAutoCompress(ctx, rt, threshold, minTokens, now)
	}
	if m.getRuntime(sessionID) == nil {
		return
	}
	if rt.isBusyForMaintenance() {
		return
	}
	// 不在此重检 updated_at：压缩 persist 会刷新 updated_at（D31）。
	if released, err := m.Release(sessionID); err != nil {
		m.logger.Warn("idle session maintenance release failed", "session_id", sessionID, "error", err)
	} else if released {
		m.logger.Info("idle session maintenance evicted", "session_id", sessionID)
	}
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

// isBusyForMaintenance turn 进行中或队列非空时跳过；pending HITL 暂停视为 idle（可 evict，F-NM4）。
func (r *runtime) isBusyForMaintenance() bool {
	if r.queue.Len() > 0 {
		return true
	}
	return r.lifecycleExecutionBusy()
}

func (r *runtime) meetsIdleThreshold(idleThreshold time.Duration, now time.Time) bool {
	if r.isChildSession() || r.store == nil {
		return false
	}
	rec, err := r.store.Load(context.Background(), r.session.ID)
	if err != nil || rec == nil {
		return false
	}
	if len(rec.Messages) == 0 {
		return false
	}
	return now.Sub(rec.UpdatedAt) >= idleThreshold
}

func (r *runtime) eligibleForIdleAutoCompress(idleThreshold time.Duration, minTokens int, now time.Time) bool {
	if r.isChildSession() {
		return false
	}
	r.mu.Lock()
	idleApplied := r.idleAutoCompressApplied
	msgs := append([]llm.Message(nil), r.messages...)
	queuePending := r.queue.Len() > 0
	if r.inputBox != nil {
		queuePending = queuePending || r.inputBox.Len() > 0
	}
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	if idleApplied {
		return false
	}
	busy := r.lifecycleExecutionBusy() || queuePending || pending != nil
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
