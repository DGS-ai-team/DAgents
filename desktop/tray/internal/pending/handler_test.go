package pending

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
)

func TestSyncFromSessions(t *testing.T) {
	store := NewStore()
	changed := SyncFromSessions(store, []nodeclient.SessionSummary{
		{SessionID: "sess-hitl", HasPendingHITL: true, PendingHITLItems: 2},
		{SessionID: "sess-unread", HasUnread: true, NotifySeq: 10, AckSeq: 5},
		{SessionID: "sess-clear", HasUnread: false, HasPendingHITL: false},
	})
	if !changed {
		t.Fatal("expected initial sync to change store")
	}
	sum := store.Summary()
	if sum.SessionCount != 2 || sum.ItemCount != 3 {
		t.Fatalf("summary = %+v", sum)
	}

	if SyncFromSessions(store, []nodeclient.SessionSummary{
		{SessionID: "sess-hitl", HasPendingHITL: true, PendingHITLItems: 2},
		{SessionID: "sess-unread", HasUnread: true, NotifySeq: 10, AckSeq: 5},
	}) {
		t.Fatal("identical sync should not report change")
	}

	changed = SyncFromSessions(store, []nodeclient.SessionSummary{
		{SessionID: "sess-unread", HasUnread: false, AckSeq: 10, NotifySeq: 10},
	})
	if !changed {
		t.Fatal("expected change when unread cleared")
	}
	if store.Summary().SessionCount != 0 {
		t.Fatalf("expected empty store, got %+v", store.Summary())
	}
}

func TestShouldSyncOnEvent(t *testing.T) {
	if !ShouldSyncOnEvent(nodeclient.StreamEvent{Type: "done", SessionID: "s1"}) {
		t.Fatal("done should trigger sync")
	}
	if ShouldSyncOnEvent(nodeclient.StreamEvent{Type: "assistant", SessionID: "s1"}) {
		t.Fatal("assistant should not trigger sync")
	}
}

func TestEntrySummaryLabel(t *testing.T) {
	e := Entry{SessionID: "abcd1234efgh", HITLItems: 1, HasUnread: true}
	if e.SummaryLabel() == "" {
		t.Fatal("expected label")
	}
	e2 := Entry{SessionID: "sess-x", HasUnread: true}
	if e2.SummaryLabel() == "" {
		t.Fatal("expected unread label")
	}
}
