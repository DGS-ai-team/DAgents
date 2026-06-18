package manage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAgentCard(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent-card.json")
	if err := os.WriteFile(path, []byte(`{
  "name": "合规助手",
  "description": "内控审查",
  "capabilities": ["compliance_review"],
  "metadata": {"role": "compliance"}
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	card, err := LoadAgentCard(path)
	if err != nil || card == nil {
		t.Fatalf("LoadAgentCard: card=%v err=%v", card, err)
	}
	if card.Name != "合规助手" || card.role() != "compliance" {
		t.Fatalf("card=%+v", card)
	}
	m := card.asMap()
	if m["name"] != "合规助手" {
		t.Fatalf("map=%v", m)
	}
}

func TestLoadAgentCard_missingOptional(t *testing.T) {
	card, err := LoadAgentCard("")
	if err != nil || card != nil {
		t.Fatalf("card=%v err=%v", card, err)
	}
}
