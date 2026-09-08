package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestP0EnsureTriggerTargetLoadsBoundAgentRuntime(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{NodeID: "node-p0", RuntimeRoot: filepath.Join(root, "runtime")}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	on := true
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		"a": {Provider: "mock", Model: "model-a", Mock: true, MultimodalEnabled: &on},
		"b": {Provider: "mock", Model: "model-b", Mock: true, MultimodalEnabled: &on},
	}
	cfg.LLM.ProfileOrder = []string{"a", "b"}
	cfg.LLM.Active = "a"
	if err := cfg.SetActiveLLMProfile("a"); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agents
	t.Cleanup(func() { srv.sessions.Stop(); _ = agents.Close() })
	create := func(name, profile string) string {
		body, _ := json.Marshal(map[string]any{"display_name": name, "defaults": map[string]any{"llm": map[string]any{"active": profile}}})
		r := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
		w := httptest.NewRecorder()
		srv.Handler().ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
		}
		var view agentView
		_ = json.Unmarshal(w.Body.Bytes(), &view)
		return view.AgentID
	}
	a, b := create("A", "a"), create("B", "b")
	if _, err := srv.sessions.Release(b); err != nil {
		t.Fatalf("release B runtime for cold-load check: %v", err)
	}
	if srv.sessions.Get(b) != nil {
		t.Fatalf("B runtime was unexpectedly loaded before target routing")
	}
	if err := srv.ensureAgentRuntime(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	digestA := srv.sessions.RuntimeLLMProfileDigest(a)
	if digestA == "" {
		t.Fatal("A runtime digest is empty after ensure")
	}
	triggerSubmitter := &session.TriggerSubmitter{Mgr: srv.sessions, EnsureAgentRuntime: func(id string) error { return srv.ensureAgentRuntime(context.Background(), id) }}
	sessID, err := triggerSubmitter.EnsureSessionForAgent(b, "")
	if err != nil {
		t.Fatal(err)
	}
	sess := srv.sessions.Get(sessID)
	if sess == nil || sess.AgentID != b {
		t.Fatalf("target session = %+v, want agent %s", sess, b)
	}
	if err := srv.ensureAgentRuntime(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if got := srv.sessions.RuntimeLLMProfileDigest(b); got == "" || got == digestA {
		t.Fatalf("B runtime did not load distinct bound profile: A=%q B=%q", digestA, got)
	}
}
