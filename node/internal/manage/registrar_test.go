package manage

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testManageConfig(serverURL, token string) *config.Config {
	cfg := &config.Config{
		NodeID: "ops-01",
		Agent: config.AgentConfig{
			Name: "展示名",
		},
		Local: config.LocalConfig{Endpoint: "http://127.0.0.1:18765"},
		Manage: config.ManageConfig{
			Enabled:   true,
			URL:       serverURL,
			NodeToken: token,
			Registration: config.ManageRegistrationConfig{
				BaseURL:         "http://10.0.0.5:18765",
				IntervalSeconds: 1,
				TTLSeconds:      60,
				Team:            "platform",
			},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}

func TestRegistrar_registerAndHeartbeat(t *testing.T) {
	var registerCalls atomic.Int32
	var heartbeatCalls atomic.Int32
	var gotToken string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get(tokenHeader)
		gotAgentID := r.Header.Get(agentIDHeader)
		if gotAgentID != "ops-01" {
			t.Fatalf("agent id header = %q, want ops-01", gotAgentID)
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/registry/agents":
			registerCalls.Add(1)
			var payload registerPayload
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("register body: %v", err)
			}
			if payload.AgentID != "ops-01" {
				t.Fatalf("unexpected register payload: %+v", payload)
			}
			if payload.BaseURL != "http://127.0.0.1:18765" {
				t.Fatalf("base_url = %q (want local.endpoint)", payload.BaseURL)
			}
			// host_ips 由本机网卡自动采集；测试环境可能为空，但字段应存在于 payload
			_ = payload.HostIPs
			_ = json.NewEncoder(w).Encode(map[string]any{
				"heartbeat_interval_seconds": 2,
				"agent":                      map[string]string{"status": "online"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/heartbeat"):
			heartbeatCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "online"})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/deregister"):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]bool{"deleted": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := testManageConfig(srv.URL, "node-secret")
	reg := NewRegistrar(cfg, nil)
	reg.SetToolNamesProvider(func() []string { return []string{"bash_run", "read_file"} })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg.Start(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if registerCalls.Load() >= 1 && heartbeatCalls.Load() >= 1 && reg.Registered() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if registerCalls.Load() < 1 {
		t.Fatal("expected at least one register call")
	}
	if heartbeatCalls.Load() < 1 {
		t.Fatal("expected at least one heartbeat call")
	}
	if !reg.Registered() {
		t.Fatal("expected registered=true")
	}
	if gotToken != "node-secret" {
		t.Fatalf("token = %q, want node-secret", gotToken)
	}

	reg.Stop(context.Background())
}

func TestRegistrar_reregistersOnHeartbeat404(t *testing.T) {
	var registerCalls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/registry/agents":
			registerCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"heartbeat_interval_seconds": 1,
				"agent":                      map[string]string{"status": "online"},
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/heartbeat"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/deregister"):
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := testManageConfig(srv.URL, "")
	reg := NewRegistrar(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reg.Start(ctx)

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if registerCalls.Load() >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if registerCalls.Load() < 2 {
		t.Fatalf("register calls = %d, want >= 2", registerCalls.Load())
	}
	cancel()
}

func TestRegistrar_buildRegisterPayload_usesAgentConfig(t *testing.T) {
	cfg := testManageConfig("http://127.0.0.1:8020", "")
	cfg.NodeID = "ops-01"
	cfg.Agent = config.AgentConfig{
		Name:         "展示名",
		Description:  "Card 描述",
		Capabilities: []string{"compliance_review"},
	}
	reg := NewRegistrar(cfg, nil)
	payload := reg.buildRegisterPayload()
	if payload.Name != "展示名" {
		t.Fatalf("name = %q, want 展示名", payload.Name)
	}
	if payload.Description != "Card 描述" {
		t.Fatalf("description = %q", payload.Description)
	}
	if len(payload.Capabilities) != 1 || payload.Capabilities[0] != "compliance_review" {
		t.Fatalf("capabilities = %v", payload.Capabilities)
	}
}

func TestRegistrar_buildRegisterPayload_hasCard(t *testing.T) {
	cfg := testManageConfig("http://127.0.0.1:8020", "")
	reg := NewRegistrar(cfg, nil)
	payload := reg.buildRegisterPayload()
	if payload.Card == nil {
		t.Fatal("expected registration card")
	}
}
