package config

import "testing"

func TestBrowserDefaults(t *testing.T) {
	cfg := &Config{}
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
	if cfg.Browser.ServiceURL != "http://127.0.0.1:18766" {
		t.Fatalf("service_url = %q", cfg.Browser.ServiceURL)
	}
}

func TestBrowserEnabled(t *testing.T) {
	on := true
	cfg := &Config{Browser: BrowserConfig{Enabled: &on}}
	if !cfg.BrowserEnabled() {
		t.Fatal("expected enabled")
	}
}

func TestValidateBrowserRejectsFileScheme(t *testing.T) {
	on := true
	cfg := &Config{
		AgentID: "test-agent",
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
