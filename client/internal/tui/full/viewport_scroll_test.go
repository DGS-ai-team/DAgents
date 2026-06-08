package full

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func TestRefreshViewportContent_staysWhenScrolledUp(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
	}
	m.transcript.Add("line-1")
	m.transcript.Add("line-2")
	m.transcript.Add("line-3")
	m.transcript.Add("line-4")
	m.transcript.Add("line-5")
	m.viewport = viewport.New(20, 2)
	m.refreshViewportContent(true, -1)
	if !m.viewport.AtBottom() {
		t.Fatalf("expected bottom after follow, yoffset=%d", m.viewport.YOffset)
	}

	m.viewport.LineUp(2)
	if m.viewport.AtBottom() {
		t.Fatal("expected scrolled up")
	}
	yBefore := m.viewport.YOffset

	m.transcript.Add("line-6")
	m.syncViewport()

	if m.viewport.AtBottom() {
		t.Fatal("syncViewport should not jump to bottom when user scrolled up")
	}
	if m.viewport.YOffset != yBefore {
		t.Fatalf("yoffset = %d, want %d", m.viewport.YOffset, yBefore)
	}
	if !strings.Contains(m.viewport.View(), "line-3") {
		t.Fatalf("view = %q", m.viewport.View())
	}
}

func TestRefreshViewportContent_followsWhenAtBottom(t *testing.T) {
	m := &model{
		transcript: tuishared.NewTranscript(0),
	}
	for i := 0; i < 5; i++ {
		m.transcript.Add("line")
	}
	m.viewport = viewport.New(20, 2)
	m.syncViewportFollow()

	m.transcript.Add("new-tail")
	m.syncViewport()

	if !m.viewport.AtBottom() {
		t.Fatalf("expected follow at bottom, yoffset=%d", m.viewport.YOffset)
	}
	if !strings.Contains(m.viewport.View(), "new-tail") {
		t.Fatalf("view = %q", m.viewport.View())
	}
}
