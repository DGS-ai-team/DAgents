package notify

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/pending"
)

// SyncPlan 描述一次 Toast 同步：哪些该推送、同步后的去重快照。
type SyncPlan struct {
	ToPush   []pending.Entry
	NextLast map[string]pending.Entry
}

// PlanSync 计算 Toast 推送与去重状态。
// retainIDs：仍有待办但因 UI 焦点被抑制的 Agent，必须保留 last，避免取消焦点后重复弹窗。
func PlanSync(last map[string]pending.Entry, toastEntries []pending.Entry, retainIDs map[string]struct{}) SyncPlan {
	if last == nil {
		last = map[string]pending.Entry{}
	}
	next := make(map[string]pending.Entry, len(last)+len(toastEntries))
	var toPush []pending.Entry

	for _, e := range toastEntries {
		id := entryKey(e)
		if id == "" {
			continue
		}
		prev := last[id]
		if prev.Active() && prev.HITLItems == e.HITLItems && prev.HasUnread == e.HasUnread {
			next[id] = e
			continue
		}
		toPush = append(toPush, e)
		next[id] = e
	}
	for id := range retainIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := next[id]; ok {
			continue
		}
		if prev, ok := last[id]; ok {
			next[id] = prev
		}
	}
	return SyncPlan{ToPush: toPush, NextLast: next}
}

func entryKey(e pending.Entry) string {
	if id := strings.TrimSpace(e.SessionID); id != "" {
		return id
	}
	return strings.TrimSpace(e.AgentID)
}
