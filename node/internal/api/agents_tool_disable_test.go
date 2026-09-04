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
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestPatchAgent_toolDisableSoftRejectAndNotice(t *testing.T) {
	root, err := os.MkdirTemp("", "dagents-tool-disable-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })

	cfg := &config.Config{NodeID: "node-test", RuntimeRoot: filepath.Join(root, "runtime")}
	cfg.ApplyDefaults()
	cfg.Onboarding.NodeProfileCompleted = true
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
		t.Fatal("registry missing")
	}
	out, err := reg.Execute(context.Background(), "bash_run", `{"command":"echo ok","timeout_seconds":10}`)
	if err != nil {
		t.Fatalf("bash enabled execute: %v", err)
	}
	if !strings.Contains(out, "ok") && !strings.Contains(strings.ToLower(out), "exit") {
		t.Fatalf("unexpected bash out: %q", out)
	}

	events := srv.stream.SubscribeAgent(srv.stream.CurrentSeq(), created.AgentID)
	t.Cleanup(func() { srv.stream.Unsubscribe(events) })

	patchBody, _ := json.Marshal(map[string]any{
		"defaults": map[string]any{
			"tools": map[string]any{"enabled_groups": []string{"fs"}},
		},
	})
	req = httptest.NewRequest(http.MethodPatch, "/v1/agents/"+created.AgentID, bytes.NewReader(patchBody))
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rr.Code, rr.Body.String())
	}

	noticeDeadline := time.Now().Add(2 * time.Second)
	gotNotice := false
	for time.Now().Before(noticeDeadline) && !gotNotice {
		select {
		case ev := <-events:
			if ev.Type == "system_notice" {
				msg, _ := ev.Data["message"].(string)
				if strings.Contains(msg, "工具集已变更") {
					gotNotice = true
				}
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !gotNotice {
		t.Fatal("expected system_notice after toolset shrink")
	}
	if session.ToolsetChangedNotice == "" {
		t.Fatal("notice constant empty")
	}

	reg = srv.sessions.SessionTools(created.AgentID)
	if reg == nil {
		t.Fatal("registry missing after reload")
	}
	_, err = reg.Execute(context.Background(), "bash_run", `{"command":"echo should-fail","timeout_seconds":10}`)
	if err == nil {
		t.Fatal("expected bash_run soft reject after disable")
	}
	if !strings.Contains(err.Error(), "is not enabled") {
		t.Fatalf("want not-enabled error, got %v", err)
	}
	if _, err := reg.Execute(context.Background(), "write_file", `{"path":"a.txt","content":"x"}`); err != nil {
		t.Fatalf("write_file should remain enabled: %v", err)
	}
}
