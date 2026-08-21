package session

// UpgradeReadiness 供 Shell apply 前查询 Node 是否可安全升级（F-ND1）。
type UpgradeReadiness struct {
	Ready            bool     `json:"ready"`
	HasActiveTurn    bool     `json:"has_active_turn"`
	ActiveTurnCount  int      `json:"active_turn_count"`
	ActiveSessionIDs []string `json:"active_session_ids,omitempty"`
}

// UpgradeReadiness 扫描内存中全部 session runtime；任一 turn 非 idle 或 pending HITL 则不可升级。
func (m *Manager) UpgradeReadiness() UpgradeReadiness {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := UpgradeReadiness{Ready: true}
	for sid, rt := range m.sessions {
		if !rt.isBusyForUpgrade() {
			continue
		}
		out.HasActiveTurn = true
		out.ActiveTurnCount++
		out.ActiveSessionIDs = append(out.ActiveSessionIDs, sid)
	}
	out.Ready = !out.HasActiveTurn
	return out
}

func (r *runtime) isBusyForUpgrade() bool {
	if r == nil || r.turnCoordinator == nil {
		return false
	}
	return r.turnCoordinator.Snapshot().HasActiveTurn
}
