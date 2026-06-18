package shared

import (
	"strings"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestFormatSkillsPanelBody(t *testing.T) {
	body := FormatSkillsPanelBody(&nodeapi.SessionSkills{
		SessionID: "sess-1",
		LoadedSkills: []any{
			map[string]any{"skill_name": "write-skill", "description": "Write skills"},
		},
		AvailableSkills: []any{
			map[string]any{"skill_name": "write-skill", "description": "Write skills"},
			map[string]any{"skill_name": "other", "description": "Other"},
		},
	})
	text := strings.Join(body, "\n")
	for _, part := range []string{"session|sess-1", "loaded|write-skill", "avail|other"} {
		if !strings.Contains(text, part) {
			t.Fatalf("missing %q in %q", part, text)
		}
	}
}

func TestFormatSessionsPanelBodyCurrent(t *testing.T) {
	body := FormatSessionsPanelBody([]nodeapi.SessionSummary{{
		SessionID:        "sess-a",
		Active:           true,
		MessageCount:     3,
		FirstUserMessage: "hello",
		QueuePending:     0,
		RunTurnPhase:     "idle",
	}}, "sess-a")
	text := strings.Join(body, "\n")
	if !strings.Contains(text, "sess-curr|sess-a") {
		t.Fatalf("expected current marker: %q", text)
	}
}

func TestFormatTranscriptSystemPanel(t *testing.T) {
	tr := NewTranscript(50)
	tr.AddSystemPanel("Skills", []string{
		panelLine(panelKindSection, "已加载 (1)"),
		panelLine(panelKindLoaded, "write-skill", "desc"),
	})
	lines := tr.LinesForDisplay(80)
	if len(lines) < 2 {
		t.Fatalf("expected panel lines, got %d", len(lines))
	}
	plain := stripANSI(lines[0])
	if !strings.Contains(plain, "Skills") {
		t.Fatalf("missing title: %q", plain)
	}
	plainBody := stripANSI(strings.Join(lines[1:], "\n"))
	if !strings.Contains(plainBody, "● write-skill") {
		t.Fatalf("missing loaded item: %q", plainBody)
	}
}
