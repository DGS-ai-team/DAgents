package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func appendMaintenanceJournal(t *testing.T, db *store.SQLiteStore, agentID, sessionID, turnID string, withUsage bool) {
	t.Helper()
	now := time.Now().UTC()
	types := []turn.EventType{turn.EventTurnStarted, turn.EventStepStarted, turn.EventModelRequestStarted, turn.EventModelRequestCompleted}
	if withUsage {
		types = append(types, turn.EventModelUsageRecorded)
	}
	types = append(types, turn.EventAssistantMessageRecorded, turn.EventStepCompleted, turn.EventTurnCompleted)
	if turnID == "failure-turn" {
		types = append(types[:len(types)-2], append([]turn.EventType{turn.EventToolBatchCreated, turn.EventToolCallRecorded, turn.EventToolExecutionStarted, turn.EventToolExecutionCompleted, turn.EventToolResultRecorded, turn.EventToolBatchSettled}, types[len(types)-2:]...)...)
	}
	for i, eventType := range types {
		event := turn.NewTurnEventEnvelope(sessionID, eventType, now.Add(time.Duration(i)*time.Millisecond))
		event.AgentID, event.TurnID = agentID, turnID
		event.Source = string(turn.TurnSourceSideEffect)
		if eventType != turn.EventTurnStarted {
			event.StepID = "step-1"
		}
		event.CommandID = "maintenance-journal-" + turnID + "-" + strconv.Itoa(i)
		event.Payload = json.RawMessage(`{"generation":1}`)
		if eventType == turn.EventModelRequestStarted {
			event.Payload = json.RawMessage(`{"generation":1,"request_digest":"maintenance"}`)
		}
		if eventType == turn.EventModelUsageRecorded {
			event.Payload, _ = json.Marshal(map[string]any{"generation": 1, "usage": turn.StepUsage{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}})
		}
		if turnID == "failure-turn" && eventType == turn.EventToolCallRecorded {
			event.ToolCallID = "failure-call"
			event.Payload = json.RawMessage(`{"generation":1,"tool_name":"write_file","arguments_json":"{\"path\":\"handbook/guide.md\",\"content\":\"guide\"}"}`)
		}
		if turnID == "failure-turn" && (eventType == turn.EventToolBatchCreated || eventType == turn.EventToolBatchSettled) {
			event.Payload = json.RawMessage(`{"generation":1,"tool_batch_id":"failure-batch"}`)
		}
		if turnID == "failure-turn" && (eventType == turn.EventToolExecutionStarted || eventType == turn.EventToolExecutionCompleted) {
			event.ToolCallID, event.ToolExecutionID = "failure-call", "failure-exec"
			event.Payload = json.RawMessage(`{"generation":1,"tool_name":"write_file","execution_status":"succeeded"}`)
		}
		if turnID == "failure-turn" && eventType == turn.EventToolResultRecorded {
			event.ToolCallID, event.ToolExecutionID = "failure-call", "failure-exec"
			event.Payload = json.RawMessage(`{"generation":1,"tool_name":"write_file","result_content":"ok"}`)
		}
		if _, err := db.AppendTurnEvent(context.Background(), event); err != nil {
			t.Fatalf("append %s: %v", eventType, err)
		}
	}
}

