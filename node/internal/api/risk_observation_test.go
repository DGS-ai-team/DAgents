package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

type riskAPIClient struct{ calls atomic.Int32 }

func (c *riskAPIClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	c.calls.Add(1)
	return `{"level":"low","reason":"ok","recommendation":"allow"}`, nil
}
func (c *riskAPIClient) CompleteTextWithUsage(ctx context.Context, req llm.CompleteRequest) (string, *llm.Usage, error) {
	text, err := c.CompleteText(ctx, req)
	return text, &llm.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, err
}
func (c *riskAPIClient) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{}, nil
}
func (c *riskAPIClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

type riskGoalLLM struct {
	riskCalls atomic.Int32
	toolSeen  chan struct{}
	toolDone  chan struct{}
	riskStart chan struct{}
	riskDone  chan struct{}
	turns     atomic.Int32
}

func (c *riskGoalLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return `{"level":"low","reason":"ok","recommendation":"allow"}`, nil
}
func (c *riskGoalLLM) CompleteTextWithUsage(ctx context.Context, req llm.CompleteRequest) (string, *llm.Usage, error) {
	c.riskCalls.Add(1)
	if c.riskStart != nil {
		close(c.riskStart)
		select {
		case <-ctx.Done():
			if c.riskDone != nil {
				close(c.riskDone)
			}
			return "", nil, ctx.Err()
		}
	}
	text, err := c.CompleteText(ctx, req)
	return text, &llm.Usage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}, err
}

func TestRiskObservationRuntimeCloseCancelsBlockedRiskHost(t *testing.T) {
	cfg := testConfig(t)
	fake := &riskGoalLLM{toolSeen: make(chan struct{}), riskStart: make(chan struct{}), riskDone: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	if srv.maintenanceSched != nil {
		srv.maintenanceSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv.agents = as
	timeNow := time.Now().UTC()
	rec := store.AgentRecord{AgentID: "risk-close-agent", DisplayName: "risk close", RuntimeRevision: 1, CreatedAt: timeNow, UpdatedAt: timeNow,
		ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","workspace":{"mode":"private"},"defaults":{"tools":{"enabled_groups":["fs"]},"hooks":{"risk_observation_enabled":true}}}`)}
	if err := as.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"objective": "read", "acceptance": "finish", "agent_id": rec.AgentID, "enabled": true})
	created := createGoalViaAutonomy(srv, rec.AgentID, body)
	if created.Code != http.StatusOK {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var goal struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &goal); err != nil {
		t.Fatal(err)
	}
	wake := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wake, httptest.NewRequest(http.MethodPost, "/v1/goals/"+goal.ID+"/wake", nil))
	if wake.Code != http.StatusAccepted {
		t.Fatalf("wake=%d %s", wake.Code, wake.Body.String())
	}
	select {
	case <-fake.riskStart:
	case <-time.After(3 * time.Second):
		t.Fatal("risk host did not start")
	}
	closed := make(chan struct{})
	go func() { srv.Close(); close(closed) }()
	select {
	case <-fake.riskDone:
	case <-time.After(3 * time.Second):
		t.Fatal("risk host context was not cancelled")
	}
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("server close hung")
	}
}

