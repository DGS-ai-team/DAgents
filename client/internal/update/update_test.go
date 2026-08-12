package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestRunCheckOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/agent/update":
			_ = json.NewEncoder(w).Encode(nodeapi.AgentUpdateStatus{
				CurrentVersion:   "0.5.0",
				LatestVersion:    "0.5.1",
				UpgradeAvailable: true,
				ManageReachable:  true,
				Channel:          "stable",
				Platform:         "linux-amd64",
				Message:          "新版本 0.5.1 可用",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := &config.Config{
		Local: config.LocalConfig{Endpoint: srv.URL},
	}
	if got := Run(context.Background(), cfg, Options{CheckOnly: true}); got != 0 {
		t.Fatalf("Run check-only = %d, want 0", got)
	}
}

func TestRunUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(nodeapi.AgentUpdateStatus{
			CurrentVersion:   "0.5.1",
			LatestVersion:    "0.5.1",
			UpgradeAvailable: false,
			ManageReachable:  true,
		})
	}))
	defer srv.Close()

	cfg := &config.Config{
		Local: config.LocalConfig{Endpoint: srv.URL},
	}
	if got := Run(context.Background(), cfg, Options{Output: t.TempDir() + "/pkg.tar.gz"}); got != ExitUpToDate {
		t.Fatalf("Run up-to-date = %d, want %d", got, ExitUpToDate)
	}
}
