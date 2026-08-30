package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
	sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"
)

func TestCheckerFetchCheck(t *testing.T) {
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
				"download_url": "/v1/releases/packages/dagents-local-assistant/stable/windows-amd64/latest/download",
			},
		})
	}))
	defer srv.Close()

	cfg := &config.Config{NodeID: "shell-1"}
	cfg.Manage.Enabled = true
	cfg.Manage.URL = srv.URL
	checker := NewChecker(cfg, t.TempDir(), nil)
	status := checker.fetchCheck()
	if !status.ManageReachable {
		t.Fatalf("expected reachable")
	}
	if !status.UpgradeAvailable || status.LatestVersion != "0.9.0" {
		t.Fatalf("status=%+v", status)
	}
	if asset := status.Asset; asset == nil || asset["download_url"] != srv.URL+"/v1/releases/packages/dagents-local-assistant/stable/windows-amd64/latest/download" {
		t.Fatalf("asset=%v", status.Asset)
	}
}

func TestReadInstallVersion(t *testing.T) {
	dir := t.TempDir()
	if got := ReadInstallVersion(dir); got != "dev" {
		t.Fatalf("missing VERSION: got %q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "VERSION"), []byte("0.6.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ReadInstallVersion(dir); got != "0.6.2" {
		t.Fatalf("got %q", got)
	}
}

func TestIsWindowsInstallerAsset(t *testing.T) {
	tests := []struct {
		name   string
		status sharedupdate.Status
		want   bool
	}{
		{
			name: "x64 installer",
			status: sharedupdate.Status{
				Platform: "windows-amd64",
				Asset: map[string]any{
					"filename": "dagents-local-assistant-windows-amd64-installer-0.10.5.exe",
				},
			},
			want: true,
		},
		{
			name: "legacy archive",
			status: sharedupdate.Status{
				Platform: "windows-386",
				Asset: map[string]any{
					"filename": "dagents-local-assistant-windows-386-0.10.4.zip",
				},
			},
			want: false,
		},
		{
			name: "linux executable name",
			status: sharedupdate.Status{
				Platform: "linux-amd64",
				Asset:    map[string]any{"filename": "tool.exe"},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isWindowsInstallerAsset(tt.status); got != tt.want {
				t.Fatalf("isWindowsInstallerAsset() = %v, want %v", got, tt.want)
			}
		})
	}
}
