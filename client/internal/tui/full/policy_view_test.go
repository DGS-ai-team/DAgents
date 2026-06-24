package full

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestPolicyModeLabel(t *testing.T) {
	if policyModeLabel("never") != "自动允许" {
		t.Fatal()
	}
	if policyModeLabel("always") != "需审批" {
		t.Fatal()
	}
	if policyModeLabel("rule") != "特殊规则" {
		t.Fatal()
	}
	if policyModeLabel("deny") != "禁止" {
		t.Fatal()
	}
}

func TestPolicyEntryMode(t *testing.T) {
	if policyEntryMode("rule", "require_approval") != "rule" {
		t.Fatal()
	}
	if policyEntryMode("", "allow_auto") != "never" {
		t.Fatal()
	}
}

func TestPolicyVisibleRowsFilter(t *testing.T) {
	m := &model{
		policyMode: true,
		policySnapshot: &nodeapi.PolicySnapshot{
			Tools: []nodeapi.PolicyToolEntry{
				{Name: "read_file", Mode: "never"},
				{Name: "write_file", Mode: "always"},
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
