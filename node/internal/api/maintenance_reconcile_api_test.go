package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestMaintenanceReconcileHTTPSuccessAndExactRetry(t *testing.T) {
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	client := &maintenanceScriptedClient{}
	srv := NewServer(cfg, nil, WithLLM(client))
	defer srv.Close()
	const agentID = "reconcile-http-agent"
	now := time.Now().UTC()
	snap := json.RawMessage(`{"agent_type":"auto"}`)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: agentID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 20, TotalTokenBudget: 20}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(agentID, now)
	if err != nil || claim.Occurrence == nil {
		t.Fatalf("claim=%+v err=%v", claim, err)
	}
	parentID, fingerprint, sessionID, turnID := "reconcile-parent", "reconcile-source", "reconcile-session", "reconcile-turn"
	ms, err := srv.openAgentMemoryService(agentID, &store.AgentRecord{AgentID: agentID, ConfigSnapshot: snap})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.ApplyMaintenanceOperation(context.Background(), memory.MaintenanceOperation{OperationID: parentID, AgentID: agentID, Scope: memory.ScopeAgent, SourceFingerprint: fingerprint, ExpectedCursor: 0, NextCursor: 3}, []memory.Candidate{}, memory.MaintenanceCursor{AgentID: agentID, Scope: memory.ScopeAgent, Sequence: 3, SourceFingerprint: fingerprint}); err != nil {
		ms.Close()
		t.Fatal(err)
	}
	ms.Close()
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, fingerprint, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(agentID, parentID, json.RawMessage(`[]`), json.RawMessage(`{"messages":[]}`), 3, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(agentID, parentID, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, agentID, 20, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	root, err := agentruntime.HandbookRoot(cfg.RuntimeDir(), agentID, agentruntime.WorkspaceConfig{}, agentruntime.HandbookConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.MarkHandbookRunning(agentID, childID, sessionID); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.BindHandbookTurnWithRoot(agentID, childID, sessionID, turnID, filepath.Clean(root), now); err != nil {
		t.Fatal(err)
	}
	appendMaintenanceJournal(t, srv.store, agentID, sessionID, turnID, true)
	fs, err := handbookfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := handbookfs.WithProvenance(context.Background(), handbookfs.Provenance{MaintenanceReceiptID: childID, SessionID: sessionID, TurnID: turnID})
	if _, err := fs.Write(ctx, filepath.Join(root, "guide.md"), "", []byte("guide")); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.FinishMaintenance(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "needs reconciliation", now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"local_date":"` + claim.Occurrence.LocalDate + `","schedule_revision":` + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10) + `,"token":"` + snapshot.Token + `","receipt_id":"` + childID + `"}`
	post := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+agentID+"/maintenance/reconcile", strings.NewReader(body)))
		return w
	}
	w := post()
	if w.Code != http.StatusOK {
		t.Fatalf("reconcile status=%d body=%s", w.Code, w.Body.String())
	}
	usageBefore, _ := srv.goalStore.GetUsage(agentID)
	w = post()
	if w.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", w.Code, w.Body.String())
	}
	usageAfter, _ := srv.goalStore.GetUsage(agentID)
	if usageAfter.MaintenanceTokens != usageBefore.MaintenanceTokens {
		t.Fatalf("retry charged usage: before=%+v after=%+v", usageBefore, usageAfter)
	}
	child, _ := srv.goalStore.GetMaintenanceReceipt(childID)
	if child.PhaseState != goals.MaintenancePhaseComplete || child.UsedTokens != 3 {
		t.Fatalf("child=%+v", child)
	}
	occ, _ := srv.goalStore.GetMaintenanceRecoverySnapshot(agentID, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if occ.Occurrence.Status != goals.MaintenanceOccurrenceRecoveryRequired {
		t.Fatalf("occurrence advanced unexpectedly: %+v", occ.Occurrence)
	}
	if client.calls != 0 {
		t.Fatalf("reconcile invoked LLM: %d", client.calls)
	}
	if _, err := os.Stat(filepath.Join(root, "guide.md")); err != nil {
		t.Fatal(err)
	}
}

type reconcileFixture struct {
	srv                           *Server
	id, childID, date, body, root string
}

func newReconcileFixture(t *testing.T, withUsage, pending bool) reconcileFixture {
	return newReconcileFixtureOptions(t, withUsage, pending, true)
}

func newReconcileFixtureOptions(t *testing.T, withUsage, pending, bindRoot bool) reconcileFixture {
	t.Helper()
	cfg := testConfig(t)
	settings, _ := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	_ = settings.Save(context.Background(), cfg)
	settings.Close()
	srv := NewServer(cfg, nil, WithLLM(&maintenanceScriptedClient{}))
	id := "reconcile-failure-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	now := time.Now().UTC()
	snap := json.RawMessage(`{"agent_type":"auto"}`)
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: snap, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC", MaintenanceTokenBudget: 20, TotalTokenBudget: 20}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	claim, err := srv.goalStore.ClaimMaintenance(id, now)
	if err != nil {
		t.Fatal(err)
	}
	parentID, fingerprint, sessionID, turnID := "failure-parent", "failure-source", "failure-session", "failure-turn"
	ms, err := srv.openAgentMemoryService(id, &store.AgentRecord{AgentID: id, ConfigSnapshot: snap})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ms.ApplyMaintenanceOperation(context.Background(), memory.MaintenanceOperation{OperationID: parentID, AgentID: id, Scope: memory.ScopeAgent, SourceFingerprint: fingerprint, ExpectedCursor: 0, NextCursor: 3}, []memory.Candidate{}, memory.MaintenanceCursor{AgentID: id, Scope: memory.ScopeAgent, Sequence: 3, SourceFingerprint: fingerprint}); err != nil {
		ms.Close()
		t.Fatal(err)
	}
	ms.Close()
	if _, err := srv.goalStore.BeginMaintenanceForOccurrence(id, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, parentID, fingerprint, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := srv.goalStore.SaveMaintenanceResultWithEvidence(id, parentID, json.RawMessage(`[]`), json.RawMessage(`{"messages":[]}`), 3, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SettleMaintenance(id, parentID, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.PrepareHandbook(parentID, id, 20, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	root, err := agentruntime.HandbookRoot(cfg.RuntimeDir(), id, agentruntime.WorkspaceConfig{}, agentruntime.HandbookConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.MarkHandbookRunning(id, childID, sessionID); err != nil {
		t.Fatal(err)
	}
	if bindRoot {
		if err := srv.goalStore.BindHandbookTurnWithRoot(id, childID, sessionID, turnID, filepath.Clean(root), now); err != nil {
			t.Fatal(err)
		}
	} else if err := srv.goalStore.BindHandbookTurn(id, childID, sessionID, turnID, now); err != nil {
		t.Fatal(err)
	}
	appendMaintenanceJournal(t, srv.store, id, sessionID, turnID, withUsage)
	fs, err := handbookfs.New(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := handbookfs.WithProvenance(context.Background(), handbookfs.Provenance{MaintenanceReceiptID: childID, SessionID: sessionID, TurnID: turnID})
	path := filepath.Join(root, "guide.md")
	if _, err := fs.Write(ctx, path, "", []byte("guide")); err != nil {
		t.Fatal(err)
	}
	if pending {
		if err := os.WriteFile(filepath.Join(root, ".history", "pending.json"), []byte(`{"revision":99}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.goalStore.FinishMaintenance(id, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision, goals.MaintenanceOccurrenceRecoveryRequired, "", "needs reconciliation", now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := srv.goalStore.GetMaintenanceRecoverySnapshot(id, claim.Occurrence.LocalDate, claim.Occurrence.ScheduleRevision)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"local_date":"` + claim.Occurrence.LocalDate + `","schedule_revision":` + strconv.FormatInt(claim.Occurrence.ScheduleRevision, 10) + `,"token":"` + snapshot.Token + `","receipt_id":"` + childID + `"}`
	return reconcileFixture{srv: srv, id: id, childID: childID, date: claim.Occurrence.LocalDate, body: body, root: root}
}

func TestMaintenanceReconcileRejectsUnknownUsageWithoutMutation(t *testing.T) {
	f := newReconcileFixture(t, false, false)
	defer f.srv.Close()
	beforeChild, _ := f.srv.goalStore.GetMaintenanceReceipt(f.childID)
	beforeUsage, _ := f.srv.goalStore.GetUsage(f.id)
	before, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, 1)
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+f.id+"/maintenance/reconcile", strings.NewReader(f.body)))
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	afterChild, _ := f.srv.goalStore.GetMaintenanceReceipt(f.childID)
	afterUsage, _ := f.srv.goalStore.GetUsage(f.id)
	after, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, before.Occurrence.ScheduleRevision)
	if !reflect.DeepEqual(afterChild, beforeChild) || !reflect.DeepEqual(afterUsage, beforeUsage) || after.Token != before.Token {
		t.Fatalf("failure mutated state child=%+v usage=%+v token=%q", afterChild, afterUsage, after.Token)
	}
}

func TestMaintenanceReconcileRejectsPendingAndPreservesFiles(t *testing.T) {
	f := newReconcileFixture(t, true, true)
	defer f.srv.Close()
	path := filepath.Join(f.root, "guide.md")
	before, _ := os.ReadFile(path)
	pendingBefore, _ := os.ReadFile(filepath.Join(f.root, ".history", "pending.json"))
	snap, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, 1)
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+f.id+"/maintenance/reconcile", strings.NewReader(f.body)))
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	after, _ := os.ReadFile(path)
	pendingAfter, _ := os.ReadFile(filepath.Join(f.root, ".history", "pending.json"))
	snapAfter, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, snap.Occurrence.ScheduleRevision)
	if string(after) != string(before) || string(pendingAfter) != string(pendingBefore) || snapAfter.Token != snap.Token {
		t.Fatalf("pending reconciliation changed files or token")
	}
}

func TestMaintenanceReconcileRejectsMissingBoundRoot(t *testing.T) {
	f := newReconcileFixtureOptions(t, true, false, false)
	defer f.srv.Close()
	snap, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, 1)
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+f.id+"/maintenance/reconcile", strings.NewReader(f.body)))
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	after, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, snap.Occurrence.ScheduleRevision)
	if after.Token != snap.Token {
		t.Fatal("missing root changed recovery snapshot")
	}
}

func TestMaintenanceReconcileRejectsBusyAndStaleOrCrossAgentRequest(t *testing.T) {
	f := newReconcileFixture(t, true, false)
	defer f.srv.Close()
	lease, release, acquired, err := f.srv.sessions.TryAcquireMaintenanceContext(context.Background(), f.id)
	if err != nil || !acquired {
		t.Fatalf("acquire maintenance gate: acquired=%v err=%v", acquired, err)
	}
	_ = lease
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+f.id+"/maintenance/reconcile", strings.NewReader(f.body)))
	release()
	if w.Code != http.StatusConflict {
		t.Fatalf("busy status=%d body=%s", w.Code, w.Body.String())
	}

	snapshot, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, 1)
	stale := strings.Replace(f.body, snapshot.Token, strings.Repeat("0", len(snapshot.Token)), 1)
	w = httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+f.id+"/maintenance/reconcile", strings.NewReader(stale)))
	if w.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", w.Code, w.Body.String())
	}
	// Build a real receipt owned by another Agent; an unknown ID would only
	// exercise the missing-record path rather than cross-Agent isolation.
	otherID := f.id + "-other"
	now := time.Now().UTC()
	if err := f.srv.agents.Save(context.Background(), store.AgentRecord{AgentID: otherID, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: otherID, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 00:00", Timezone: "UTC"}, 0, now.Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	otherClaim, err := f.srv.goalStore.ClaimMaintenance(otherID, now)
	if err != nil {
		t.Fatal(err)
	}
	otherParent := "other-parent-" + strconv.FormatInt(now.UnixNano(), 10)
	if _, err := f.srv.goalStore.BeginMaintenanceForOccurrence(otherID, otherClaim.Occurrence.LocalDate, otherClaim.Occurrence.ScheduleRevision, otherParent, "other-source", 0, now); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.goalStore.SaveMaintenanceResultWithEvidence(otherID, otherParent, json.RawMessage(`[]`), json.RawMessage(`{"messages":[]}`), 1, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.goalStore.SettleMaintenance(otherID, otherParent, 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.goalStore.PrepareHandbook(otherParent, otherID, 1, now); err != nil {
		t.Fatal(err)
	}
	otherChildID := "handbook:" + otherParent
	otherBefore, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(otherID, otherClaim.Occurrence.LocalDate, otherClaim.Occurrence.ScheduleRevision)
	cross := strings.Replace(f.body, f.childID, otherChildID, 1)
	w = httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+f.id+"/maintenance/reconcile", strings.NewReader(cross)))
	if w.Code != http.StatusConflict && w.Code != http.StatusNotFound {
		t.Fatalf("cross-agent status=%d body=%s", w.Code, w.Body.String())
	}
	firstAfter, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(f.id, f.date, snapshot.Occurrence.ScheduleRevision)
	otherAfter, _ := f.srv.goalStore.GetMaintenanceRecoverySnapshot(otherID, otherClaim.Occurrence.LocalDate, otherClaim.Occurrence.ScheduleRevision)
	if firstAfter.Token != snapshot.Token || otherAfter.Token != otherBefore.Token {
		t.Fatal("cross-agent request changed a recovery snapshot")
	}
}

func TestMaintenanceReconcileRejectsMissingMutationHistory(t *testing.T) {
	f := newReconcileFixture(t, true, false)
	defer f.srv.Close()
	if err := os.Remove(filepath.Join(f.root, ".history", "manifest.jsonl")); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	f.srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+f.id+"/maintenance/reconcile", strings.NewReader(f.body)))
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestValidateNoopMutationEvidence(t *testing.T) {
	if got, ok := normalizeHandbookEvidencePath("handbook/a/../guide.md"); !ok || got != "guide.md" {
		t.Fatalf("normalized handbook path=%q valid=%v", got, ok)
	}
	for _, value := range []string{"../outside", "/outside", `C:\outside`} {
		if _, ok := normalizeHandbookEvidencePath(value); ok {
			t.Fatalf("unsafe path accepted: %q", value)
		}
	}
	makeEvents := func(fact bool, mutate func(*map[string]any)) []turn.TurnEventEnvelope {
		callPayload := map[string]any{"tool_name": "write_file", "arguments_json": `{"path":"handbook/guide.md"}`}
		call := turn.TurnEventEnvelope{EventType: turn.EventToolCallRecorded, AgentID: "agent", SessionID: "session", TurnID: "turn", ToolCallID: "call", Payload: mustJSONReconcile(callPayload)}
		events := []turn.TurnEventEnvelope{call}
		if fact {
			content := map[string]any{"version": 1, "receipt_id": "receipt", "session_id": "session", "turn_id": "turn", "tool_call_id": "call", "tool_name": "write_file", "path": "guide.md", "digest": strings.Repeat("a", 64)}
			outer := map[string]any{"external_fact_kind": "handbook.noop", "result_content": string(mustJSON(content))}
			events = append(events, turn.TurnEventEnvelope{EventType: turn.EventExternalFactRecorded, AgentID: "agent", SessionID: "session", TurnID: "turn", ToolCallID: "call", Payload: mustJSONReconcile(outer)})
		}
		if mutate != nil {
			var p map[string]any
			_ = json.Unmarshal(events[1].Payload, &p)
			mutate(&p)
			events[1].Payload = mustJSONReconcile(p)
		}
		return events
	}
	if err := validateNoopMutationEvidence(makeEvents(true, nil), "agent", "session", "turn", "receipt"); err != nil {
		t.Fatalf("valid no-op rejected: %v", err)
	}
	for name, events := range map[string][]turn.TurnEventEnvelope{
		"missing": makeEvents(false, nil),
		"wrong source": makeEvents(true, func(p *map[string]any) {
			(*p)["result_content"] = strings.Replace((*p)["result_content"].(string), "receipt", "other", 1)
		}),
		"bad digest": makeEvents(true, func(p *map[string]any) {
			(*p)["result_content"] = strings.Replace((*p)["result_content"].(string), strings.Repeat("a", 64), "bad", 1)
		}),
	} {
		if err := validateNoopMutationEvidence(events, "agent", "session", "turn", "receipt"); err == nil {
			t.Fatalf("%s evidence accepted", name)
		}
	}
}

func mustJSONReconcile(value any) []byte { b, _ := json.Marshal(value); return b }

type apiNoopClient struct{ calls int }

func (c *apiNoopClient) StreamChat(_ context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "noop-write", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/guide.md","content":"stable","call_purpose":"maintain"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}
func (*apiNoopClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*apiNoopClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

func TestValidateNoopMutationEvidenceFromRealSessionSQLiteEvents(t *testing.T) {
	workspace, handbook := t.TempDir(), t.TempDir()
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(handbook); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(handbook, "guide.md"), []byte("stable"), 0644); err != nil {
		t.Fatal(err)
	}
	turnStore, err := store.Open(filepath.Join(workspace, "turn-events.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer turnStore.Close()
	client := &apiNoopClient{}
	allow := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeNever}})
	mgr := session.NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, allow, turnStore, session.TurnOptions{AutoAgent: true}, logx.Discard())
	defer mgr.Stop()
	rt, _, err := mgr.CreateWithOptionsAndLLM("maintenance-noop-api", session.TurnOptions{AutoAgent: true}, reg, nil, client, "agent-1")
	if err != nil {
		t.Fatal(err)
	}
	lease, release, acquired, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !acquired {
		t.Fatalf("gate: %v %v", acquired, err)
	}
	defer release()
	lease = handbookfs.WithProvenance(lease, handbookfs.Provenance{MaintenanceReceiptID: "receipt-noop", SessionID: rt.ID})
	var sid, tid string
	result, err := mgr.RunHandbookMaintenanceWithBinding(lease, rt.ID, "整理手册", turn.TurnBudget{MaxSteps: 2, MaxTotalTokens: 100}, func(sessionID, turnID string) error { sid, tid = sessionID, turnID; return nil })
	if err != nil || result.Changed || sid == "" || tid == "" {
		t.Fatalf("result=%+v sid=%q tid=%q err=%v", result, sid, tid, err)
	}
	events, err := turnStore.ListTurnEventsForTurn(context.Background(), sid, tid)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateNoopMutationEvidence(events, "agent-1", sid, tid, "receipt-noop"); err != nil {
		t.Fatalf("real session no-op evidence rejected: %v", err)
	}
}
