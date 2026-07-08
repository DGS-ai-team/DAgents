package pending

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
)

func TestApplyEvent_hitlAndDone(t *testing.T) {
	store := NewStore()
	changed := ApplyEvent(store, nodeclient.StreamEvent{
		Type:      "hitl_required",
		SessionID: "sess-1",
		Data: map[string]any{
			"items": []any{map[string]any{"hitl_type": "user_information"}, map[string]any{"hitl_type": "execute_tool"}},
		},
	})
	if !changed {
		t.Fatal("expected pending mark")
	}
	sum := store.Summary()
	if sum.SessionCount != 1 || sum.ItemCount != 2 {
		t.Fatalf("summary = %+v", sum)
	}

	ApplyEvent(store, nodeclient.StreamEvent{
		Type:      "done",
		SessionID: "sess-1",
		Data:      map[string]any{"finish_reason": "awaiting_hitl", "awaiting": "hitl", "turn_complete": false},
	})
	if store.Summary().SessionCount != 1 {
		t.Fatal("awaiting_hitl done should keep pending")
	}

	ApplyEvent(store, nodeclient.StreamEvent{
		Type:      "done",
		SessionID: "sess-1",
		Data:      map[string]any{"finish_reason": "stop", "turn_complete": true},
	})
	if store.Summary().SessionCount != 1 {
		t.Fatalf("stop should mark unread, summary = %+v", store.Summary())
	}
	e := store.Entries()[0]
	if !e.HasUnread || e.HITLItems != 0 {
		t.Fatalf("entry = %+v", e)
	}
	store.MarkConsumed("sess-1")
	if store.Summary().SessionCount != 0 {
		t.Fatalf("consumed should clear unread, summary = %+v", store.Summary())
	}
}

func TestApplyEvent_unreadOnly(t *testing.T) {
	store := NewStore()
	ApplyEvent(store, nodeclient.StreamEvent{
		Type:      "done",
		SessionID: "sess-reply",
		Data:      map[string]any{"finish_reason": "stop", "turn_complete": true},
	})
	e := store.Entries()[0]
	if e.HITLItems != 0 {
		t.Fatalf("entry = %+v", e)
	}
	if e.FocusHITL() {
		t.Fatal("unread-only should not focus hitl")
	}
}

func TestApplyEvent_a2aEvents(t *testing.T) {
	store := NewStore()
	for _, typ := range []string{"approval_required", "user_information_required"} {
		ApplyEvent(store, nodeclient.StreamEvent{Type: typ, SessionID: "sess-a2a", Data: map[string]any{}})
	}
	if got := store.Summary().SessionCount; got != 1 {
		t.Fatalf("session count = %d", got)
	}
}

func TestSyncActiveAwaiting(t *testing.T) {
	store := NewStore()
	store.MarkHITL("sess-active", 1, "hitl_required")
	store.MarkHITL("sess-idle", 1, "hitl_required")
	store.MarkUnread("sess-unread")

	SyncActiveAwaiting(store, []nodeclient.SessionSummary{
		{SessionID: "sess-active", Active: true, RunTurnPhase: "awaiting_hitl"},
		{SessionID: "sess-idle", Active: true, RunTurnPhase: "idle"},
	})

	entries := store.Entries()
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
}
