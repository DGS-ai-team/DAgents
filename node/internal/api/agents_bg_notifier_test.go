package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// bash_run is deliberately synchronous: a timeout is a terminal tool result,
// not an implicit background job which later injects an async callback.
func TestPerAgentBashTimeoutDoesNotCreateAsyncCallback(t *testing.T) {
	root, err := os.MkdirTemp("", "dagents-per-agent-bg-notify-*")
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
	_ = os.WriteFile(filepath.Join(userDir, "bash-agent.yaml"), []byte(`
id: bash-agent
display_name: Bash
defaults:
  tools:
    enabled_groups: [fs, bash]
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
		"template_id":  "bash-agent",
		"display_name": "Bash Agent",
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

	reg := srv.sessions.SessionTools(created.AgentID)
	if reg == nil {
		t.Fatal("per-agent registry missing")
	}
	if defaultReg := srv.sessions.DefaultTools(); defaultReg != nil && reg == defaultReg {
		t.Fatal("expected distinct per-agent registry")
	}

	ctx := tools.WithToolCallID(tools.WithSession(context.Background(), created.AgentID), "call-per-agent-timeout")
	out, err := reg.Execute(ctx, "bash_run", `{"command":"sleep 2","timeout_seconds":1}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "status=TIMED_OUT") || strings.Contains(out, "job_id=") {
		t.Fatalf("expected synchronous timeout without job, got %q", out)
	}
	counts := reg.SessionToolJobCounts(created.AgentID)
	if counts.Running != 0 || counts.Background != 0 {
		t.Fatalf("timed out bash left jobs behind: %+v", counts)
	}
}
