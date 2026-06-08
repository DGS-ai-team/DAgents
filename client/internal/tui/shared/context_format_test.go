package shared

import (
	"strings"
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestFormatSessionContext_includesSystemPrompt(t *testing.T) {
	text := FormatSessionContext(&nodeapi.SessionContext{
		SessionID:           "sess-1",
		MessagesCount:       2,
		MessagesTotalTokens: 100,
		SystemPrompt:        "You are a helpful agent.\nFollow rules.",
	})
	if !strings.Contains(text, "system_prompt:") {
		t.Fatalf("missing system_prompt section: %q", text)
	}
	if !strings.Contains(text, "You are a helpful agent.") {
		t.Fatalf("missing prompt body: %q", text)
	}
}
