package full

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestPolicyDecisionLabel(t *testing.T) {
	if policyDecisionLabel("allow_auto") != "白名单" {
		t.Fatal()
	}
	if policyDecisionLabel("deny") != "黑名单" {
		t.Fatal()
	}
	if policyDecisionLabel("require_approval") != "需审批" {
		t.Fatal()
	}
}

func TestPolicyVisibleRowsFilter(t *testing.T) {
	m := &model{
		policyMode: true,
		policySnapshot: &nodeapi.PolicySnapshot{
			Tools: []nodeapi.PolicyToolEntry{
				{Name: "read_file", Decision: "allow_auto"},
				{Name: "write_file", Decision: "require_approval"},
			},
		},
	}
	m.input = textarea.New()
	m.input.SetValue("read")
	rows := m.policyVisibleRows()
	if len(rows) != 1 || rows[0].toolName != "read_file" {
		t.Fatalf("rows=%+v", rows)
	}
}
