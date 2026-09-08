package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func TestAutoOverviewExcludesNormalArchivedAndSupportsFilters(t *testing.T) {
	srv, agents := autonomyRegressionServer(t)
	now := time.Now().UTC()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "normal-overview", DisplayName: "Normal", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "archived-auto", DisplayName: "Archived", Archived: true, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auto/overview?page=1&page_size=1&search=auto", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("overview status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Items []struct {
			AgentID string `json:"agent_id"`
			State   string `json:"state"`
		} `json:"items"`
		Total    int            `json:"total"`
		Counts   map[string]int `json:"counts"`
		Revision string         `json:"revision"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || len(got.Items) != 1 || got.Items[0].AgentID != "auto-reg" || got.Items[0].State != "unconfigured" {
		t.Fatalf("unexpected overview: %+v", got)
	}
	if got.Revision == "" || got.Counts["unconfigured"] != 1 {
		t.Fatalf("missing snapshot metadata: %+v", got)
	}
	if got.Counts["total"] != 1 {
		t.Fatalf("counts total=%d", got.Counts["total"])
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auto/overview?status=disabled", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status filter=%d", rec.Code)
	}
	var disabled struct {
		Items  []json.RawMessage `json:"items"`
		Total  int               `json:"total"`
		Counts map[string]int    `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &disabled); err != nil {
		t.Fatal(err)
	}
	if disabled.Total != 0 || len(disabled.Items) != 0 || disabled.Counts["total"] != 1 {
		t.Fatalf("status filter must preserve aggregate count: %+v", disabled)
	}
}

func TestAutoOverviewTerminalGoalIsStandbyWithResultStatus(t *testing.T) {
	for _, tc := range []struct{ status, want string }{{"completed", "standby"}, {"stopped", "standby"}, {"failed", "needs_attention"}} {
		t.Run(tc.status, func(t *testing.T) {
			got, _ := autoGoalState(goals.Goal{Status: goals.Status(tc.status), StatusReason: "terminal"}, nil)
			if got != tc.want {
				t.Fatalf("state=%q want %q", got, tc.want)
			}
		})
	}
}

func TestAutoOverviewNextAtRequiresEnabledProfileAndOwnedGoalTrigger(t *testing.T) {
	now := time.Now().UTC()
	future := float64(now.Add(time.Hour).UnixNano()) / 1e9
	g := goals.Goal{ID: "goal-1", AgentID: "auto-1", TriggerID: "trigger-1", Status: goals.StatusActive}
	base := triggers.Definition{TriggerID: "trigger-1", ManagedGoalID: "goal-1", OwnerAgentID: "auto-1", TargetAgentID: "auto-1", Controller: "goal", ControllerID: "goal-1", Enabled: true, NextFireAt: &future}
	if got := autoTriggerNextAt(base, g, false, "auto-1", now); got != nil {
		t.Fatal("disabled profile exposed next_at")
	}
	wrongOwner := base
	wrongOwner.OwnerAgentID = "other-agent"
	if got := autoTriggerNextAt(wrongOwner, g, true, "auto-1", now); got != nil {
		t.Fatal("orphan trigger exposed next_at")
	}
	if got := autoTriggerNextAt(base, g, true, "auto-1", now); got == nil {
		t.Fatal("valid trigger omitted next_at")
	}
}
