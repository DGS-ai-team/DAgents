package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testMaintenanceParentID(agentID, fingerprint string, cursor int64) string {
	sum := sha256.Sum256([]byte(agentID + ":" + fingerprint + ":" + strconv.FormatInt(cursor, 10)))
	return "maintenance-" + fmt.Sprintf("%x", sum[:])
}

func TestHandbookRunningReopenDoesNotInvokeModel(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil, WithLLM(&maintenanceScriptedClient{}))
	defer func() { srv.Close() }()
	const agentID = "handbook-running-reopen"
	now := time.Now().UTC()
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 5, TotalTokenBudget: 5}, 0, now); err != nil {
		t.Fatal(err)
	}
	parentID := testMaintenanceParentID(agentID, "parent-fingerprint", 7)
	if _, err := srv.goalStore.BeginMaintenance(agentID, parentID, "parent-fingerprint", 1, now); err != nil {
		t.Fatal(err)
	}
	evidence := json.RawMessage(`{"cursor_before":0,"cursor_after":7,"messages":[]}`)
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[]`), evidence, 7, 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 1, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 4, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	if _, err := srv.goalStore.MarkHandbookRunning(agentID, childID, "already-running"); err != nil {
		t.Fatal(err)
	}
	client := &maintenanceScriptedClient{}
	srv.Close()
	srv = NewServer(cfg, nil, WithLLM(client))
	_, cleanup, err := srv.runHandbookMaintenance(context.Background(), context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`)}, "resume", parentID, mustReceipt(t, srv.goalStore, parentID), turn.TurnBudget{MaxTotalTokens: 4})
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("running handbook receipt was re-run")
	}
	if client.calls != 0 {
		t.Fatalf("reopened running receipt invoked model: %d", client.calls)
	}
	child, _ := srv.goalStore.GetMaintenanceReceipt(childID)
	if child.PhaseState != goals.MaintenancePhaseRunning || child.Status != "pending" {
		t.Fatalf("running receipt changed: %+v", child)
	}
}

