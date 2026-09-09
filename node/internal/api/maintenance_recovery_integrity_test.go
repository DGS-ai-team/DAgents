package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func setupRecoveryIntegrityCase(t *testing.T, mutation string) (*Server, *maintenanceScriptedClient, string, string, int64) {
	t.Helper()
	cfg := testConfig(t)
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client))
	const agentID = "recovery-integrity-agent"
	now := time.Now().UTC()
	snap := json.RawMessage(`{"agent_type":"auto"}`)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap, CreatedAt: now, UpdatedAt: now}); err != nil {
		srv.Close()
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 5, TotalTokenBudget: 5}, 0, now.Add(-24*time.Hour)); err != nil {
		srv.Close()
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		srv.Close()
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	parentID := "integrity-parent-" + mutation
	ms, err := srv.openAgentMemoryService(agentID, &store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap})
	if err != nil {
		srv.Close()
		t.Fatal(err)
	}
	if _, err := ms.ApplyMaintenanceOperation(context.Background(), memory.MaintenanceOperation{OperationID: parentID, AgentID: agentID, Scope: memory.ScopeAgent, SourceFingerprint: "actual-source", ExpectedCursor: 0, NextCursor: 3}, []memory.Candidate{}, memory.MaintenanceCursor{AgentID: agentID, Scope: memory.ScopeAgent, Sequence: 3, SourceFingerprint: "actual-source"}); err != nil {
		ms.Close()
		srv.Close()
		t.Fatal(err)
	}
	ms.Close()
	receiptFingerprint := "actual-source"
	candidates := json.RawMessage(`[]`)
	if mutation == "source" {
		receiptFingerprint = "tampered-source"
	} else {
		candidates = json.RawMessage(`[{ }]`)
	}
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, receiptFingerprint, 0, now); err != nil {
		srv.Close()
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, candidates, json.RawMessage(`{"messages":[{"role":"user","content":"durable"}]}`), 3, 0, false); err != nil {
		srv.Close()
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 0, false, now); err != nil {
		srv.Close()
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 5, now); err != nil {
		srv.Close()
		t.Fatal(err)
	}
	if _, err := srv.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "integrity test", now); err != nil {
		srv.Close()
		t.Fatal(err)
	}
	return srv, client, agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision
}

func TestMaintenanceRecoveryRejectsReceiptIntegrityMismatch(t *testing.T) {
	for _, mutation := range []string{"source", "candidate"} {
		t.Run(mutation, func(t *testing.T) {
			srv, client, agentID, date, revision := setupRecoveryIntegrityCase(t, mutation)
			defer srv.Close()
			query := "?local_date=" + date + "&schedule_revision=" + strconv.FormatInt(revision, 10)
			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/"+agentID+"/maintenance/recovery"+query, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
			}
			var view struct {
				Token string `json:"token"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Token == "" {
				t.Fatalf("token=%q err=%v", view.Token, err)
			}
			body := `{"local_date":"` + date + `","schedule_revision":` + strconv.FormatInt(revision, 10) + `,"token":"` + view.Token + `"}`
			w = httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/recovery", strings.NewReader(body)))
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "recovery_memory_mismatch") {
				t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
			}
			if client.calls != 0 {
				t.Fatalf("LLM calls=%d", client.calls)
			}
			snapshot, err := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, date, revision)
			if err != nil || snapshot.Occurrence.RecoveryCount != 0 || snapshot.Occurrence.Status != goals.MaintenanceOccurrenceRecoveryRequired {
				t.Fatalf("snapshot=%+v err=%v", snapshot.Occurrence, err)
			}
		})
	}
}
