package shared

import (
	"strings"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestFormatSessionContextPanel_containsSections(t *testing.T) {
	text := FormatSessionContextPanel(&nodeapi.SessionContext{
		SessionID:    "s1",
		SystemPrompt: "hello world",
		MessagesCount: 3,
	})
	if !strings.Contains(text, "Session Context") {
		t.Fatalf("missing title: %q", text)
	}
	if !strings.Contains(text, "system_prompt") {
		t.Fatalf("missing section: %q", text)
	}
}
