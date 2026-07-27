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
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// 复现并回归：per-agent Registry 必须挂 BackgroundJobNotifier，否则 status=succeeded 但无 async 回灌。
func TestPerAgentBackgroundJobNotifiesAsyncCallback(t *testing.T) {
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
sandbox:
  enabled: true
  backend: process
  workspace_subdir: data
  fs_root_isolation: true
  allow_bash: true
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

	ctx := tools.WithToolCallID(tools.WithSession(context.Background(), created.AgentID), "call-per-agent-bg")
	done := make(chan string, 1)
	go func() {
		out, execErr := reg.Execute(ctx, "bash_run", `{"command":"sleep 1","timeout_seconds":30}`)
		if execErr != nil {
			done <- "ERR:" + execErr.Error()
			return
		}
		done <- out
	}()

	bgDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(bgDeadline) {
		if err := reg.BackgroundSyncBash(created.AgentID, "call-per-agent-bg"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case out := <-done:
		if strings.HasPrefix(out, "ERR:") || !strings.Contains(out, "status=RUNNING") {
			t.Fatalf("expected RUNNING degrade, got %q", out)
		}
		jobID := extractBashJobID(out)
		if jobID == "" {
			t.Fatalf("missing job_id in %q", out)
		}

		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			statusOut, _ := reg.Execute(context.Background(), "background_job_status", `{"job_id":"`+jobID+`"}`)
			if !strings.Contains(statusOut, "status=succeeded") {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			view, err := srv.sessions.GetHydrateView(created.AgentID)
			if err != nil {
				t.Fatal(err)
			}
			if hydrateContainsJobID(view, jobID) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}

		statusOut, _ := reg.Execute(context.Background(), "background_job_status", `{"job_id":"`+jobID+`"}`)
		view, _ := srv.sessions.GetHydrateView(created.AgentID)
		t.Fatalf("job status=%q but async callback not applied; hydrate=%+v", statusOut, view)
	case <-time.After(10 * time.Second):
		t.Fatal("bash did not return after background")
	}
}

func extractBashJobID(text string) string {
	idx := strings.Index(text, "job_id=")
	if idx < 0 {
		return ""
	}
	rest := text[idx+len("job_id="):]
	end := strings.IndexAny(rest, " \n")
	if end < 0 {
		return strings.TrimSpace(rest)
	}
	return rest[:end]
}

func hydrateContainsJobID(view *session.HydrateView, jobID string) bool {
	if view == nil {
		return false
	}
	for _, e := range view.Transcript {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), jobID) {
			return true
		}
	}
	return false
}