func TestMaintenanceRecoveryHTTPSuccessThenScheduler(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client))
	defer func() { srv.Close() }()
	const agentID = "recovery-success-agent"
	now := time.Now().UTC()
	snap := json.RawMessage(`{"agent_type":"auto","defaults":{"tools":{"enabled_groups":["fs"]}}}`)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.agents.MutateAgentPolicy(context.Background(), agentID, func(p *store.AgentPolicyRecord) error { p.Tools["write_file"] = "never"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 5, TotalTokenBudget: 5}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	parentID := "daily-recovery-parent"
	ms, err := srv.openAgentMemoryService(agentID, &store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.ApplyMaintenanceOperation(context.Background(), memory.MaintenanceOperation{OperationID: parentID, AgentID: agentID, Scope: memory.ScopeAgent, SourceFingerprint: "daily-recovery", ExpectedCursor: 0, NextCursor: 3}, []memory.Candidate{}, memory.MaintenanceCursor{AgentID: agentID, Scope: memory.ScopeAgent, Sequence: 3, SourceFingerprint: "daily-recovery"}); err != nil {
		ms.Close()
		t.Fatal(err)
	}
	ms.Close()
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, "daily-recovery", 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"recover durable evidence"}]}`), 3, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 5, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "test recovery", now); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	srv = NewServer(cfg, nil, WithLLM(client))
	query := "?local_date=" + claim.Occurrence.LocalDate + "&schedule_revision=" + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/"+agentID+"/maintenance/recovery"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	var view struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.Token == "" {
		t.Fatalf("GET token=%q err=%v", view.Token, err)
	}
	body := `{"local_date":"` + claim.Occurrence.LocalDate + `","schedule_revision":` + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10) + `,"token":"` + view.Token + `"}`
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/recovery", strings.NewReader(body)))
	if w.Code != http.StatusAccepted {
		t.Fatalf("POST status=%d body=%s", w.Code, w.Body.String())
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), now)
	child, _ := srv.goalStore.GetMaintenanceReceipt("handbook:" + parentID)
	if child.PhaseState != goals.MaintenancePhaseComplete {
		t.Fatalf("scheduler did not complete handbook child: %+v", child)
	}
	expectedRoot, err := agentruntime.HandbookRoot(cfg.RuntimeDir(), agentID, agentruntime.WorkspaceConfig{}, agentruntime.HandbookConfig{})
	if err == nil {
		expectedRoot, err = filepath.EvalSymlinks(expectedRoot)
	}
	if err != nil || child.HandbookRoot != expectedRoot {
		t.Fatalf("handbook root binding=%q want=%q err=%v", child.HandbookRoot, expectedRoot, err)
	}
	record, err := srv.agents.Get(context.Background(), agentID)
	if err != nil || record == nil {
		t.Fatalf("load agent for directory change: %v", err)
	}
	record.ConfigSnapshot = json.RawMessage(`{"agent_type":"auto","handbook":{"directory":"changed-after-run"}}`)
	if err := srv.agents.Save(context.Background(), *record); err != nil {
		t.Fatal(err)
	}
	unchanged, _ := srv.goalStore.GetMaintenanceReceipt("handbook:" + parentID)
	if unchanged.HandbookRoot != expectedRoot {
		t.Fatalf("handbook root changed after agent directory edit: got=%q want=%q", unchanged.HandbookRoot, expectedRoot)
	}
	handbook, err := agentruntime.HandbookRoot(cfg.RuntimeDir(), agentID, agentruntime.WorkspaceConfig{}, agentruntime.HandbookConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(handbook, "maintenance.md")); err != nil || string(raw) != "evidence" {
		t.Fatalf("handbook=%q err=%v", raw, err)
	}
	oldCalls := client.calls
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/recovery", strings.NewReader(body)))
	if w.Code != http.StatusConflict || client.calls != oldCalls {
		t.Fatalf("stale recovery accepted: status=%d calls=%d want=%d", w.Code, client.calls, oldCalls)
	}
}

func TestMaintenanceRecoveryHTTPRejectsChangedToken(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil)
	defer srv.Close()
	const agentID = "recovery-token-agent"
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC"}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	query := "?local_date=" + claim.Occurrence.LocalDate + "&schedule_revision=" + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/"+agentID+"/maintenance/recovery"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	var snapshot struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil || snapshot.Token == "" {
		t.Fatalf("GET token=%q err=%v", snapshot.Token, err)
	}
	body := `{"local_date":"` + claim.Occurrence.LocalDate + `","schedule_revision":` + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10) + `,"token":"stale-token"}`
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/recovery", strings.NewReader(body))
	srv.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale token status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestMaintenanceRecoveryHTTPRejectsMissingMemoryOperation(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil)
	defer srv.Close()
	const agentID = "recovery-missing-operation"
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 5, TotalTokenBudget: 5}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	parentID := "missing-memory-operation"
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, "missing-source", 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"missing operation evidence"}]}`), 3, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 5, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.MarkHandbookRunning(agentID, "handbook:"+parentID, "missing-session"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "missing operation", now); err != nil {
		t.Fatal(err)
	}
	before, err := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if err != nil {
		t.Fatal(err)
	}
	query := "?local_date=" + claim.Occurrence.LocalDate + "&schedule_revision=" + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/"+agentID+"/maintenance/recovery"+query, nil))
	var view struct {
		Token    string `json:"token"`
		Receipts []struct {
			Stage          string `json:"stage"`
			Reconciliation *struct {
				Reason string `json:"reason"`
			} `json:"reconciliation"`
		} `json:"receipts"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &view) != nil || view.Token == "" {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	var foundReconciliation bool
	for _, receipt := range view.Receipts {
		if receipt.Stage == goals.MaintenancePhaseRunning && receipt.Reconciliation != nil {
			foundReconciliation = true
			if receipt.Reconciliation.Reason == "" {
				t.Fatalf("GET reconciliation omitted diagnostic reason: %s", w.Body.String())
			}
		}
	}
	if !foundReconciliation {
		t.Fatalf("GET omitted running handbook reconciliation: %s", w.Body.String())
	}
	after, err := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if err != nil || after.Token != before.Token || after.Usage != before.Usage || after.Occurrence.Status != before.Occurrence.Status || len(after.Receipts) != len(before.Receipts) {
		t.Fatalf("read-only reconciliation changed recovery snapshot: before=%+v after=%+v err=%v", before.Occurrence, after.Occurrence, err)
	}
	body := `{"local_date":"` + claim.Occurrence.LocalDate + `","schedule_revision":` + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10) + `,"token":"` + view.Token + `"}`
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/recovery", strings.NewReader(body)))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "recovery_memory_mismatch") {
		t.Fatalf("missing operation accepted: status=%d body=%s", w.Code, w.Body.String())
	}
	snapshot, _ := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if snapshot.Occurrence.RecoveryCount != 0 || snapshot.Occurrence.Status != goals.MaintenanceOccurrenceRecoveryRequired {
		t.Fatalf("recovery state changed: %+v", snapshot.Occurrence)
	}
}

func TestMaintenanceRecoveryGETReconcilesBoundTurnsReadOnly(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil)
	defer srv.Close()
	const agentID = "recovery-reconcile-agent"
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 20, TotalTokenBudget: 20}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	for _, tc := range []struct {
		parent, session, turn string
		withUsage             bool
	}{
		{"reconcile-known-parent", "reconcile-session-known", "reconcile-turn-known", true},
	} {
		if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, tc.parent, "source-"+tc.parent, 0, now); err != nil {
			t.Fatal(err)
		}
		if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, tc.parent, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"reconciliation evidence"}]}`), 0, 0, false); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.goalStore.SettleMaintenance(agentID, tc.parent, 0, false, now); err != nil {
			t.Fatal(err)
		}
		childID := "handbook:" + tc.parent
		if _, err := srv.goalStore.PrepareHandbook(tc.parent, agentID, 5, now); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.goalStore.MarkHandbookRunning(agentID, childID, tc.session); err != nil {
			t.Fatal(err)
		}
		if err := srv.goalStore.BindHandbookTurn(agentID, childID, tc.session, tc.turn, now); err != nil {
			t.Fatal(err)
		}
		appendMaintenanceJournal(t, srv.store, agentID, tc.session, tc.turn, tc.withUsage)
	}
	if _, err := srv.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "reconcile", now); err != nil {
		t.Fatal(err)
	}
	before, err := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if err != nil {
		t.Fatal(err)
	}
	query := "?local_date=" + claim.Occurrence.LocalDate + "&schedule_revision=" + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/"+agentID+"/maintenance/recovery"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	var view struct {
		Token    string `json:"token"`
		Receipts []struct {
			ID             string `json:"id"`
			Reconciliation *struct {
				Completed  bool           `json:"completed"`
				UsageKnown bool           `json:"usage_known"`
				Usage      turn.TurnUsage `json:"usage"`
				Reason     string         `json:"reason"`
			} `json:"reconciliation"`
		} `json:"receipts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Token != before.Token {
		t.Fatalf("GET changed recovery token: got=%q want=%q", view.Token, before.Token)
	}
	var known bool
	for _, receipt := range view.Receipts {
		if receipt.Reconciliation == nil {
			continue
		}
		switch receipt.ID {
		case "handbook:reconcile-known-parent":
			known = true
			if !receipt.Reconciliation.Completed || !receipt.Reconciliation.UsageKnown || receipt.Reconciliation.Usage.TotalTokens != 3 {
				t.Fatalf("known reconciliation=%+v", receipt.Reconciliation)
			}
		}
	}
	if !known {
		t.Fatalf("GET omitted bound reconciliation summaries: %+v", view.Receipts)
	}
	after, err := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if err != nil || after.Token != before.Token || after.Usage != before.Usage || after.Occurrence.RecoveryCount != before.Occurrence.RecoveryCount {
		t.Fatalf("GET mutated recovery state: before=%+v after=%+v err=%v", before.Occurrence, after.Occurrence, err)
	}
}

func TestMaintenanceRecoveryGETBoundTurnMissingUsageIsUnconfirmed(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil)
	defer srv.Close()
	const agentID = "recovery-reconcile-missing-usage"
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 20, TotalTokenBudget: 20}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	parentID, sessionID, turnID := "missing-usage-parent", "missing-usage-session", "missing-usage-turn"
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, "missing-usage-source", 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"missing usage"}]}`), 0, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 0, false, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 5, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.MarkHandbookRunning(agentID, childID, sessionID); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.BindHandbookTurn(agentID, childID, sessionID, turnID, now); err != nil {
		t.Fatal(err)
	}
	appendMaintenanceJournal(t, srv.store, agentID, sessionID, turnID, false)
	if _, err := srv.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "missing usage", now); err != nil {
		t.Fatal(err)
	}
	before, err := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if err != nil {
		t.Fatal(err)
	}
	query := "?local_date=" + claim.Occurrence.LocalDate + "&schedule_revision=" + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/"+agentID+"/maintenance/recovery"+query, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", w.Code, w.Body.String())
	}
	var view struct {
		Token    string `json:"token"`
		Receipts []struct {
			ID             string `json:"id"`
			Reconciliation *struct {
				Completed  bool   `json:"completed"`
				UsageKnown bool   `json:"usage_known"`
				Reason     string `json:"reason"`
			} `json:"reconciliation"`
		} `json:"receipts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	for _, receipt := range view.Receipts {
		if receipt.ID == childID {
			if receipt.Reconciliation == nil || receipt.Reconciliation.Completed || receipt.Reconciliation.UsageKnown || receipt.Reconciliation.Reason == "" {
				t.Fatalf("missing usage was confirmed: %+v", receipt.Reconciliation)
			}
			if view.Token != before.Token {
				t.Fatalf("GET changed token")
			}
			return
		}
	}
	t.Fatalf("GET omitted child reconciliation: %s", w.Body.String())
}

func TestMaintenanceRecoveryHTTPBusyDoesNotResume(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil)
	defer srv.Close()
	id := "recovery-busy"
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC"}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(id, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	if _, err := srv.goalStore.FinishMaintenance(id, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "busy", now); err != nil {
		t.Fatal(err)
	}
	q := "?local_date=" + claim.Occurrence.LocalDate + "&schedule_revision=" + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/"+id+"/maintenance/recovery"+q, nil))
	var view struct {
		Token string `json:"token"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		t.Fatalf("GET=%d %s", w.Code, w.Body.String())
	}
	ctx, release, acquired, err := srv.sessions.TryAcquireMaintenanceContext(context.Background(), id)
	_ = ctx
	if err != nil || !acquired {
		t.Fatalf("gate acquired=%v err=%v", acquired, err)
	}
	defer release()
	body := `{"local_date":"` + claim.Occurrence.LocalDate + `","schedule_revision":` + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10) + `,"token":"` + view.Token + `"}`
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+id+"/maintenance/recovery", strings.NewReader(body)))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "agent_busy") {
		t.Fatalf("busy recovery status=%d body=%s", w.Code, w.Body.String())
	}
	snapshot, _ := srv.goalStore.GetMaintenanceRecoverySnapshot(id, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if snapshot.Occurrence.RecoveryCount != 0 {
		t.Fatalf("busy changed recovery: %+v", snapshot.Occurrence)
	}
}

func TestMaintenanceRecoveryHTTPCrossAgentCannotResume(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil)
	defer srv.Close()
	now := time.Now().UTC()
	for _, id := range []string{"recovery-owner", "recovery-other"} {
		if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC"}, 0, now.Add(-24*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	claim, err := srv.goalStore.ClaimMaintenance("recovery-owner", now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	if _, err := srv.goalStore.FinishMaintenance("recovery-owner", claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "cross-agent", now); err != nil {
		t.Fatal(err)
	}
	q := "?local_date=" + claim.Occurrence.LocalDate + "&schedule_revision=" + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/agents/recovery-owner/maintenance/recovery"+q, nil))
	var view struct {
		Token string `json:"token"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &view) != nil {
		t.Fatalf("GET=%d %s", w.Code, w.Body.String())
	}
	body := `{"local_date":"` + claim.Occurrence.LocalDate + `","schedule_revision":` + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10) + `,"token":"` + view.Token + `"}`
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/recovery-other/maintenance/recovery", strings.NewReader(body)))
	if w.Code != http.StatusConflict && w.Code != http.StatusNotFound {
		t.Fatalf("cross-agent status=%d body=%s", w.Code, w.Body.String())
	}
}