func TestHandbookPreparedReopenUsesExistingReservation(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client))
	defer func() { srv.Close() }()
	const agentID = "handbook-prepared-reopen"
	now := time.Now().UTC()
	snap := json.RawMessage(`{"agent_type":"auto","defaults":{"tools":{"enabled_groups":["fs"]}}}`)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.agents.MutateAgentPolicy(context.Background(), agentID, func(p *store.AgentPolicyRecord) error { p.Tools["write_file"] = "never"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 5, TotalTokenBudget: 5}, 0, now); err != nil {
		t.Fatal(err)
	}
	parentID := testMaintenanceParentID(agentID, "prepared-parent", 3)
	ms, err := srv.openAgentMemoryService(agentID, &store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.ApplyMaintenanceOperation(context.Background(), memory.MaintenanceOperation{OperationID: parentID, AgentID: agentID, Scope: memory.ScopeAgent, SourceFingerprint: "prepared-parent", ExpectedCursor: 0, NextCursor: 3}, nil, memory.MaintenanceCursor{AgentID: agentID, Scope: memory.ScopeAgent, Sequence: 3, SourceFingerprint: "prepared-parent"}); err != nil {
		ms.Close()
		t.Fatal(err)
	}
	ms.Close()
	if _, err := srv.goalStore.BeginMaintenance(agentID, parentID, "prepared-parent", 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[]`), json.RawMessage(`{"cursor_before":0,"cursor_after":3,"messages":[{"role":"user","content":"prepared durable evidence"}]}`), 3, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 5, now); err != nil {
		t.Fatal(err)
	}
	// Reopen the persisted stores before attempting the prepared child.
	srv.Close()
	srv = NewServer(cfg, nil, WithLLM(client))
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/run", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("reopen maintenance status=%d body=%s", w.Code, w.Body.String())
	}
	if client.calls != 2 {
		t.Fatalf("prepared child did not run exactly once: calls=%d", client.calls)
	}
	foundEvidence := false
	for _, prompt := range client.prompts {
		if strings.Contains(prompt, "prepared durable evidence") {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Fatalf("prepared evidence missing from handbook prompt: %+v", client.prompts)
	}
	handbook, err := agentruntime.HandbookRoot(cfg.RuntimeDir(), agentID, agentruntime.WorkspaceConfig{}, agentruntime.HandbookConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(handbook, "maintenance.md")); err != nil || string(raw) != "evidence" {
		t.Fatalf("recovered handbook file=%q err=%v", raw, err)
	}
	child, _ := srv.goalStore.GetMaintenanceReceipt("handbook:" + parentID)
	if child.PhaseState != goals.MaintenancePhaseComplete || child.UsedTokens != 4 {
		t.Fatalf("prepared child result=%+v", child)
	}
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/run", nil))
	if w.Code != http.StatusOK || client.calls != 2 {
		t.Fatalf("repeat reopen status=%d calls=%d body=%s", w.Code, client.calls, w.Body.String())
	}
}

func TestHandbookParentBacklogBeyondBatchRemainsPending(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client))
	defer srv.Close()
	const agentID = "handbook-parent-backlog"
	now := time.Now().UTC()
	snap := json.RawMessage(`{"agent_type":"auto","defaults":{"tools":{"enabled_groups":["fs"]}}}`)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.agents.MutateAgentPolicy(context.Background(), agentID, func(p *store.AgentPolicyRecord) error { p.Tools["write_file"] = "never"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC"}, 0, now); err != nil {
		t.Fatal(err)
	}
	for cursor := int64(1); cursor <= 9; cursor++ {
		id := testMaintenanceParentID(agentID, fmt.Sprintf("backlog-%d", cursor), cursor)
		if _, err := srv.goalStore.BeginMaintenance(agentID, id, fmt.Sprintf("backlog-%d", cursor), 0, now); err != nil {
			t.Fatal(err)
		}
		if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, id, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"durable backlog"}]}`), cursor, 0, false); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.goalStore.SettleMaintenance(agentID, id, 0, false, now); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(selectHandbookParents(srv.goalStore, agentID, 8)); got != 8 {
		t.Fatalf("selected parents=%d want 8", got)
	}
	if !hasPendingHandbookParents(srv.goalStore, agentID) {
		t.Fatal("ninth handbook parent was reported complete")
	}
	post := func() (int, string) {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/run", nil))
		return w.Code, w.Body.String()
	}
	if code, body := post(); code != http.StatusOK || !strings.Contains(body, `"status":"pending"`) || client.calls == 0 {
		t.Fatalf("first backlog batch code=%d calls=%d body=%s", code, client.calls, body)
	}
	firstCalls := client.calls
	if code, body := post(); code != http.StatusOK || !strings.Contains(body, `"status":"completed"`) || client.calls <= firstCalls {
		t.Fatalf("second backlog batch code=%d calls=%d body=%s", code, client.calls, body)
	}
	secondCalls := client.calls
	if code, body := post(); code != http.StatusOK || client.calls != secondCalls {
		t.Fatalf("duplicate backlog batch code=%d calls=%d body=%s", code, client.calls, body)
	}
}

func TestManualHTTPDoesNotResumeDailyPreparedHandbook(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client), WithMaintenanceExtractor(&apiMaintenanceExtractor{}))
	defer srv.Close()
	const agentID = "manual-daily-isolation"
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
		t.Fatalf("claim occurrence: %+v err=%v", claim, err)
	}
	parentID := "daily-prepared-parent"
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, "daily-source", 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"daily prepared evidence"}]}`), 2, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 5, now); err != nil {
		t.Fatal(err)
	}
	before, _ := srv.goalStore.GetMaintenanceReceipt("handbook:" + parentID)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/run", nil))
	if w.Code != http.StatusOK || client.calls != 0 {
		t.Fatalf("manual resumed daily handbook: status=%d calls=%d body=%s", w.Code, client.calls, w.Body.String())
	}
	after, _ := srv.goalStore.GetMaintenanceReceipt("handbook:" + parentID)
	if after.PhaseState != before.PhaseState || after.Status != before.Status || after.UsedTokens != before.UsedTokens {
		t.Fatalf("daily prepared child changed by manual run: before=%+v after=%+v", before, after)
	}
}

func TestManualHTTPDoesNotApplyDailyPendingMemoryReceipt(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client), WithMaintenanceExtractor(&apiMaintenanceExtractor{}))
	defer srv.Close()
	const agentID = "manual-daily-pending"
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC"}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim occurrence: %+v err=%v", claim, err)
	}
	parentID := "daily-pending-parent"
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, "daily-pending", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[{"information":"pending daily candidate"}]`), json.RawMessage(`{"messages":[{"role":"user","content":"pending daily evidence"}]}`), 4, 1, false); err != nil {
		t.Fatal(err)
	}
	before, _ := srv.goalStore.GetMaintenanceReceipt(parentID)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/run", nil))
	if w.Code != http.StatusOK || client.calls != 0 {
		t.Fatalf("manual touched daily pending receipt: status=%d calls=%d body=%s", w.Code, client.calls, w.Body.String())
	}
	after, _ := srv.goalStore.GetMaintenanceReceipt(parentID)
	if after.Status != before.Status || after.NextCursor != before.NextCursor || after.UsedTokens != before.UsedTokens || len(after.CandidateJSON) != len(before.CandidateJSON) {
		t.Fatalf("daily pending receipt changed by manual run: before=%+v after=%+v", before, after)
	}
}

