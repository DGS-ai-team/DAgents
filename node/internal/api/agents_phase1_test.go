package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/sandbox"
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
			"backend": "process",
			"allow_bash": true,
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
	if !created.SandboxEnabled || created.SandboxBackend != "process" {
		t.Fatalf("sandbox = enabled=%v backend=%q", created.SandboxEnabled, created.SandboxBackend)
	}
	ws := filepath.Join(cfg.AgentsDir(), created.AgentID, "data")
	if st, err := os.Stat(ws); err != nil || !st.IsDir() {
		t.Fatalf("workspace missing: %v", err)
	}
}

func TestCreateAgent_dockerRequiresCLI(t *testing.T) {
	restore := sandbox.SetLookPathForTest(func(string) (string, error) { return "", os.ErrNotExist })
	t.Cleanup(restore)

	cfg := &config.Config{NodeID: "node-test", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "ops.yaml"), []byte("id: ops\ndisplay_name: Ops\nsandbox:\n  enabled: false\n"), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"template_id": "ops",
		"sandbox": map[string]any{
			"enabled": true,
			"backend": "docker",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("docker_unavailable")) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCreateAgent_remoteReservedNotImplemented(t *testing.T) {
	cfg := &config.Config{NodeID: "node-test", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"display_name": "远程沙箱助手",
		"sandbox": map[string]any{
			"enabled":         true,
			"backend":         "remote",
			"remote_endpoint": "https://sbx.example.com",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !bytes.Contains(rr.Body.Bytes(), []byte("remote_unavailable")) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestCreateAgent_dockerOKWhenCLIPresent(t *testing.T) {
	restorePath := sandbox.SetLookPathForTest(func(string) (string, error) { return "/usr/bin/docker", nil })
	t.Cleanup(restorePath)
	restoreRun := sandbox.SetRunDockerForTest(func(_ context.Context, _ string, args ...string) (string, string, error) {
		if len(args) > 0 && args[0] == "inspect" {
			return "true", "", nil
		}
		return "", "", nil
	})
	t.Cleanup(restoreRun)

	cfg := &config.Config{NodeID: "node-test", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()
	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "ops.yaml"), []byte("id: ops\ndisplay_name: Ops\nsandbox:\n  enabled: false\n"), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"template_id": "ops",
		"sandbox": map[string]any{
			"enabled": true,
			"backend": "docker",
			"image":   "alpine:3.20",
			"memory":  "256m",
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
	snap, err := agentruntime.ParseSnapshot(created.ConfigSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Sandbox.FSRootIsolation {
		t.Fatal("docker should force fs_root_isolation")
	}
	if snap.Sandbox.Image != "alpine:3.20" || snap.Sandbox.Network != "none" {
		t.Fatalf("sandbox fields=%+v", snap.Sandbox)
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

func TestCreateAgent_fullSettingsWithoutTemplateMerge(t *testing.T) {
	cfg := &config.Config{NodeID: "node-test", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"display_name": "完整助手",
		"template_id":  "general", // 仅溯源
		"defaults": map[string]any{
			"llm":   map[string]any{"active": "default", "max_tool_loops": 8},
			"tools": map[string]any{"enabled_groups": []any{"fs"}},
			"prompt_context": map[string]any{
				"long_term_enabled": false,
			},
		},
		"sandbox": map[string]any{
			"enabled":             true,
			"backend":             "process",
			"fs_root_isolation":   true,
			"allow_bash":          false,
			"allow_network_tools": false,
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
	if created.TemplateID != "general" {
		t.Fatalf("template_id = %q", created.TemplateID)
	}
	var snap map[string]any
	if err := json.Unmarshal(created.ConfigSnapshot, &snap); err != nil {
		t.Fatal(err)
	}
	defaults, _ := snap["defaults"].(map[string]any)
	tools, _ := defaults["tools"].(map[string]any)
	groups, _ := tools["enabled_groups"].([]any)
	if len(groups) != 1 || groups[0] != "fs" {
		t.Fatalf("tools = %#v", tools)
	}
	pc, _ := defaults["prompt_context"].(map[string]any)
	if pc["long_term_enabled"] != false {
		t.Fatalf("prompt_context = %#v", pc)
	}
	sandbox, _ := snap["sandbox"].(map[string]any)
	if sandbox["allow_bash"] != false || sandbox["fs_root_isolation"] != true {
		t.Fatalf("sandbox = %#v", sandbox)
	}
}
