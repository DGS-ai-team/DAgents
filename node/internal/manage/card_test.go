package manage

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestRegistrationCard_fromConfig(t *testing.T) {
	cfg := &config.Config{
		AgentID: "node-a",
		Agent: config.AgentConfig{
			Name:        "合规助手",
			Description: "合规审查",
			Role:        "compliance",
			Capabilities: []string{"compliance_review"},
			Metadata: map[string]any{
				"department": "内控合规部",
			},
		},
	}
	card := RegistrationCard(cfg)
	if card["name"] != "合规助手" {
		t.Fatalf("name = %v", card["name"])
	}
	meta, ok := card["metadata"].(map[string]any)
	if !ok || meta["role"] != "compliance" {
		t.Fatalf("metadata = %v", card["metadata"])
	}
}

func TestRegistrationCard_compliancePeer(t *testing.T) {
	cfg := &config.Config{
		Agent: config.AgentConfig{
			Role:           "ops",
			CompliancePeer: "node-a",
		},
	}
	card := RegistrationCard(cfg)
	meta := card["metadata"].(map[string]any)
	if meta["compliance_peer"] != "node-a" {
		t.Fatalf("peer = %v", meta["compliance_peer"])
	}
}
