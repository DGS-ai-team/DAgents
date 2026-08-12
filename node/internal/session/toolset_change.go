package session

import (
	"context"
	"strings"
)

// ToolsetChangedNotice 为工具集缩水时推送给用户的可见提示。
const ToolsetChangedNotice = "工具集已变更：部分工具已禁用，进行中的工具调用已中断。"

// ToolsetChangedInterruptMessage 写入历史的 tool result 打断文案。
const ToolsetChangedInterruptMessage = "工具集已变更，已中断该工具调用。"

// NotifyToolsetChanged 在 enabled_groups 缩水时打断 pending HITL / 在途 turn，并推送 system_notice。
// 须在 Release/reload 之前调用，以便打断结果写入持久化历史。
func (m *Manager) NotifyToolsetChanged(sessionID string) {
	if m == nil {
		return
	}
	rt := m.getRuntime(strings.TrimSpace(sessionID))
	if rt == nil {
		return
	}
	rt.notifyToolsetChanged()
}

func (r *runtime) notifyToolsetChanged() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.pending != nil && r.orch != nil {
		pending := r.pending
		r.pending = nil
		r.orch.InterruptPendingWithReason(
			r.session.ID,
			&r.messages,
			pending,
			ToolsetChangedInterruptMessage,
			map[string]any{"interrupted_by_toolset_change": true},
		)
	}
	if r.orch != nil {
		_ = r.orch.RepairUnrespondedToolCalls(r.session.ID, &r.messages)
	}
	r.mu.Unlock()

	_ = r.cancelTurn()

	if r.hub != nil {
		r.hub.Publish(r.session.ID, "system_notice", map[string]any{
			"message": ToolsetChangedNotice,
		})
	}
	r.persist(context.Background())
	r.logger.Info("toolset changed: interrupted pending tools",
		"session_id", r.session.ID,
	)
}
