package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/a2aclient"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestAgentInvokeUsesCompliancePeerDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/a2a/tasks":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["to_agent_id"] != "node-a" {
				t.Fatalf("to_agent_id=%q", body["to_agent_id"])
			}
			if body["caller_session_id"] != "sess-x" {
				t.Fatalf("caller_session_id=%q", body["caller_session_id"])
			}
			_ = json.NewEncoder(w).Encode(a2aclient.CreateResponse{TaskID: "t1", Status: "queued"})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/a2a/tasks/t1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{"task_id": "t1", "status": "completed", "result_text": "ok"},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	reg.SetManageRuntime(a2aclient.New(cfg), "node-b", "node-a", nil)

	ctx := WithSession(context.Background(), "sess-x")
	out, err := reg.Execute(ctx, "agent_invoke", `{"content":"【合规咨询】测试","call_purpose":"合规咨询"}`)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("out=%q", out)
	}
}

func TestAgentInvokeRequiresManageRuntime(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Execute(context.Background(), "agent_invoke", `{"content":"x","call_purpose":"test"}`)
	if err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %v", err)
	}
}

func TestAgentInvokeInDefinitionsWhenConfigured(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if hasToolName(reg.Definitions(), "agent_invoke") {
		t.Fatal("expected agent_invoke absent before SetManageRuntime")
	}
	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: "http://127.0.0.1:1"}}
	reg.SetManageRuntime(a2aclient.New(cfg), "node-b", "node-a", nil)
	if !hasToolName(reg.Definitions(), "agent_invoke") {
		t.Fatal("expected agent_invoke present after SetManageRuntime")
	}
	if !hasToolName(reg.Definitions(), "agent_discover") {
		t.Fatal("expected agent_discover present after SetManageRuntime")
	}
}

func TestAgentDiscoverUsesCallerGroupsFromManage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/registry/agents/discover" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("discovery_group"); got != "" {
			t.Fatalf("discovery_group=%q, want empty when tool omits param", got)
		}
		if got := r.Header.Get("x-dagents-agent-id"); got != "node-b" {
			t.Fatalf("agent header=%q", got)
		}
		_ = json.NewEncoder(w).Encode(a2aclient.DiscoverResponse{
			Agents: []a2aclient.DiscoverAgent{{AgentID: "node-a", Name: "合规助手"}},
		})
	}))
	defer srv.Close()

	reg, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AgentID: "node-b", Manage: config.ManageConfig{URL: srv.URL}}
	reg.SetManageRuntime(a2aclient.New(cfg), "node-b", "", nil)

	out, err := reg.Execute(context.Background(), "agent_discover", `{"call_purpose":"发现 peer"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "node-a") {
		t.Fatalf("out=%q", out)
	}
}

func hasToolName(defs []ToolDef, name string) bool {
	for _, d := range defs {
		if d.Function.Name == name {
			return true
		}
	}
	return false
}
