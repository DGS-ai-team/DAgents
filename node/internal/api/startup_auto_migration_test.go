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
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func TestStartupMigratesAutoBeforeOverview(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	cfg.LLM.Mock = true
	now := time.Now().UTC()
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range []store.AgentRecord{
		{AgentID: "startup-auto", DisplayName: "Auto", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now},
		{AgentID: "startup-normal", DisplayName: "Normal", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal"}`), CreatedAt: now, UpdatedAt: now},
	} {
		if err := as.Save(context.Background(), rec); err != nil {
			t.Fatal(err)
		}
	}
	if err := as.Close(); err != nil {
		t.Fatal(err)
	}
	ns, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := ns.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	_ = ns.Close()
	ts, err := triggers.OpenStore(cfg.TriggersStorePath(), 200)
	if err != nil {
		t.Fatal(err)
	}
	def, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "normal-trigger", Condition: map[string]any{"interval_seconds": 300}, TargetAgentID: "startup-normal", TaskTemplate: "normal", Enabled: func() *bool { v := true; return &v }()}, cfg.NodeID, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.CreateTrigger(def); err != nil {
		t.Fatal(err)
	}
	_ = ts
	gs, err := goals.OpenStore(cfg.RuntimeDir() + "/goals.json")
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := gs.Create(goals.CreateInput{AgentID: "startup-auto", Managed: true, Objective: "legacy objective", Acceptance: "legacy proof"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = gs.RecordUsage("startup-auto", 100, 0, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err = gs.Create(goals.CreateInput{AgentID: "startup-normal", Managed: false, Objective: "normal", Acceptance: "proof"}, now); err != nil {
		t.Fatal(err)
	}
	_ = legacy
	_ = gs
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}))
	defer srv.Close()
	if srv.startupErr != nil {
		t.Fatalf("startup=%v", srv.startupErr)
	}
	p, ok := srv.goalStore.GetProfile("startup-auto")
	if !ok || p.CurrentGoalID != legacy.ID {
		t.Fatalf("auto profile=%+v ok=%v", p, ok)
	}
	usage, ok := srv.goalStore.GetUsage("startup-auto")
	if !ok || usage.BusinessTokens != 100 {
		t.Fatalf("usage=%+v ok=%v", usage, ok)
	}
	if _, ok := srv.goalStore.GetProfile("startup-normal"); ok {
		t.Fatal("normal received auto profile")
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auto/overview", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("overview=%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "startup-auto") {
		t.Fatalf("overview missing auto: %s", rec.Body.String())
	}
}
