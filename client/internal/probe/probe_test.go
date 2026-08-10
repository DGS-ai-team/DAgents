package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	mux.HandleFunc("GET /v1/agent/info", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"node_id": "a1",
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
			"status": "ok", "node_id": "a1", "version": "0.9.3",
		})
	})
	mux.HandleFunc("GET /v1/agent/info", func(w http.ResponseWriter, _ *http.Request) {
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
