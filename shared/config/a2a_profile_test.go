package config

import "testing"

func TestExposeToPeersEffective_AcceptInbound(t *testing.T) {
	cfg := &Config{}
	if cfg.ExposeToPeersEffective() {
		t.Fatal("default must not expose")
	}
	on := true
	cfg.Manage.A2A.AcceptInbound = &on
	if !cfg.ExposeToPeersEffective() {
		t.Fatal("accept_inbound=true should expose")
	}
	off := false
	cfg.Manage.A2A.AcceptInbound = &off
	if cfg.ExposeToPeersEffective() {
		t.Fatal("accept_inbound=false should not expose")
	}
	// role 不再影响
	cfg.Agent.Role = "compliance"
	if cfg.ExposeToPeersEffective() {
		t.Fatal("role must not force expose")
	}
}

func TestManageA2AInboxEnabled(t *testing.T) {
	cfg := &Config{Manage: ManageConfig{Enabled: true}}
	if cfg.ManageA2AEnabled() {
		t.Fatal("default inbox off")
	}
	on := true
	cfg.Manage.A2A.AcceptInbound = &on
	if !cfg.ManageA2AEnabled() {
		t.Fatal("inbox should follow accept_inbound")
	}
	disabled := false
	cfg.Manage.A2A.Enabled = &disabled
	if cfg.ManageA2AEnabled() {
		t.Fatal("explicit a2a.enabled=false should win")
	}
}

func TestExposeToPeersForRole_Deprecated(t *testing.T) {
	if ExposeToPeersForRole("compliance", nil) {
		t.Fatal("deprecated helper must not expose by role")
	}
	on := true
	if !ExposeToPeersForRole("ops", &on) {
		t.Fatal("yamlOverride should still work")
	}
}

func TestAgentDisplayNameFallback(t *testing.T) {
	cfg := &Config{NodeID: "node-a"}
	if cfg.NodeDisplayName() != "node-a" {
		t.Fatalf("name = %q", cfg.NodeDisplayName())
	}
	cfg.Agent.Name = "展示名"
	if cfg.NodeDisplayName() != "展示名" {
		t.Fatalf("name = %q", cfg.NodeDisplayName())
	}
}

func TestPlacementFlagsAlwaysFalse(t *testing.T) {
	cfg := &Config{}
	if cfg.AllowPeerCreateEffective() || cfg.AllowScreenViewEffective() {
		t.Fatal("placement flags must stay false after D5")
	}
	on := true
	cfg.Placement.AllowPeerCreate = &on
	cfg.Placement.AllowScreenView = &on
	if cfg.AllowPeerCreateEffective() || cfg.AllowScreenViewEffective() {
		t.Fatal("YAML placement knobs must not re-enable product capability")
	}
}
