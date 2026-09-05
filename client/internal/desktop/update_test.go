package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestGetUpdateStatusUsesShell(t *testing.T) {
	shell := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"current_version":   "0.7.0",
			"latest_version":    "0.7.1",
			"upgrade_available": true,
			"manage_reachable":  true,
			"channel":           "stable",
			"platform":          "windows-amd64",
			"message":           "新版本 0.7.1 可用",
			"apply_command":     "dagents update",
		})
	}))
	defer shell.Close()

	oldBase := desktopAPIBaseURL
	desktopAPIBaseURL = shell.URL
	defer func() { desktopAPIBaseURL = oldBase }()

	status, err := GetUpdateStatus(context.Background(), &http.Client{})
	if err != nil {
		t.Fatal(err)
	}
	if status.LatestVersion != "0.7.1" {
		t.Fatalf("latest = %q", status.LatestVersion)
	}
	if !status.UpgradeAvailable {
		t.Fatal("expected upgrade available from shell")
	}
}

func TestResolveAgentUpdateUsesNodeOnLinuxPath(t *testing.T) {
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(nodeapi.AgentUpdateStatus{
			CurrentVersion:   "0.7.0",
			LatestVersion:    "0.7.0",
			ManageReachable:  true,
			UpgradeAvailable: false,
		})
	}))
	defer node.Close()

	client := nodeapi.New(node.URL, &http.Client{})
	status, err := ResolveAgentUpdate(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if status.LatestVersion != "0.7.0" {
		t.Fatalf("latest = %q", status.LatestVersion)
	}
}
