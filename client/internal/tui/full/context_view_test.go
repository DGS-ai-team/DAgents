package full

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func TestContextView_viewportLimitsHeight(t *testing.T) {
	longPrompt := strings.Repeat("line\n", 80)
	ctx := &nodeapi.SessionContext{
		SessionID:    "sess-1",
		SystemPrompt: longPrompt,
	}
	text := tuishared.FormatSessionContext(ctx)

	m := &model{
		contextMode: true,
		contextText: text,
	}
	m.viewport = viewport.New(40, 5)
	m.refreshContextViewportContent(false, 0)

	view := m.viewport.View()
	if strings.Count(view, "\n") >= 80 {
		t.Fatalf("viewport should clip content, got %d lines in view", strings.Count(view, "\n"))
	}
}

func TestContextView_scrollIncreasesYOffset(t *testing.T) {
	m := &model{
		contextMode: true,
		contextText: strings.Repeat("context-line\n", 30),
	}
	m.viewport = viewport.New(40, 4)
	m.refreshContextViewportContent(false, 0)

	m.viewport.LineDown(3)
	if m.viewport.YOffset == 0 {
		t.Fatal("expected YOffset > 0 after scroll")
	}
}

func TestApplySize_contextModePreservesContextText(t *testing.T) {
	m := &model{
		contextMode: true,
		contextText: "context-only-content",
		transcript:  tuishared.NewTranscript(0),
		input:       textarea.New(),
	}
	m.transcript.Add("[user] chat line")
	m.viewport = viewport.New(40, 10)
	m.refreshContextViewportContent(false, 0)

	m.applySize(40, 24)

	if !strings.Contains(m.viewport.View(), "context-only-content") {
		t.Fatalf("resize replaced context with transcript: %q", m.viewport.View())
	}
	if strings.Contains(m.viewport.View(), "chat line") {
		t.Fatal("transcript leaked into context viewport")
	}
}
