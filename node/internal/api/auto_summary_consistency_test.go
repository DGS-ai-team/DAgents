package api

import (
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAutoSummaryConsistentAcrossEndpoints(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	check := func(name string, want string) {
		t.Run(name, func(t *testing.T) {
			get := httptest.NewRecorder()
			srv.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/agents/auto-reg/autonomy", nil))
			if get.Code != 200 {
				t.Fatalf("autonomy=%d", get.Code)
			}
			var a struct {
				Summary map[string]any `json:"summary"`
			}
			if err := json.Unmarshal(get.Body.Bytes(), &a); err != nil {
				t.Fatal(err)
			}
			o := httptest.NewRecorder()
			srv.Handler().ServeHTTP(o, httptest.NewRequest(http.MethodGet, "/v1/auto/overview", nil))
			if o.Code != 200 {
				t.Fatalf("overview=%d", o.Code)
			}
			var out struct {
				Items []struct {
					Summary map[string]any `json:"summary"`
				} `json:"items"`
			}
			if err := json.Unmarshal(o.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			if len(out.Items) != 1 {
				t.Fatalf("items=%d", len(out.Items))
			}
			for _, k := range []string{"state", "state_reason", "next_at"} {
				if a.Summary[k] != out.Items[0].Summary[k] {
					t.Fatalf("%s autonomy=%v overview=%v", k, a.Summary[k], out.Items[0].Summary[k])
				}
			}
			if a.Summary["state"] != want {
				t.Fatalf("state=%v want=%s", a.Summary["state"], want)
			}
		})
	}
	check("unconfigured", "unconfigured")
	if w := putAutonomy(t, srv, map[string]any{"objective": "x", "acceptance": "proof", "enabled": false}); w.Code != 200 {
		t.Fatalf("put=%d", w.Code)
	}
	check("disabled", "disabled")
	p, _ := srv.goalStore.GetProfile("auto-reg")
	if _, err := srv.goalStore.SetStatus(p.CurrentGoalID, "completed", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	// Profile remains disabled, so the public summary remains disabled and has no next_at.
	check("terminal-disabled", "disabled")
}

func TestAutoSummaryUsesDecisionSummaryWhenTopLevelEmpty(t *testing.T) {
	srv, _ := autonomyRegressionServer(t)
	if w := putAutonomy(t, srv, map[string]any{"objective": "work", "acceptance": "proof", "enabled": true}); w.Code != 200 {
		t.Fatalf("put=%d", w.Code)
	}
	p, _ := srv.goalStore.GetProfile("auto-reg")
	g, _ := srv.goalStore.Get(p.CurrentGoalID)
	r, err := srv.goalStore.StartRun(g.ID, "test", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	decision := &goals.FinalDecision{Outcome: goals.OutcomeProgress, Summary: "decision-only summary", Reason: "waiting", ExpectedProgress: "next", NextAction: goals.NextNone}
	if _, err = srv.goalStore.Checkpoint(g.ID, r.ID, goals.Checkpoint{Decision: decision}, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	detail := httptest.NewRecorder()
	srv.Handler().ServeHTTP(detail, httptest.NewRequest(http.MethodGet, "/v1/agents/auto-reg/autonomy", nil))
	var d struct {
		Summary map[string]any `json:"summary"`
	}
	if err := json.Unmarshal(detail.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Summary["last_summary"] != "decision-only summary" {
		t.Fatalf("detail summary=%v", d.Summary)
	}
	o := httptest.NewRecorder()
	srv.Handler().ServeHTTP(o, httptest.NewRequest(http.MethodGet, "/v1/auto/overview", nil))
	var ov struct {
		Items []struct {
			Summary map[string]any `json:"summary"`
		} `json:"items"`
	}
	if err := json.Unmarshal(o.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if len(ov.Items) != 1 || ov.Items[0].Summary["last_summary"] != "decision-only summary" {
		t.Fatalf("overview=%v", ov)
	}
}
