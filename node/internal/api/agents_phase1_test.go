package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestCreateAgent_createsWorkspace(t *testing.T) {
	cfg := &config.Config{NodeID: "node-test", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
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
defaults:
  tools:
    enabled_groups: [fs]
`), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	t.Cleanup(func() { srv.sessions.Stop() })
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"template_id":  "code-reviewer",
		"display_name": "审查A",
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
	if created.AgentID == "" || created.DisplayName != "审查A" {
		t.Fatalf("created = %+v", created)
	}
	workspaceRoot, err := agentruntime.EffectiveWorkspaceRoot(cfg.RuntimeDir(), created.AgentID, agentruntime.WorkspaceConfig{Mode: agentruntime.WorkspaceModePrivate})
	if err != nil {
		t.Fatal(err)
	}
	stateRoot, err := agentruntime.WorkspaceStateRoot(workspaceRoot, created.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	for _, subdir := range []string{"history", "memory"} {
		if st, err := os.Stat(filepath.Join(stateRoot, subdir)); err != nil || !st.IsDir() {
			t.Fatalf("workspace state %q missing: %v", subdir, err)
		}
	}
}

func TestCreateAgent_fullSettingsWithoutTemplateMerge(t *testing.T) {
	cfg := &config.Config{NodeID: "node-test", RuntimeRoot: t.TempDir()}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	t.Cleanup(func() { srv.sessions.Stop() })
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"display_name": "完整助手",
		"template_id":  "general", // 仅溯源
		"defaults": map[string]any{
			"llm":   map[string]any{"active": "default", "max_steps": 8},
			"tools": map[string]any{"enabled_groups": []any{"fs"}},
			"prompt_context": map[string]any{
				"memory_enabled": false,
			},
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
	if pc["memory_enabled"] != false {
		t.Fatalf("prompt_context = %#v", pc)
	}
	if _, hasSandbox := snap["sandbox"]; hasSandbox {
		t.Fatalf("unexpected sandbox in snapshot: %#v", snap["sandbox"])
	}
}
