package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckURL(t *testing.T) {
	u, err := CheckURL("https://manage.example/", "0.6.1", "windows-amd64", "stable")
	if err != nil {
		t.Fatal(err)
	}
	if u != "https://manage.example/v1/releases/check?channel=stable&current=0.6.1&platform=windows-amd64" {
		t.Fatalf("url=%q", u)
	}
}

func TestNormalizeAssetURLs(t *testing.T) {
	asset := map[string]any{
		"download_url": "/v1/releases/packages/latest/download",
	}
	out := NormalizeAssetURLs("https://manage.example", asset)
	if out["download_url"] != "https://manage.example/v1/releases/packages/latest/download" {
		t.Fatalf("asset=%v", out)
	}
}

func TestCheck(t *testing.T) {
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

	status := Check(CheckRequest{
		ManageURL:      srv.URL,
		CurrentVersion: "0.6.0",
		Platform:       "linux-amd64",
		Channel:        "stable",
		AgentID:        "node-1",
	})
	if !status.ManageReachable {
		t.Fatalf("expected reachable")
	}
	if !status.PlatformSupported {
		t.Fatalf("legacy Manage response should default platform_supported to true: %+v", status)
	}
	if !status.UpgradeAvailable || status.LatestVersion != "0.9.0" {
		t.Fatalf("status=%+v", status)
	}
	if asset := status.Asset; asset == nil || asset["download_url"] != srv.URL+"/v1/releases/packages/dagents-local-assistant/stable/linux-amd64/latest/download" {
		t.Fatalf("asset=%v", status.Asset)
	}
}

func TestCheckReportsUnsupportedPlatformWithoutAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"latest":             "0.11.0",
			"upgrade_available":  false,
			"platform_supported": false,
			"asset":              nil,
		})
	}))
	defer srv.Close()

	status := Check(CheckRequest{
		ManageURL:      srv.URL,
		CurrentVersion: "0.11.0",
		Platform:       "windows-386",
	})
	if !status.ManageReachable || status.PlatformSupported || status.UpgradeAvailable || status.Asset != nil {
		t.Fatalf("status=%+v", status)
	}
	if status.Message != "当前平台没有可用的更新包" {
		t.Fatalf("message=%q", status.Message)
	}
}

func TestCheckRecordsAttemptWhenManageUnavailable(t *testing.T) {
	status := Check(CheckRequest{ManageURL: "", CurrentVersion: "0.9.0"})
	if status.ManageReachable {
		t.Fatal("expected Manage to be unreachable")
	}
	if status.LastCheckedAt == "" {
		t.Fatal("expected failed check to record an attempt timestamp")
	}
}

func TestStatusPlatformSupportedCompatibility(t *testing.T) {
	var legacy Status
	if err := json.Unmarshal([]byte(`{"current_version":"0.7.0"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if !legacy.PlatformSupported {
		t.Fatal("legacy status without platform_supported should be treated as supported")
	}

	var unsupported Status
	if err := json.Unmarshal([]byte(`{"current_version":"0.7.0","platform_supported":false}`), &unsupported); err != nil {
		t.Fatal(err)
	}
	if unsupported.PlatformSupported {
		t.Fatal("explicit platform_supported=false must be preserved")
	}
}
