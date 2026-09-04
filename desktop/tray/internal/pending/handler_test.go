package pending

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
)

func TestSyncFromAgents(t *testing.T) {
	store := NewStore()
	changed := SyncFromAgents(store, []nodeclient.AgentSummary{
		{AgentID: "agt-hitl", DisplayName: "审查", HasPendingHITL: true, PendingHITLItems: 2},
		{AgentID: "agt-unread", HasUnread: true, NotifySeq: 10, AckSeq: 5},
		{AgentID: "agt-clear", HasUnread: false, HasPendingHITL: false},
	})
	if !changed {
		t.Fatal("expected initial sync to change store")
	}
	sum := store.Summary()
	if sum.AgentCount != 2 || sum.ItemCount != 3 {
		t.Fatalf("summary = %+v", sum)
	}
	entries := store.Entries()
	if len(entries) < 1 || entries[0].DisplayName == "" && entries[0].AgentID != "agt-unread" {
		// 至少有一条带 display name 的 HITL 项
	}
	found := false
	for _, e := range entries {
		if e.AgentID == "agt-hitl" && e.DisplayName == "审查" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected display name on hitl entry: %+v", entries)
	}

	if SyncFromAgents(store, []nodeclient.AgentSummary{
		{AgentID: "agt-hitl", DisplayName: "审查", HasPendingHITL: true, PendingHITLItems: 2},
		{AgentID: "agt-unread", HasUnread: true, NotifySeq: 10, AckSeq: 5},
	}) {
		t.Fatal("identical sync should not report change")
	}

	changed = SyncFromAgents(store, []nodeclient.AgentSummary{
		{AgentID: "agt-unread", HasUnread: false, AckSeq: 10, NotifySeq: 10},
	})
	if !changed {
		t.Fatal("expected change when unread cleared")
	}
	if store.Summary().AgentCount != 0 {
		t.Fatalf("expected empty store, got %+v", store.Summary())
	}
}

func TestApplyNotificationChanged(t *testing.T) {
	store := NewStore()
	ev := nodeclient.StreamEvent{
		Type:    "notification_changed",
		AgentID: "a1",
		Data: map[string]any{
			"has_pending_hitl":   true,
			"pending_hitl_items": float64(2),
			"has_unread":         true,
		},
	}
	if !ApplyNotificationChanged(store, ev) {
		t.Fatal("notification_changed should update the store")
	}
	if got := store.Summary(); got.ItemCount != 3 {
		t.Fatalf("summary = %+v", got)
	}
	ev.Data["has_pending_hitl"] = false
	ev.Data["pending_hitl_items"] = float64(0)
	ev.Data["has_unread"] = false
	if !ApplyNotificationChanged(store, ev) {
		t.Fatal("clearing notification should update the store")
	}
	if got := store.Summary(); got.AgentCount != 0 {
		t.Fatalf("summary after clear = %+v", got)
	}
}

func TestHasPendingHITL(t *testing.T) {
	store := NewStore()
	if store.HasPendingHITL() {
		t.Fatal("empty store")
	}
	_ = SyncFromAgents(store, []nodeclient.AgentSummary{
		{AgentID: "agt-1", HasPendingHITL: true, PendingHITLItems: 1},
	})
	if !store.HasPendingHITL() {
		t.Fatal("expected HITL pending")
	}
	_ = SyncFromAgents(store, []nodeclient.AgentSummary{
		{AgentID: "agt-1", HasUnread: true},
	})
	if store.HasPendingHITL() {
		t.Fatal("unread-only should not count as HITL pending")
	}
}

func TestEntrySummaryLabel(t *testing.T) {
	e := Entry{AgentID: "abcd1234efgh", HITLItems: 1, HasUnread: true}
	if e.SummaryLabel() == "" {
		t.Fatal("expected label")
	}
	e2 := Entry{AgentID: "agt-x", DisplayName: "运维", HasUnread: true}
	if got := e2.SummaryLabel(); got != "运维 · 新回复" {
		t.Fatalf("label = %q", got)
	}
}
