package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestPhase2_agentMessageByAgentID(t *testing.T) {
	// 使用独立目录，避免 session 后台写入导致 testing.TempDir 清理失败。
	root, err := os.MkdirTemp("", "dagents-phase2-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	cfg := &config.Config{NodeID: "node-test", FSRoot: filepath.Join(root, "runtime")}
	cfg.ApplyDefaults()
	cfg.LLM.Mock = true

	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}

	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "code-reviewer.yaml"), []byte(`
id: code-reviewer
display_name: 审查
defaults:
  tools:
    enabled_groups: [fs, skills]
`), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB
	t.Cleanup(func() {
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
		_ = agentsDB.Close()
	})

	body, _ := json.Marshal(map[string]any{
		"template_id":  "code-reviewer",
		"display_name": "审查助手",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}

	fsRoot, ok := srv.sessions.SessionFSRoot(created.AgentID)
	if !ok {
		t.Fatal("runtime missing")
	}
	wantFSRoot := filepath.Join(cfg.FSRoot, "agents", created.AgentID, "workspace")
	if fsRoot != wantFSRoot {
		t.Fatalf("fsRoot=%q want Agent workspace %q", fsRoot, wantFSRoot)
	}

	msgBody, _ := json.Marshal(map[string]any{
		"agent_id":     created.AgentID,
		"request_type": "message",
		"content":      "hello agent",
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(msgBody))
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("message status=%d body=%s", rr.Code, rr.Body.String())
	}
	var msgResp postMessageResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &msgResp)
	if msgResp.AgentID != created.AgentID {
		t.Fatalf("msgResp=%+v", msgResp)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		_, hasTurn, _, err := srv.sessions.RuntimeInfo(created.AgentID)
		if err == nil && !hasTurn {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents/"+created.AgentID+"/hydrate", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("hydrate status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "agent_id") && !strings.Contains(rr.Body.String(), created.AgentID) {
		t.Fatalf("hydrate body unexpected: %s", rr.Body.String())
	}
}

func TestPhase3_ensureAgentRuntimeAfterRelease(t *testing.T) {
	root, err := os.MkdirTemp("", "dagents-phase3-ensure-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	cfg := &config.Config{NodeID: "node-test", FSRoot: filepath.Join(root, "runtime")}
	cfg.ApplyDefaults()
	cfg.LLM.Mock = true

	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}

	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "code-reviewer.yaml"), []byte(`
id: code-reviewer
display_name: 审查
defaults:
  tools:
    enabled_groups: [fs, skills]
`), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB
	t.Cleanup(func() {
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
		_ = agentsDB.Close()
	})

	body, _ := json.Marshal(map[string]any{
		"template_id":  "code-reviewer",
		"display_name": "审查助手",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	wantFS := filepath.Join(cfg.FSRoot, "agents", created.AgentID, "workspace")
	if fs, ok := srv.sessions.SessionFSRoot(created.AgentID); !ok || fs != wantFS {
		t.Fatalf("initial fsRoot=%q ok=%v want %q", fs, ok, wantFS)
	}

	if _, err := srv.sessions.Release(created.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, ok := srv.sessions.SessionFSRoot(created.AgentID); ok {
		t.Fatal("expected runtime released")
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/agents/"+created.AgentID+"/ensure", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ensure status=%d body=%s", rr.Code, rr.Body.String())
	}
	fsRoot, ok := srv.sessions.SessionFSRoot(created.AgentID)
	if !ok {
		t.Fatal("runtime missing after ensure")
	}
	if fsRoot != wantFS {
		t.Fatalf("fsRoot after ensure=%q want %q", fsRoot, wantFS)
	}

	// hydrate 也应隐式 ensure
	if _, err := srv.sessions.Release(created.AgentID); err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/agents/"+created.AgentID+"/hydrate", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("hydrate status=%d body=%s", rr.Code, rr.Body.String())
	}
	if fs, ok := srv.sessions.SessionFSRoot(created.AgentID); !ok || fs != wantFS {
		t.Fatalf("hydrate ensure fsRoot=%q ok=%v", fs, ok)
	}
}

func TestEnsureAgentRuntimeReappliesBoundLLMProfileWhenRevisionIsUnchanged(t *testing.T) {
	root, err := os.MkdirTemp("", "dagents-agent-llm-focus-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	cfg := &config.Config{NodeID: "node-test", FSRoot: filepath.Join(root, "runtime")}
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		"profile-a": {Provider: "mock", Model: "model-a", Mock: true},
		"profile-b": {Provider: "mock", Model: "model-b", Mock: true},
	}
	cfg.LLM.ProfileOrder = []string{"profile-a", "profile-b"}
	cfg.LLM.Active = "profile-a"
	cfg.ApplyDefaults()
	if err := cfg.SetActiveLLMProfile("profile-a"); err != nil {
		t.Fatal(err)
	}

	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB
	t.Cleanup(func() {
		if srv.sessions != nil {
			srv.sessions.Stop()
		}
		_ = agentsDB.Close()
	})

	create := func(name, profile string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"display_name": name,
			"defaults": map[string]any{
				"llm": map[string]any{"active": profile},
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("create %s status=%d body=%s", name, rr.Code, rr.Body.String())
		}
		var view agentView
		if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		return view.AgentID
	}

	agentA := create("Agent A", "profile-a")
	agentB := create("Agent B", "profile-b")
	if got := cfg.LLM.ActiveProfileID(); got != "profile-b" {
		t.Fatalf("after creating B active profile=%q", got)
	}

	// Both runtimes are already loaded and their revisions are unchanged. This
	// is the path that used to return early and leave profile-b active for A.
	for _, tc := range []struct {
		id   string
		want string
	}{
		{agentA, "profile-a"},
		{agentB, "profile-b"},
	} {
		req := httptest.NewRequest(http.MethodPost, "/v1/agents/"+tc.id+"/ensure", nil)
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("ensure %s status=%d body=%s", tc.id, rr.Code, rr.Body.String())
		}
		if got := cfg.LLM.ActiveProfileID(); got != tc.want {
			t.Fatalf("ensure %s active profile=%q want %q", tc.id, got, tc.want)
		}
	}
}

func TestResolveAgentID(t *testing.T) {
	id, err := resolveAgentID("agt-1")
	if err != nil || id != "agt-1" {
		t.Fatalf("%q %v", id, err)
	}
	if _, err := resolveAgentID(""); err == nil {
		t.Fatal("expected required error")
	}
	if _, err := resolveAgentID("  "); err == nil {
		t.Fatal("expected required error")
	}
}
