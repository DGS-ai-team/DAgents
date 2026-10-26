package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestRunCheckOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/node/update":
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

func TestRunUnsupportedPlatformFailsCheckOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"current_version":"0.5.1","latest_version":"0.5.1","upgrade_available":false,"manage_reachable":true,"platform_supported":false,"platform":"windows-386","message":"当前平台没有可用的更新包"}`))
	}))
	defer srv.Close()

	cfg := &config.Config{Local: config.LocalConfig{Endpoint: srv.URL}}
	if got := Run(context.Background(), cfg, Options{CheckOnly: true}); got != 1 {
		t.Fatalf("Run unsupported platform = %d, want failure", got)
	}
}

func TestRunDownloadsBinaryPackageFromNodeWithoutManageCredential(t *testing.T) {
	payload := []byte("verified node package")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/node/update":
			_ = json.NewEncoder(w).Encode(nodeapi.AgentUpdateStatus{
				CurrentVersion:   "1.0.0",
				LatestVersion:    "1.1.0",
				UpgradeAvailable: true,
				ManageReachable:  true,
				Channel:          "stable",
				Platform:         "windows-amd64",
				Asset: map[string]any{
					"filename":   "package.zip",
					"sha256":     checksum,
					"size_bytes": float64(len(payload)),
				},
			})
		case "/v1/node/update/package":
			if got := r.Header.Get("Authorization"); got != "" {
				t.Errorf("Node package request forwarded authorization")
			}
			if got := r.Header.Get("x-dagents-a2a-token"); got != "" {
				t.Errorf("Node package request forwarded Manage credential")
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("X-Node-Package-Version", "1.1.0")
			w.Header().Set("X-Node-Package-SHA256", checksum)
			w.Header().Set("Content-Length", "21")
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	output := filepath.Join(t.TempDir(), "package.zip")
	cfg := &config.Config{
		Local:  config.LocalConfig{Endpoint: server.URL},
		Manage: config.ManageConfig{NodeToken: "manage-secret"},
	}
	if got := Run(context.Background(), cfg, Options{Output: output, Force: true}); got != 0 {
		t.Fatalf("Run download = %d", got)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("downloaded package = %q", got)
	}
}
