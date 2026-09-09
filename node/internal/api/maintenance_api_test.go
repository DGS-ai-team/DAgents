package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type apiMaintenanceExtractor struct{ calls atomic.Int32 }
type apiCountingLLM struct {
	llm.Client
	calls atomic.Int32
}

func (c *apiCountingLLM) StreamChat(ctx context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls.Add(1)
	return c.Client.StreamChat(ctx, req, h)
}
func (c *apiCountingLLM) CompleteText(ctx context.Context, req llm.CompleteRequest) (string, error) {
	c.calls.Add(1)
	return c.Client.CompleteText(ctx, req)
}

type blockingMaintenanceExtractor struct {
	started chan struct{}
	proceed chan struct{}
	calls   atomic.Int32
}

func (e *blockingMaintenanceExtractor) ExtractWithUsage(ctx context.Context, in memory.ExtractionInput) ([]memory.Candidate, *llm.Usage, error) {
	e.calls.Add(1)
	close(e.started)
	<-e.proceed
	return nil, &llm.Usage{TotalTokens: 1}, nil
}

func (e *apiMaintenanceExtractor) ExtractWithUsage(context.Context, memory.ExtractionInput) ([]memory.Candidate, *llm.Usage, error) {
	n := e.calls.Add(1)
	return []memory.Candidate{{Request: memory.RememberRequest{Information: "api maintenance fact " + string(rune('0'+n))}}}, &llm.Usage{TotalTokens: 3}, nil
}

func TestMaintenanceAPIProcessesSequentialSnapshotsAndSkipsUnchanged(t *testing.T) {
	cfg := testConfig(t)
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	settings.Close()
	ext := &apiMaintenanceExtractor{}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithMaintenanceExtractor(ext))
	defer srv.Close()
	now := time.Now().UTC()
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: "maint-api", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	profile, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: "maint-api", Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	appendSnapshot := func(seq string) {
		e := turn.NewTurnEventEnvelope("maint-session", turn.EventTurnCompleted, now)
		e.AgentID, e.TurnID, e.CommandID = "maint-api", "turn-"+seq, "complete-"+seq
		if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), e, []llm.Message{{Role: "user", Content: seq}}); err != nil {
			t.Fatal(err)
		}
	}
	appendSnapshot("1")
	appendSnapshot("2")
	post := func() map[string]any {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/maint-api/maintenance/run", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("run status=%d body=%s", w.Code, w.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	if got := post()["sequence"]; got != float64(1) {
		t.Fatalf("first sequence=%v", got)
	}
	if got := post()["sequence"]; got != float64(2) {
		t.Fatalf("second sequence=%v", got)
	}
	if got := post()["sequence"]; got != float64(2) {
		t.Fatalf("unchanged sequence=%v", got)
	}
	if ext.calls.Load() != 2 {
		t.Fatalf("extract calls=%d", ext.calls.Load())
	}
	ms, err := srv.openAgentMemoryService("maint-api", &store.AgentRecord{AgentID: "maint-api", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`)})
	if err != nil {
		t.Fatal(err)
	}
	defer ms.Close()
	cursor, err := ms.GetMaintenanceCursor(context.Background())
	if err != nil || cursor.Sequence != 2 {
		t.Fatalf("cursor=%+v err=%v", cursor, err)
	}
	entries, err := ms.List(context.Background(), memory.ScopeAgent, true)
	if err != nil || len(entries) != 2 {
		t.Fatalf("memory entries=%d err=%v", len(entries), err)
	}
	usage, ok := srv.goalStore.GetUsage("maint-api")
	if !ok || usage.MaintenanceTokens != 6 {
		t.Fatalf("usage=%+v ok=%v", usage, ok)
	}

	get := httptest.NewRecorder()
	srv.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/agents/maint-api/maintenance", nil))
	if get.Code != http.StatusOK || !bytesContains(get.Body.Bytes(), []byte(`"maintenance_enabled":true`)) {
		t.Fatalf("get status=%d body=%s", get.Code, get.Body.String())
	}
	patchBody := `{"expected_revision":` + strconv.FormatInt(profile.Revision, 10) + `,"maintenance_schedule":"daily 10:30"}`
	patch := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v1/agents/maint-api/maintenance", strings.NewReader(patchBody))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(patch, req)
	if patch.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patch.Code, patch.Body.String())
	}
	stale := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPatch, "/v1/agents/maint-api/maintenance", strings.NewReader(`{"expected_revision":1,"maintenance_schedule":"daily 11:00"}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(stale, req)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func TestMaintenanceAPIHoldsGateUntilExtractorReturns(t *testing.T) {
	cfg := testConfig(t)
	settings, err := store.OpenNodeSettings(cfg.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	settings.Close()
	chatLLM := &apiCountingLLM{Client: &llm.MockClient{}}
	ext := &blockingMaintenanceExtractor{started: make(chan struct{}), proceed: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(chatLLM), WithMaintenanceExtractor(ext))
	defer srv.Close()
	now := time.Now().UTC()
	id := "maint-gated"
	if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.goalStore.SaveProfile(goals.AutoProfile{AgentID: id, Enabled: true, MaintenanceEnabled: true, MaintenanceSchedule: "daily 09:00", Timezone: "UTC", MaintenanceTokenBudget: 100, TotalTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.sessions.CreateWithOptionsAndLLM(id+"-chat", srv.sessions.DefaultTurnOptions(), srv.sessions.DefaultTools(), nil, chatLLM, id); err != nil {
		t.Fatal(err)
	}
	env := turn.NewTurnEventEnvelope(id+"-snapshot", turn.EventTurnCompleted, now)
	env.AgentID = id
	env.TurnID = "t1"
	env.CommandID = "c1"
	if _, err := srv.store.AppendTurnEventWithSnapshot(context.Background(), env, []llm.Message{{Role: "user", Content: "old"}}); err != nil {
		t.Fatal(err)
	}
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/agents/"+id+"/maintenance/run", nil))
		done <- w.Code
	}()
	select {
	case <-ext.started:
	case <-time.After(time.Second):
		t.Fatal("extractor did not start")
	}
	if _, err := srv.sessions.EnqueueMessage(context.Background(), id+"-chat", "message", "chat", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if got := chatLLM.calls.Load(); got != 0 {
		t.Fatalf("chat LLM called before extractor release: %d", got)
	}
	close(ext.proceed)
	select {
	case code := <-done:
		if code != http.StatusOK && code != http.StatusConflict {
			t.Fatalf("maintenance status=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("maintenance did not finish")
	}
	deadlineCalls := time.After(2 * time.Second)
	for chatLLM.calls.Load() < 1 {
		select {
		case <-deadlineCalls:
			t.Fatal("chat LLM did not run after release")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	// The chat is released only after RunOnce and its deferred lease release.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("chat did not complete")
		default:
			time.Sleep(10 * time.Millisecond)
		}
		pending, active, _, _ := srv.sessions.RuntimeInfo(id + "-chat")
		if pending == 0 && !active {
			break
		}
	}
}

func bytesContains(haystack, needle []byte) bool {
	return strings.Contains(string(haystack), string(needle))
}