func TestRiskObservationEnsureGoalRuntimeDefaultOffReadTool(t *testing.T) {
	cfg := testConfig(t)
	fake := &riskGoalLLM{toolSeen: make(chan struct{}), toolDone: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer srv.Close()
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	if srv.maintenanceSched != nil {
		srv.maintenanceSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv.agents = as
	now := time.Now().UTC()
	rec := store.AgentRecord{AgentID: "risk-off-agent", DisplayName: "risk off", RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now,
		ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","workspace":{"mode":"private"},"defaults":{"tools":{"enabled_groups":["fs"]}}}`)}
	if err := as.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	workspace, err := agentruntime.EffectiveWorkspaceRoot(cfg.RuntimeDir(), rec.AgentID, agentruntime.WorkspaceConfig{Mode: agentruntime.WorkspaceModePrivate})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "fixture.txt"), []byte("risk fixture content"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"objective": "read", "acceptance": "finish", "agent_id": rec.AgentID, "enabled": true})
	created := createGoalViaAutonomy(srv, rec.AgentID, body)
	if created.Code != http.StatusOK {
		t.Fatalf("create=%d %s", created.Code, created.Body.String())
	}
	var goal struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &goal); err != nil {
		t.Fatal(err)
	}
	wake := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wake, httptest.NewRequest(http.MethodPost, "/v1/goals/"+goal.ID+"/wake", nil))
	if wake.Code != http.StatusAccepted {
		t.Fatalf("wake=%d %s", wake.Code, wake.Body.String())
	}
	select {
	case <-fake.toolSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("goal runtime did not invoke read_file")
	}
	select {
	case <-fake.toolDone:
	case <-time.After(3 * time.Second):
		t.Fatal("read_file result was not returned to model")
	}
	if fake.riskCalls.Load() != 0 {
		t.Fatalf("default-off risk calls=%d", fake.riskCalls.Load())
	}
}
func (c *riskGoalLLM) StreamChat(ctx context.Context, req llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	if c.turns.Add(1) == 1 {
		close(c.toolSeen)
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "read-call", Type: "function", Function: llm.ToolCallFunction{
			Name: "read_file", Arguments: `{"path":"fixture.txt","call_purpose":"inspect"}`,
		}}}, FinishReason: "tool_calls"}, nil
	}
	for _, msg := range req.Messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "risk fixture content") && c.toolDone != nil {
			close(c.toolDone)
		}
	}
	select {
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	default:
		return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
	}
}

func (c *riskGoalLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestRiskObservationEnsureGoalRuntimeThroughReadTool(t *testing.T) {
	cfg := testConfig(t)
	fake := &riskGoalLLM{toolSeen: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer srv.Close()
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	if srv.maintenanceSched != nil {
		srv.maintenanceSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv.agents = as
	now := time.Now().UTC()
	rec := store.AgentRecord{AgentID: "risk-goal-agent", DisplayName: "risk goal", RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now,
		ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","workspace":{"mode":"private"},"defaults":{"tools":{"enabled_groups":["fs"]},"hooks":{"risk_observation_enabled":true}}}`)}
	if err := srv.agents.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	workspace, err := agentruntime.EffectiveWorkspaceRoot(cfg.RuntimeDir(), rec.AgentID, agentruntime.WorkspaceConfig{Mode: agentruntime.WorkspaceModePrivate})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "fixture.txt"), []byte("risk fixture content"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"objective": "read a file", "acceptance": "finish", "agent_id": rec.AgentID, "enabled": true})
	created := createGoalViaAutonomy(srv, rec.AgentID, body)
	if created.Code != http.StatusOK {
		t.Fatalf("create=%d body=%s", created.Code, created.Body.String())
	}
	var goal struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &goal); err != nil || goal.ID == "" {
		t.Fatalf("goal=%s err=%v", created.Body.String(), err)
	}
	wake := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wake, httptest.NewRequest(http.MethodPost, "/v1/goals/"+goal.ID+"/wake", nil))
	if wake.Code != http.StatusAccepted {
		t.Fatalf("wake=%d body=%s", wake.Code, wake.Body.String())
	}
	select {
	case <-fake.toolSeen:
	case <-time.After(3 * time.Second):
		t.Fatal("goal runtime did not invoke read_file")
	}
	deadline := time.Now().Add(3 * time.Second)
	for (fake.riskCalls.Load() == 0 || srv.goalStore == nil || len(srv.goalStore.ListRiskObservations(rec.AgentID, 10)) == 0) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fake.riskCalls.Load() != 1 {
		t.Fatalf("risk calls=%d; goal runtime was not observed", fake.riskCalls.Load())
	}
	if srv.goalStore == nil || len(srv.goalStore.ListRiskObservations(rec.AgentID, 10)) != 1 {
		t.Fatal("goal runtime observation was not persisted")
	}
	usage, ok := srv.goalStore.GetUsage(rec.AgentID)
	if !ok || usage.RiskReviewTokens != 5 {
		t.Fatalf("risk usage=%+v found=%v", usage, ok)
	}
}