func TestManualHTTPFiltersDailyParentsBeforeBatchLimit(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client), WithMaintenanceExtractor(&apiMaintenanceExtractor{}))
	defer srv.Close()
	const agentID = "manual-daily-pagination"
	now := time.Now().UTC()
	snap := json.RawMessage(`{"agent_type":"auto","defaults":{"tools":{"enabled_groups":["fs"]}}}`)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.agents.MutateAgentPolicy(context.Background(), agentID, func(p *store.AgentPolicyRecord) error { p.Tools["write_file"] = "never"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC"}, 0, now.Add(-9*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 8; i++ {
		claim, err := srv.goalStore.ClaimMaintenance(agentID, now.Add(-time.Duration(i)*24*time.Hour))
		if err != nil || claim.Occurrence == nil {
			t.Fatalf("daily claim %d: %+v err=%v", i, claim, err)
		}
		id := fmt.Sprintf("daily-pagination-%d", i)
		if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, id, id, 0, now); err != nil {
			t.Fatal(err)
		}
		if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, id, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"daily pagination"}]}`), int64(i), 0, false); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.goalStore.SettleMaintenance(agentID, id, 0, false, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.goalStore.BeginMaintenance(agentID, "manual-tail", "manual-tail", 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, "manual-tail", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"manual tail evidence"}]}`), 99, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, "manual-tail", 0, false, now); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/run", nil))
	if w.Code != http.StatusOK || client.calls == 0 {
		t.Fatalf("manual tail was hidden by daily page: status=%d calls=%d body=%s", w.Code, client.calls, w.Body.String())
	}
	for _, entry := range srv.goalStore.ListMaintenanceReceiptEntries(agentID) {
		if strings.HasPrefix(entry.Receipt.Fingerprint, "daily-pagination-") && entry.Receipt.HandbookReceiptID != "" {
			t.Fatalf("manual run touched daily parent: %+v", entry.Receipt)
		}
	}
}

func mustReceipt(t *testing.T, s *goals.Store, id string) goals.MaintenanceReceipt {
	t.Helper()
	r, ok := s.GetMaintenanceReceipt(id)
	if !ok {
		t.Fatalf("missing receipt %s", id)
	}
	return r
}

// maintenanceScriptedClient is finite and deterministic: the handbook phase
// performs one authorized write and then returns a terminal response.
type maintenanceScriptedClient struct {
	calls   int
	prompts []string
}

func (c *maintenanceScriptedClient) StreamChat(_ context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	for _, message := range req.Messages {
		if message.Role == "user" {
			c.prompts = append(c.prompts, message.Content)
		}
	}
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "write", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/maintenance.md","content":"evidence","call_purpose":"maintenance"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "completed", FinishReason: "stop"}, nil
}

func TestMaintenanceHTTPWritesHandbookAndKeepsAudit(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	now := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client), WithMaintenanceExtractor(&apiMaintenanceExtractor{}))
	defer srv.Close()
	id := "controller-real"
	snap := `{"agent_type":"auto","defaults":{"tools":{"enabled_groups":["fs"]}}}`
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(snap), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.agents.MutateAgentPolicy(context.Background(), id, func(p *store.AgentPolicyRecord) error { p.Tools["write_file"] = "never"; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	e := turn.NewTurnEventEnvelope("controller-source", turn.EventTurnCompleted, now)
	e.AgentID, e.TurnID, e.CommandID = id, "business-turn", "business-complete"
	if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "controller unique evidence"}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+id+"/maintenance/run", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	h, err := agentruntime.HandbookRoot(cfg.RuntimeDir(), id, agentruntime.WorkspaceConfig{}, agentruntime.HandbookConfig{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(h, "maintenance.md"))
	if err != nil || string(raw) != "evidence" {
		t.Fatalf("handbook=%q err=%v", raw, err)
	}
	u, _ := srv.goalStore.GetUsage(id)
	if u.MaintenanceTokens == 0 {
		t.Fatalf("usage=%+v", u)
	}
	foundEvidence := false
	for _, prompt := range client.prompts {
		if strings.Contains(prompt, "controller unique evidence") {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Fatalf("handbook prompt omitted business evidence: %+v", client.prompts)
	}
	firstCalls := client.calls
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+id+"/maintenance/run", nil))
	if w.Code != http.StatusOK || client.calls != firstCalls {
		t.Fatalf("unchanged run status=%d calls=%d want=%d", w.Code, client.calls, firstCalls)
	}
	for _, receipt := range srv.goalStore.ListMaintenanceReceipts(id) {
		if receipt.OccurrenceLocalDate != "" || receipt.OccurrenceScheduleRevision != 0 {
			t.Fatalf("manual receipt unexpectedly has occurrence scope: %+v", receipt)
		}
	}
}

