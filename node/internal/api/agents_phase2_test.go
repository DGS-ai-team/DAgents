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
	if fsRoot != cfg.FSRoot {
		t.Fatalf("fsRoot=%q want shared node fs_root %q", fsRoot, cfg.FSRoot)
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
	wantFS := cfg.FSRoot
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
