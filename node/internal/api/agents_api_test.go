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

func TestAgentsAPI_CRUD(t *testing.T) {
	cfg := &config.Config{
		NodeID: "node-test",
		FSRoot: t.TempDir(),
	}
	cfg.ApplyDefaults()

	agentsDB, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer agentsDB.Close()

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	srv.agents = agentsDB

	// 用户模板目录放入 general，避免依赖 cwd 下 packaging 路径。
	userDir := cfg.AgentTemplatesDir()
	if err := os.MkdirAll(userDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join("..", "..", "..", "packaging", "agent-templates", "general.yaml")
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "general.yaml"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"template_id":  "general",
		"display_name": "我的助手",
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
	if created.AgentID == "" || created.DisplayName != "我的助手" || created.TemplateID != "general" {
		t.Fatalf("created = %+v", created)
	}

	body, _ = json.Marshal(map[string]any{
		"template_id":  "general",
		"display_name": "自定义工具",
		"defaults": map[string]any{
			"tools": map[string]any{
				"enabled_groups": []string{"fs", "bash"},
			},
			"hooks": map[string]any{
				"inject_today_date_enabled": false,
			},
		},
	})
	req = httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create with defaults status=%d body=%s", rr.Code, rr.Body.String())
	}
	var custom agentView
	if err := json.Unmarshal(rr.Body.Bytes(), &custom); err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal(custom.ConfigSnapshot, &snap); err != nil {
		t.Fatal(err)
	}
	def, _ := snap["defaults"].(map[string]any)
	tools, _ := def["tools"].(map[string]any)
	groups, _ := tools["enabled_groups"].([]any)
	if len(groups) != 2 {
		t.Fatalf("enabled_groups = %#v", tools["enabled_groups"])
	}
	hooks, _ := def["hooks"].(map[string]any)
	if hooks["inject_today_date_enabled"] != false {
		t.Fatalf("hooks = %#v", hooks)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rr.Code, rr.Body.String())
	}

	patch, _ := json.Marshal(map[string]any{"display_name": "新名字"})
	req = httptest.NewRequest(http.MethodPatch, "/v1/agents/"+created.AgentID, bytes.NewReader(patch))
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rr.Code, rr.Body.String())
	}
	var patched agentView
	_ = json.Unmarshal(rr.Body.Bytes(), &patched)
	if patched.DisplayName != "新名字" {
		t.Fatalf("patched = %+v", patched)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/agents/"+created.AgentID, nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAgentTemplatesAPI_list(t *testing.T) {
	cfg := &config.Config{NodeID: "node-test", FSRoot: t.TempDir()}
	cfg.ApplyDefaults()
	userDir := cfg.AgentTemplatesDir()
	_ = os.MkdirAll(userDir, 0o755)
	_ = os.WriteFile(filepath.Join(userDir, "demo.yaml"), []byte("id: demo\ndisplay_name: Demo\nsandbox:\n  enabled: false\n"), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	req := httptest.NewRequest(http.MethodGet, "/v1/agent-templates", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Templates []map[string]any `json:"templates"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Templates) < 1 {
		t.Fatalf("templates = %+v", out.Templates)
	}
}
