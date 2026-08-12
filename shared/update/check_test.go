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
	if !status.UpgradeAvailable || status.LatestVersion != "0.9.0" {
		t.Fatalf("status=%+v", status)
	}
	if asset := status.Asset; asset == nil || asset["download_url"] != srv.URL+"/v1/releases/packages/dagents-local-assistant/stable/linux-amd64/latest/download" {
		t.Fatalf("asset=%v", status.Asset)
	}
}
