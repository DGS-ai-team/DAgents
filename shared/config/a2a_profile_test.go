package config

import "testing"

func TestExposeToPeersForRole(t *testing.T) {
	if !ExposeToPeersForRole("compliance", nil) {
		t.Fatal("compliance should expose")
	}
	if ExposeToPeersForRole("ops", nil) {
		t.Fatal("ops should not expose")
	}
}

func TestManageA2AInboxEnabledForRole(t *testing.T) {
	cfg := &Config{
		Manage: ManageConfig{Enabled: true},
	}
	if !cfg.ManageA2AInboxEnabledForRole("compliance") {
		t.Fatal("compliance should poll inbox by default")
	}
	if cfg.ManageA2AInboxEnabledForRole("ops") {
		t.Fatal("ops should not poll inbox by default")
	}
	disabled := false
	cfg.Manage.A2A.Enabled = &disabled
	if cfg.ManageA2AInboxEnabledForRole("compliance") {
		t.Fatal("explicit a2a.enabled=false should win")
	}
}

func TestAgentRoleExposeEffective(t *testing.T) {
	cfg := &Config{
		Agent: AgentConfig{Role: "compliance"},
	}
	if !cfg.ExposeToPeersEffective() {
		t.Fatal("expected expose for compliance")
	}
	cfg.Agent.Role = "ops"
	if cfg.ExposeToPeersEffective() {
		t.Fatal("expected no expose for ops")
	}
}

func TestAgentDisplayNameFallback(t *testing.T) {
	cfg := &Config{AgentID: "node-a"}
	if cfg.AgentDisplayName() != "node-a" {
		t.Fatalf("name = %q", cfg.AgentDisplayName())
	}
	cfg.Agent.Name = "展示名"
	if cfg.AgentDisplayName() != "展示名" {
		t.Fatalf("name = %q", cfg.AgentDisplayName())
	}
}
