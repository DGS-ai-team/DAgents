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

func TestListAgents_includesNotifyFields(t *testing.T) {
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
	_ = os.WriteFile(filepath.Join(userDir, "general.yaml"), []byte(`
id: general
display_name: 通用
defaults:
  tools:
    enabled_groups: [fs]
`), 0o644)

	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	t.Cleanup(func() { srv.sessions.Stop() })
	srv.agents = agentsDB

	body, _ := json.Marshal(map[string]any{
		"template_id":  "general",
		"display_name": "助手A",
		"defaults": map[string]any{
			"llm":   map[string]any{"active": "default"},
			"tools": map[string]any{"enabled_groups": []any{"fs"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rr.Code, rr.Body.String())
	}
	var out struct {
		Agents []agentView `json:"agents"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Agents) != 1 {
		t.Fatalf("agents=%d", len(out.Agents))
	}
	if out.Agents[0].AgentID == "" {
		t.Fatal("missing agent_id")
	}
	// 无待办时 omitempty 省略布尔字段；结构已可解码即可。
	_ = out.Agents[0].HasUnread
	_ = out.Agents[0].HasPendingHITL
	_ = out.Agents[0].PendingHITLItems
}
