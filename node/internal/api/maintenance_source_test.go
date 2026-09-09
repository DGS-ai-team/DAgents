package api

import (
	"context"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"path/filepath"
	"testing"
	"time"
)

func appendSourceSnapshot(t *testing.T, st *store.SQLiteStore, agent, session, content string) uint64 {
	t.Helper()
	e := turn.NewTurnEventEnvelope(session, turn.EventTurnCompleted, time.Now().UTC())
	e.AgentID = agent
	// A session may contain several completed turns; each must remain distinct.
	e.TurnID = fmt.Sprintf("%s-turn-%d", session, time.Now().UnixNano())
	e.CommandID = fmt.Sprintf("%s-command-%d", session, time.Now().UnixNano())
	var messages []llm.Message
	if content != "" {
		messages = []llm.Message{{Role: "user", Content: content}}
	}
	got, err := st.AppendTurnEventWithSnapshot(context.Background(), e, messages)
	if err != nil {
		t.Fatal(err)
	}
	return uint64(got.ID)
}

func TestMaintenanceSourceSkipsLinkedMaintenanceWithinPage(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, err := goals.OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := g.SaveProfile(goals.AutoProfile{AgentID: "a", Enabled: true, MaintenanceTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := g.BeginMaintenance("a", "receipt", "fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := g.SetMaintenanceSessionID("a", "receipt", "maintenance-session"); err != nil {
		t.Fatal(err)
	}
	appendSourceSnapshot(t, st, "a", "maintenance-session", "receipt")
	appendSourceSnapshot(t, st, "a", "maintenance-session", "receipt-2")
	id := appendSourceSnapshot(t, st, "a", "business-session", "business")
	got, err := (maintenanceSource{store: st, goals: g}).LoadMaintenanceMessages(context.Background(), "a", 0, 1)
	if err != nil || got.Sequence != id || got.SessionID != "business-session" || !got.Complete {
		t.Fatalf("batch=%+v err=%v", got, err)
	}
}

func TestMaintenanceSourceDoesNotSkipUnlinkedOrUnreadableBusiness(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, _ := goals.OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	now := time.Now().UTC()
	_, _ = g.SaveProfile(goals.AutoProfile{AgentID: "a", Enabled: true, MaintenanceTokenBudget: 100}, 0, now)
	id := appendSourceSnapshot(t, st, "a", "maintenance-user-123", "ordinary")
	got, err := (maintenanceSource{store: st, goals: g}).LoadMaintenanceMessages(context.Background(), "a", 0, 10)
	if err != nil || got.Sequence != id || got.SessionID != "maintenance-user-123" {
		t.Fatalf("ordinary=%+v err=%v", got, err)
	}
}

func TestMaintenanceSourceStopsAtUnreadableBusiness(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	g, err := goals.OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := g.SaveProfile(goals.AutoProfile{AgentID: "a", Enabled: true, MaintenanceTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := g.BeginMaintenance("a", "receipt", "fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := g.SetMaintenanceSessionID("a", "receipt", "maintenance-session"); err != nil {
		t.Fatal(err)
	}
	appendSourceSnapshot(t, st, "a", "maintenance-session", "receipt")
	barrier := appendSourceSnapshot(t, st, "a", "business-unreadable", "")
	appendSourceSnapshot(t, st, "a", "business-later", "later")
	items, err := st.ListCompletedTurnSnapshots(context.Background(), "a", 0, 10)
	if err != nil || len(items) != 3 {
		t.Fatalf("snapshots=%d err=%v", len(items), err)
	}
	got, err := (maintenanceSource{store: st, goals: g}).LoadMaintenanceMessages(context.Background(), "a", 0, 10)
	if err != nil || got.Sequence != barrier || got.Complete {
		t.Fatalf("barrier=%+v err=%v", got, err)
	}
}

func TestMaintenanceSourceUsesReceiptSessionIdentity(t *testing.T) {
	s, err := goals.OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.SaveProfile(goals.AutoProfile{AgentID: "a", Enabled: true, MaintenanceTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if s.IsMaintenanceSession("a", "maintenance-user-123") {
		t.Fatal("unreceipted same-name session skipped")
	}
	if _, err := s.BeginMaintenance("a", "receipt", "fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMaintenanceSessionID("a", "receipt", "maintenance-user-123"); err != nil {
		t.Fatal(err)
	}
	if !s.IsMaintenanceSession("a", "maintenance-user-123") {
		t.Fatal("receipt-linked session not recognized")
	}
	if s.IsMaintenanceSession("b", "maintenance-user-123") || s.IsMaintenanceSession("", "maintenance-user-123") || s.IsMaintenanceSession("a", "") {
		t.Fatal("identity crossed or accepted empty values")
	}
	if err := s.SetMaintenanceSessionID("a", "receipt", "other"); err == nil {
		t.Fatal("receipt allowed rebinding")
	}
	if err := s.SetMaintenanceSessionID("a", "receipt", "maintenance-user-123"); err != nil {
		t.Fatal(err)
	}
}
