package shared

import (
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestSkillsBloatWarningLines_belowThreshold(t *testing.T) {
	if got := SkillsBloatWarningLines(&nodeapi.SessionContext{
		SkillsCatalogEstimatedTokens: 100,
		SkillsCatalogBloatThreshold:  4000,
	}); len(got) != 0 {
		t.Fatalf("expected no lines, got %v", got)
	}
}

func TestSkillsBloatWarningLines_aboveThreshold(t *testing.T) {
	lines := SkillsBloatWarningLines(&nodeapi.SessionContext{
		SkillsCatalogEstimatedTokens: 5000,
		SkillsCatalogBloatThreshold:  4000,
	})
	if len(lines) != 2 {
		t.Fatalf("lines = %d want 2", len(lines))
	}
}
