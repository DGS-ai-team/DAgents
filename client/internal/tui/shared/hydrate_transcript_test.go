package shared

import (
	"testing"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestLoadTranscriptFromHydrate(t *testing.T) {
	tr := NewTranscript(100)
	entries := []nodeapi.TranscriptEntry{
		{"kind": "user", "text": "hello"},
		{"kind": "assistant", "text": "hi"},
		{
			"kind":     "tool_call",
			"blockId":  "call-1",
			"data":     map[string]any{"id": "call-1", "name": "read_file", "arguments": `{}`},
		},
	}
	LoadTranscriptFromHydrate(tr, entries, HydrateTranscriptOptions{Verbose: false})
	lines := tr.Lines()
	if len(lines) < 3 {
		t.Fatalf("lines=%v", lines)
	}
	if lines[0] != "[user] hello" {
		t.Fatalf("user line=%q", lines[0])
	}
}
