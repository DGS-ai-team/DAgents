package manage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestInboxPoller_pollOnce(t *testing.T) {
	var gotAgentID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/a2a/inbox" {
			http.NotFound(w, r)
			return
		}
		gotAgentID = r.URL.Query().Get("agent_id")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{
					"task_id":         "a2a-task-abc",
					"from_agent_id":   "caller",
					"kind":            "invoke",
					"content":         "hi",
					"created_at_unix": 1,
					"expires_at_unix": 999,
				},
			},
			"pending_count": 0,
		})
	}))
	defer srv.Close()

	cfg := &config.Config{
		AgentID: "callee-01",
		Manage: config.ManageConfig{
			Enabled: true,
			URL:     srv.URL,
			A2A: config.ManageA2AConfig{
				InboxWaitSeconds: 25,
			},
		},
	}
	var handled InboxTask
	poller := NewInboxPoller(cfg, nil)
	poller.SetHandler(func(ctx context.Context, task InboxTask) error {
		handled = task
		return nil
	})
	if err := poller.pollOnce(context.Background(), 0); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if gotAgentID != "callee-01" {
		t.Fatalf("agent_id=%q", gotAgentID)
	}
	if handled.TaskID != "a2a-task-abc" || handled.Content != "hi" {
		t.Fatalf("handled=%+v", handled)
	}
}

func TestConfigManageA2AInboxWait(t *testing.T) {
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	if cfg.ManageA2AInboxWait() != 25*time.Second {
		t.Fatalf("wait=%s", cfg.ManageA2AInboxWait())
	}
}

func TestConfigManageA2AEnabled(t *testing.T) {
	disabled := false
	cfg := &config.Config{
		Manage: config.ManageConfig{
			Enabled: true,
			A2A: config.ManageA2AConfig{
				Enabled: &disabled,
			},
		},
	}
	if cfg.ManageA2AEnabled() {
		t.Fatal("expected a2a disabled when explicitly false")
	}
	cfg2 := &config.Config{
		Manage: config.ManageConfig{Enabled: true},
	}
	if !cfg2.ManageA2AEnabled() {
		t.Fatal("expected a2a enabled by default when manage enabled")
	}
	cfg3 := &config.Config{
		Manage: config.ManageConfig{Enabled: false},
	}
	if cfg3.ManageA2AEnabled() {
		t.Fatal("expected a2a disabled when manage disabled")
	}
}

func TestConfigManageA2AInboxPollInterval(t *testing.T) {
	cfg := &config.Config{
		Manage: config.ManageConfig{
			Registration: config.ManageRegistrationConfig{IntervalSeconds: 45},
		},
	}
	cfg.ApplyDefaults()
	if cfg.ManageA2AInboxPollInterval() != 45*time.Second {
		t.Fatalf("poll=%s", cfg.ManageA2AInboxPollInterval())
	}
}

func TestInboxPoller_inboxURLIncludesWait(t *testing.T) {
	cfg := &config.Config{
		AgentID: "node-1",
		Manage: config.ManageConfig{
			URL: "http://manage.local:8020",
			A2A: config.ManageA2AConfig{InboxWaitSeconds: 25},
		},
	}
	p := NewInboxPoller(cfg, nil)
	u, err := p.inboxURL(25 * time.Second)
	if err != nil {
		t.Fatalf("inboxURL: %v", err)
	}
	if !strings.Contains(u, "agent_id=node-1") || !strings.Contains(u, "wait=25") {
		t.Fatalf("url=%q", u)
	}
}

func TestInboxPoller_pollOnce_http_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := &config.Config{
		AgentID: "callee",
		Manage:  config.ManageConfig{URL: srv.URL},
	}
	p := NewInboxPoller(cfg, nil)
	err := p.pollOnce(context.Background(), 0)
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestInboxPoller_backoffInterval(t *testing.T) {
	p := NewInboxPoller(&config.Config{}, nil)
	if p.backoffInterval() != time.Second {
		t.Fatalf("initial backoff=%s", p.backoffInterval())
	}
	p.failures = 3
	cfg := &config.Config{}
	cfg.ApplyDefaults()
	p.cfg = cfg
	if p.backoffInterval() != 30*time.Second {
		t.Fatalf("degraded backoff=%s", p.backoffInterval())
	}
}
