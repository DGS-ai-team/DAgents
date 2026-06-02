package shared

import (
	"strings"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestFormatChildAgentsListEmpty(t *testing.T) {
	got := FormatChildAgentsList(nil, nil)
	if !strings.Contains(got, "(无)") {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestFormatChildAgentsListItems(t *testing.T) {
	got := FormatChildAgentsList([]nodeapi.ChildAgentListItem{{
		ChildSessionID: "child-abc",
		Purpose:        "review",
		TemplateID:     "general-helper",
		Status:         "active",
		TurnCount:      2,
		MaxTurns:       20,
		ExpiresAt:      "2026-06-02T12:00:00Z",
	}}, map[string]bool{"child-abc": true})
	for _, part := range []string{"child-abc", "review", "待审批", "2/20"} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in %q", part, got)
		}
	}
}