func TestMaintenanceHTTPBudgetZeroSkipsHandbookLLM(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	now := time.Now().UTC()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client), WithMaintenanceExtractor(&apiMaintenanceExtractor{}))
	defer srv.Close()
	id := "controller-budget"
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 1, TotalTokenBudget: 1}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.RecordUsage(id, 0, 1, 0, now); err != nil {
		t.Fatal(err)
	}
	e := turn.NewTurnEventEnvelope("budget-source", turn.EventTurnCompleted, now)
	e.AgentID, e.TurnID, e.CommandID = id, "budget-business-turn", "budget-business-complete"
	if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "budget business evidence"}}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+id+"/maintenance/run", nil))
	if w.Code != 409 {
		t.Fatalf("status=%d", w.Code)
	}
	if client.calls != 0 {
		t.Fatalf("llm calls=%d", client.calls)
	}
}

func TestMaintenanceSchedulerWritesConfiguredHandbookAndDeduplicates(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	handbook := filepath.Join(t.TempDir(), "custom-handbook")
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client), WithMaintenanceExtractor(&apiMaintenanceExtractor{}))
	defer srv.Close()
	now := time.Now().UTC()
	id := "scheduler-configured"
	snapBytes, _ := json.Marshal(map[string]any{"agent_type": "auto", "handbook": map[string]any{"directory": handbook}, "defaults": map[string]any{"tools": map[string]any{"enabled_groups": []string{"fs"}}}})
	snap := string(snapBytes)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(snap), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.agents.MutateAgentPolicy(context.Background(), id, func(p *store.AgentPolicyRecord) error { p.Tools["write_file"] = "never"; return nil }); err != nil {
		t.Fatal(err)
	}
	profile, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	e := turn.NewTurnEventEnvelope("scheduler-source", turn.EventTurnCompleted, now)
	e.AgentID, e.TurnID, e.CommandID = id, "scheduler-business-turn", "scheduler-business-complete"
	if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: "scheduler unique evidence"}}); err != nil {
		t.Fatal(err)
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), now)
	raw, err := os.ReadFile(filepath.Join(handbook, "maintenance.md"))
	if client.calls == 0 {
		t.Fatalf("scheduler did not invoke handbook model")
	}
	if err != nil || string(raw) != "evidence" {
		t.Fatalf("handbook=%q err=%v", raw, err)
	}
	first := client.calls
	if first == 0 {
		t.Fatal("scheduler did not call handbook model")
	}
	usage, _ := srv.goalStore.GetUsage(id)
	if usage.MaintenanceTokens == 0 {
		t.Fatalf("usage=%+v", usage)
	}
	srv.maintenanceSched.RunOnceForTest(context.Background(), now)
	if client.calls != first {
		t.Fatalf("duplicate occurrence called model: %d -> %d", first, client.calls)
	}
	status, err := srv.goalStore.MaintenanceScheduleStatus(id, now)
	if err != nil || status.Last == nil {
		t.Fatalf("missing occurrence status: %+v err=%v", status, err)
	}
	entries := srv.goalStore.ListMaintenanceOccurrenceReceipts(id, status.Last.LocalDate, profile.MaintenanceRevision)
	var parent, child bool
	for _, entry := range entries {
		if entry.Receipt.ParentReceiptID == "" && entry.Receipt.HandbookReceiptID != "" {
			parent = true
		}
		if entry.Receipt.ParentReceiptID != "" {
			child = true
		}
	}
	if !parent || !child {
		t.Fatalf("occurrence receipts missing parent/child: %+v", entries)
	}
}
func (*maintenanceScriptedClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*maintenanceScriptedClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}
