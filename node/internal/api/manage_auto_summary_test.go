package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestAutoSummaryProviderReporterPrivacyAndProjection(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	now := time.Now().UTC()
	for _, rec := range []store.AgentRecord{
		{AgentID: "normal-private", DisplayName: "Normal", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal"}`), CreatedAt: now, UpdatedAt: now},
		{AgentID: "archived-private", DisplayName: "Archived", Archived: true, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now},
	} {
		if err := srv.agents.Save(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
	if w := putAutonomy(t, srv, map[string]any{"objective": "private/path", "acceptance": "user prompt", "role_objective": "role objective", "enabled": true}); w.Code != http.StatusOK {
		t.Fatalf("fixture autonomy PUT=%d %s", w.Code, w.Body.String())
	}
	profile, _ := srv.goalStore.GetProfile("auto-reg")
	goal, _ := srv.goalStore.Get(profile.CurrentGoalID)
	run, err := srv.goalStore.StartRun(goal.ID, "test", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.Checkpoint(goal.ID, run.ID, goals.Checkpoint{Summary: "checkpoint secret", Done: false}, now); err != nil {
		t.Fatal(err)
	}
	provider := srv.autoSummaryProvider()
	var received map[string]any
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer remote.Close()
	reporter, err := manage.NewAutoSummaryReporter(remote.URL, "node-a", "secret", provider)
	if err != nil {
		t.Fatal(err)
	}
	if err := reporter.Report(context.Background()); err != nil {
		t.Fatal(err)
	}
	if received["agent_id"] != "auto-reg" || received["state"] == "" || received["as_of"] == nil {
		t.Fatalf("incomplete summary: %#v", received)
	}
	if _, ok := received["agent_id"].(string); !ok || received["agent_id"] == "normal-private" || received["agent_id"] == "archived-private" {
		t.Fatalf("normal or archived agent was reported: %#v", received["agent_id"])
	}
	if received["role"] != "Auto employee" {
		t.Fatalf("unsafe role projection: %#v", received["role"])
	}
	body, _ := json.Marshal(received)
	for _, secret := range []string{"role objective", "private/path", "checkpoint secret", "user prompt"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("private value leaked: %q", secret)
		}
	}
	usage, ok := received["usage"].(map[string]any)
	if !ok || usage["tokens"] == nil {
		t.Fatalf("missing aggregate usage: %#v", received["usage"])
	}
}
