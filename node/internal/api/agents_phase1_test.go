package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestCreateAgent_sandboxOverrideAndWorkspace(t *testing.T) {
	cfg := &config.Config{NodeID: "node-test", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()

	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "code-reviewer.yaml"), []byte(`
id: code-reviewer
display_name: 审查
sandbox:
  enabled: true
  backend: process
  allow_bash: false
`), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"template_id":  "code-reviewer",
		"display_name": "审查A",
		"sandbox": map[string]any{
			"enabled": true,
			"backend": "docker",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var created agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if !created.SandboxEnabled || created.SandboxBackend != "docker" {
		t.Fatalf("sandbox = enabled=%v backend=%q", created.SandboxEnabled, created.SandboxBackend)
	}
	ws := filepath.Join(cfg.AgentsDir(), created.AgentID, "data")
	if st, err := os.Stat(ws); err != nil || !st.IsDir() {
		t.Fatalf("workspace missing: %v", err)
	}
}

func TestCreateAgent_rejectsInvalidBackend(t *testing.T) {
	cfg := &config.Config{NodeID: "node-test", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "general.yaml"), []byte("id: general\ndisplay_name: G\nsandbox:\n  enabled: false\n"), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"template_id": "general",
		"sandbox":     map[string]any{"backend": "firecracker"},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400 body=%s", rr.Code, rr.Body.String())
	}
}
