package config

import "testing"

func TestManageWorkgroupEnabled(t *testing.T) {
	cfg := &Config{}
	if cfg.ManageWorkgroupEnabled() {
		t.Fatal("disabled manage must not enable workgroup")
	}
	cfg.Manage.Enabled = true
	if !cfg.ManageWorkgroupEnabled() {
		t.Fatal("nil workgroup.enabled defaults true when manage enabled")
	}
	off := false
	cfg.Manage.Workgroup.Enabled = &off
	if cfg.ManageWorkgroupEnabled() {
		t.Fatal("explicit false")
	}
	on := true
	cfg.Manage.Workgroup.Enabled = &on
	if !cfg.ManageWorkgroupEnabled() {
		t.Fatal("explicit true")
	}
}
