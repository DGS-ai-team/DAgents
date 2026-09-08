package api

import (
	"context"
	"encoding/json"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type goalWakeLLM struct {
	called  chan struct{}
	release chan struct{}
}

func (f *goalWakeLLM) NormalizeAssistant(existing []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, m)
}
func (f *goalWakeLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (f *goalWakeLLM) StreamChat(ctx context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	f.called <- struct{}{}
	select {
	case <-f.release:
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
	if h.OnDelta != nil {
		h.OnDelta("observed")
	}
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	return llm.ChatResult{Content: "observed", FinishReason: "stop"}, nil
}

func TestGoalWakeUsesDedicatedRuntimeAndManagedClaim(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	as, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer as.Close()
	fake := &goalWakeLLM{called: make(chan struct{}, 2), release: make(chan struct{})}
	srv := NewServer(cfg, nil, WithLLM(fake), WithSkipStore())
	defer srv.sessions.Stop()
	defer func() {
		if srv.feedbackStore != nil {
			_ = srv.feedbackStore.Close()
		}
	}()
	srv.agents = as
	rec := store.AgentRecord{AgentID: "agent-goal", DisplayName: "goal", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), RuntimeRevision: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := as.Save(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"objective": "observe", "acceptance": "checkpoint", "agent_id": rec.AgentID, "enabled": true})
	rr := httptest.NewRecorder()
	rr = createGoalViaAutonomy(srv, rec.AgentID, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("create=%d %s", rr.Code, rr.Body.String())
	}
	var g struct {
		ID        string `json:"id"`
		SessionID string `json:"session_id"`
		AgentID   string `json:"agent_id"`
		TriggerID string `json:"trigger_id"`
	}
	if json.Unmarshal(rr.Body.Bytes(), &g) != nil || g.SessionID == "" || g.AgentID != rec.AgentID || g.TriggerID == "" {
		t.Fatalf("goal=%s", rr.Body.String())
	}
	if g.SessionID == rec.AgentID {
		t.Fatal("goal reused agent session")
	}
	wake := httptest.NewRecorder()
	srv.Handler().ServeHTTP(wake, httptest.NewRequest(http.MethodPost, "/v1/goals/"+g.ID+"/wake", nil))
	if wake.Code != http.StatusAccepted {
		t.Fatalf("wake=%d %s", wake.Code, wake.Body.String())
	}
	select {
	case <-fake.called:
	case <-time.After(5 * time.Second):
		t.Fatal("fake LLM was not called")
	}
	second := httptest.NewRecorder()
	srv.Handler().ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/v1/goals/"+g.ID+"/wake", nil))
	if second.Code != http.StatusConflict {
		t.Fatalf("duplicate wake=%d %s", second.Code, second.Body.String())
	}
	close(fake.release)
}
