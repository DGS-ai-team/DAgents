package config

import "testing"

func TestBrowserDefaults(t *testing.T) {
	cfg := &Config{Browser: BrowserConfig{
		ServiceURL: "http://192.0.2.1:9000",
		CDPURL:     "http://192.0.2.2:9222",
	}}
	cfg.ApplyDefaults()
	if cfg.Browser.DefaultTimeoutMS != 30000 {
		t.Fatalf("timeout = %d", cfg.Browser.DefaultTimeoutMS)
	}
	if cfg.BrowserOutputDir() != "browser" {
		t.Fatalf("output dir = %q", cfg.BrowserOutputDir())
	}
	if cfg.BrowserEnabled() {
		t.Fatal("expected disabled by default")
	}
	if cfg.Browser.ServiceURL != DefaultBrowserServiceURL {
		t.Fatalf("service_url = %q", cfg.Browser.ServiceURL)
	}
	if cfg.Browser.CDPURL != "" {
		t.Fatalf("legacy CDP URL was not cleared: %q", cfg.Browser.CDPURL)
	}
	if cfg.Browser.MaxSessions != 8 {
		t.Fatalf("max_sessions = %d", cfg.Browser.MaxSessions)
	}
}

func TestBrowserEnabled(t *testing.T) {
	on := true
	cfg := &Config{Browser: BrowserConfig{Enabled: &on}}
	if !cfg.BrowserEnabled() {
		t.Fatal("expected enabled")
	}
}

func TestBrowserPlatformSupportHasReason(t *testing.T) {
	if BrowserPlatformReason() == "" {
		t.Fatal("expected a platform status reason")
	}
}

func TestValidateBrowserRejectsFileScheme(t *testing.T) {
	on := true
	cfg := &Config{
		NodeID: "test-agent",
		Browser: BrowserConfig{
			Enabled:           &on,
			AllowedURLSchemes: []string{"file"},
		},
	}
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected validation error for file scheme")
	}
}
