package manage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestUpdateCheckerFetchCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/releases/check" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"latest":            "0.9.0",
			"upgrade_available": true,
			"release_notes":     "test",
			"asset": map[string]any{
				"download_url": "/v1/releases/packages/dagents-local-assistant/stable/linux-amd64/latest/download",
			},
		})
	}))
	defer srv.Close()

	cfg := &config.Config{NodeID: "node-1"}
	cfg.Manage.Enabled = true
	cfg.Manage.URL = srv.URL
	checker := NewUpdateChecker(cfg, nil)
	status := checker.fetchCheck()
	if !status.ManageReachable {
		t.Fatalf("expected reachable")
	}
	if !status.UpgradeAvailable || status.LatestVersion != "0.9.0" {
		t.Fatalf("status=%+v", status)
	}
	if asset := status.Asset; asset == nil || asset["download_url"] != srv.URL+"/v1/releases/packages/dagents-local-assistant/stable/linux-amd64/latest/download" {
		t.Fatalf("asset=%v", status.Asset)
	}
}

func TestUpdateCheckerDoesNotForwardNodeTokenAcrossRedirect(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetCalls.Add(1)
		if r.Header.Get("x-dagents-a2a-token") != "" {
			t.Error("Node credential reached redirect target")
		}
	}))
	defer target.Close()
	manageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/releases/check" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("x-dagents-a2a-token") == "" {
			t.Error("Node credential missing from configured Manage request")
		}
		http.Redirect(w, r, target.URL+"/collect", http.StatusFound)
	}))
	defer manageServer.Close()

	cfg := &config.Config{NodeID: "node-redirect-test"}
	cfg.Manage.Enabled = true
	cfg.Manage.NodeToken = "credential-value"
	cfg.Manage.URL = manageServer.URL
	status := NewUpdateChecker(cfg, nil).CheckNow()
	if status.ManageReachable {
		t.Fatal("redirect response must not be treated as a successful release check")
	}
	if got := targetCalls.Load(); got != 0 {
		t.Fatalf("redirect target called %d times", got)
	}
}
