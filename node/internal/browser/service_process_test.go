package browser

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestLocalBrowserServiceAddressOnlyAllowsLoopbackHTTP(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantAddr  string
		wantError bool
	}{
		{name: "default v4", url: "http://127.0.0.1:18766", wantAddr: "127.0.0.1:18766"},
		{name: "localhost", url: "http://localhost:18766/", wantAddr: "127.0.0.1:18766"},
		{name: "v6 loopback", url: "http://[::1]:18766", wantAddr: "[::1]:18766"},
		{name: "remote", url: "http://192.0.2.1:18766", wantError: true},
		{name: "https", url: "https://127.0.0.1:18766", wantError: true},
		{name: "path", url: "http://127.0.0.1:18766/api", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, _, err := localBrowserServiceAddress(tt.url)
			if (err != nil) != tt.wantError {
				t.Fatalf("address error = %v, wantError %t", err, tt.wantError)
			}
			if err == nil && addr != tt.wantAddr {
				t.Fatalf("address = %q, want %q", addr, tt.wantAddr)
			}
		})
	}
}

func TestBrowserSidecarSettingsUseEffectiveNodeConfig(t *testing.T) {
	headed, ignoreHTTPS := false, true
	cfg := &config.Config{
		RuntimeRoot: filepath.Join(t.TempDir(), "runtime"),
		Browser: config.BrowserConfig{
			Headed: &headed, ChromePath: "C:/Chrome/chrome.exe", CDPURL: "http://127.0.0.1:9222",
			IgnoreHTTPSErrors: &ignoreHTTPS, DefaultTimeoutMS: 12000, OutputDir: "shots", MaxSessions: 3,
			AllowedURLSchemes: []string{"https"},
		},
	}
	settings, err := browserSidecarSettings(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := filepath.Abs(cfg.RuntimeDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(settings.RuntimeRoot) != filepath.Clean(wantRoot) {
		t.Fatalf("runtime root = %q, want absolute form of %q", settings.RuntimeRoot, cfg.RuntimeDir())
	}
	if settings.Browser.Headed || !settings.Browser.IgnoreHTTPSErrors || settings.Browser.DefaultTimeoutMS != 12000 || settings.Browser.MaxSessions != 3 {
		t.Fatalf("effective browser settings not preserved: %#v", settings.Browser)
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if _, ok := decoded["llm"]; ok {
		t.Fatal("sidecar config must not contain Node LLM secrets")
	}
}

func TestBrowserServiceHealthyRequiresSuccessfulPing(t *testing.T) {
	if browserServiceHealthy("http://127.0.0.1:1", 100000000) {
		t.Fatal("unavailable endpoint reported healthy")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	if !browserServiceHealthy(server.URL, 100000000) {
		t.Fatal("healthy Browser ping was rejected")
	}
}