func riskAPIFixture(t *testing.T, enabled bool) (*Server, store.AgentRecord, *goals.Store, *riskAPIClient) {
	t.Helper()
	cfg := testConfig(t)
	srv := NewServer(cfg, nil, WithSkipStore())
	t.Cleanup(func() { srv.Close() })
	// NewServer starts the reconciliation ticker before test-only dependency
	// replacement; stop it before installing the fixture stores.
	if srv.triggerSched != nil {
		srv.triggerSched.Stop()
	}
	if srv.maintenanceSched != nil {
		srv.maintenanceSched.Stop()
	}
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv.agents = as
	now := time.Now().UTC()
	rec := store.AgentRecord{AgentID: "risk-api-agent", DisplayName: "risk", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}
	if enabled {
		rec.ConfigSnapshot = json.RawMessage(`{"agent_type":"auto","defaults":{"hooks":{"risk_observation_enabled":true}}}`)
	}
	if err := srv.agents.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	gs, err := goals.OpenStore(filepath.Join(cfg.RuntimeDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gs.SaveProfile(goals.AutoProfile{AgentID: rec.AgentID, Enabled: true, TotalTokenBudget: 10000}, 0, now); err != nil {
		t.Fatal(err)
	}
	srv.goalStore = gs
	client := &riskAPIClient{}
	return srv, rec, gs, client
}

func TestRiskObservationProductionAdapterAndHTTPDTO(t *testing.T) {
	srv, rec, gs, client := riskAPIFixture(t, true)
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	var opts session.TurnOptions
	agentruntime.ApplyDefaultsToTurnOptions(&opts, snap)
	if !opts.RiskObservationEnabled {
		t.Fatal("risk observation config was not applied")
	}
	srv.attachRiskObserver(&opts, client, rec, snap)
	d, ok := opts.RiskSubmitter.(*hooks.RiskDispatcher)
	if !ok || d == nil {
		t.Fatalf("risk dispatcher not attached: %T", opts.RiskSubmitter)
	}
	if !opts.RiskSubmitter.Submit(hooks.RiskObservationInput{RequestID: "api:req:1", ToolName: "read_file", PolicyAction: "allow", ArgsJSON: []byte(`{"path":"safe.txt"}`)}) {
		t.Fatal("risk submit rejected")
	}
	deadline := time.Now().Add(2 * time.Second)
	for client.calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	d.Close()
	if client.calls.Load() != 1 {
		t.Fatalf("risk calls=%d", client.calls.Load())
	}
	if got := len(gs.ListRiskObservations(rec.AgentID, 10)); got != 1 {
		t.Fatalf("persisted observations=%d", got)
	}

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/agents/"+rec.AgentID+"/risk-observations", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body struct {
		AgentID      string               `json:"agent_id"`
		Observations []riskObservationDTO `json:"observations"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.AgentID != rec.AgentID || len(body.Observations) != 1 || body.Observations[0].ToolName != "read_file" {
		t.Fatalf("body=%+v", body)
	}
	if body.Observations[0].ArgsDigest == "" {
		t.Fatal("missing args digest")
	}
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/agents/"+rec.AgentID+"/risk-observations?limit=bad", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid limit status=%d", rr.Code)
	}
}

func TestRiskObservationDefaultOffDoesNotAttachOrCall(t *testing.T) {
	srv, rec, _, client := riskAPIFixture(t, false)
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	var opts session.TurnOptions
	agentruntime.ApplyDefaultsToTurnOptions(&opts, snap)
	srv.attachRiskObserver(&opts, client, rec, snap)
	if opts.RiskObservationEnabled || opts.RiskSubmitter != nil {
		t.Fatalf("default risk observer attached: enabled=%v submitter=%T", opts.RiskObservationEnabled, opts.RiskSubmitter)
	}
	if client.calls.Load() != 0 {
		t.Fatal("default-off risk host was called")
	}
}
