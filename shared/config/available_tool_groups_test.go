package config

import "testing"

func TestUIEnabledAlwaysTrue(t *testing.T) {
	off := false
	cfg := &Config{UI: UIConfig{Enabled: &off}}
	if !cfg.UIEnabled() {
		t.Fatal("UIEnabled must always be true")
	}
	if !(*Config)(nil).UIEnabled() {
		t.Fatal("nil config UIEnabled must be true")
	}
}

func TestAvailableAgentToolGroupsGatesBrowserWeCom(t *testing.T) {
	cfg := &Config{}
	groups := cfg.AvailableAgentToolGroups()
	for _, g := range groups {
		if g == "browser" || g == "wecom" {
			t.Fatalf("expected browser/wecom gated off by default, got %v", groups)
		}
	}

	on := true
	cfg.Browser.Enabled = &on
	cfg.WeCom.Enabled = &on
	groups = cfg.AvailableAgentToolGroups()
	hasBrowser, hasWeCom := false, false
	for _, g := range groups {
		if g == "browser" {
			hasBrowser = true
		}
		if g == "wecom" {
			hasWeCom = true
		}
	}
	if !hasBrowser || !hasWeCom {
		t.Fatalf("expected browser+wecom when enabled, got %v", groups)
	}
}

func TestFilterAgentToolGroups(t *testing.T) {
	cfg := &Config{}
	got := cfg.FilterAgentToolGroups([]string{"fs", "browser", "bash", "wecom", "fs"})
	if len(got) != 2 || got[0] != "fs" || got[1] != "bash" {
		t.Fatalf("got %#v", got)
	}
}
