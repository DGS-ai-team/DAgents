package probe

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestNode_success(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok", "node_id": "a1", "version": "0.2.2",
		})
	})
	mux.HandleFunc("GET /v1/node/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id":      "a1",
			"capabilities": []string{"shell"}, "manage_registered": false,
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cfg := &config.Config{NodeID: "a1", Local: config.LocalConfig{Endpoint: ts.URL}}
	cfg.ApplyDefaults()

	res, err := Node(context.Background(), cfg, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	if res.NodeID != "a1" || res.Status != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if res.ProfilePending {
		t.Fatal("expected profile complete")
	}
}

func TestNode_profilePendingStillOK(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok", "node_id": "a1", "version": "0.9.5",
		})
	})
	mux.HandleFunc("GET /v1/node/info", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "node_profile_required",
				"message": "请先完成 Node 身份与 LLM 配置后再使用本机功能",
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cfg := &config.Config{NodeID: "a1", Local: config.LocalConfig{Endpoint: ts.URL}}
	cfg.ApplyDefaults()

	res, err := Node(context.Background(), cfg, ts.Client())
	if err != nil {
		t.Fatal(err)
	}
	if !res.ProfilePending || res.NodeID != "a1" || res.Status != "ok" {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestNode_agentIDMismatch(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "node_id": "other"})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cfg := &config.Config{NodeID: "expected", Local: config.LocalConfig{Endpoint: ts.URL}}
	cfg.ApplyDefaults()

	if _, err := Node(context.Background(), cfg, ts.Client()); err == nil {
		t.Fatal("expected mismatch error")
	}
}

func TestNode_rejectsRemoteEndpointBeforeRequest(t *testing.T) {
	cfg := &config.Config{Local: config.LocalConfig{Endpoint: "http://192.0.2.10:18765"}}
	if _, err := Node(context.Background(), cfg, nil); err == nil {
		t.Fatal("expected remote endpoint to be rejected")
	}
}

func TestNode_normalizesDerivedWildcardListenEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "node_id": "a1", "version": "1"})
	})
	mux.HandleFunc("GET /v1/node/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"node_id": "a1", "capabilities": []string{}})
	})
	for _, host := range []string{"0.0.0.0", "::"} {
		t.Run(host, func(t *testing.T) {
			bindHost := "127.0.0.1"
			wantNodeHost := "127.0.0.1"
			if host == "::" {
				bindHost = "::1"
				wantNodeHost = "[::1]"
			}
			listener, err := net.Listen("tcp", net.JoinHostPort(bindHost, "0"))
			if err != nil {
				t.Skipf("cannot bind %s loopback: %v", bindHost, err)
			}
			ts := httptest.NewUnstartedServer(mux)
			ts.Listener = listener
			ts.Start()
			t.Cleanup(ts.Close)
			u, err := url.Parse(ts.URL)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.Atoi(u.Port())
			if err != nil {
				t.Fatal(err)
			}
			cfg := &config.Config{NodeID: "a1", Listen: config.ListenConfig{Host: host, Port: port}}
			cfg.ApplyDefaults()
			res, err := Node(context.Background(), cfg, ts.Client())
			if err != nil {
				t.Fatalf("Node with derived wildcard endpoint: %v (local.endpoint %q)", err, cfg.Local.Endpoint)
			}
			if res.Endpoint != "http://"+wantNodeHost+":"+u.Port() {
				t.Fatalf("probe endpoint = %q, want loopback target", res.Endpoint)
			}
		})
	}
}
