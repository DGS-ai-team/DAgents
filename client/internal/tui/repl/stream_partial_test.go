package repl

import (
	"strings"
	"testing"

	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func TestStreamRunnerPartialToolCallDoesNotDuplicateTranscript(t *testing.T) {
	t.Parallel()

	tr := tuishared.NewTranscript(0)
	state := tuishared.NewToolCallStreamState()

	partial := map[string]any{
		"partial":    true,
		"tool_index": 0,
		"tool_calls": []any{
			map[string]any{
				"id":   "call-1",
				"type": "function",
				"function": map[string]any{
					"name":      "bash_run",
					"arguments": `{"command":"ls`,
				},
			},
		},
	}
	tuishared.HandleToolCallEvent(tr, state, partial, false, nil)
	tuishared.HandleToolCallEvent(tr, state, map[string]any{
		"partial":    true,
		"tool_index": 0,
		"tool_calls": []any{
			map[string]any{
				"id":   "call-1",
				"type": "function",
				"function": map[string]any{
					"name":      "bash_run",
					"arguments": `{"command":"ls -la"}`,
				},
			},
		},
	}, false, nil)

	lines := tr.Lines()
	if len(lines) == 0 {
		t.Fatal("expected partial tool lines in transcript")
	}
	pending := 0
	for _, line := range lines {
		if strings.Contains(line, "tool-pending|") || strings.Contains(line, "tool-call-code|") {
			pending++
		}
	}
	if pending > 4 {
		t.Fatalf("partial updates should replace in place, got %d tool meta lines: %v", pending, lines)
	}

	tuishared.HandleToolCallEvent(tr, state, map[string]any{
		"partial":    false,
		"tool_index": 0,
		"tool_calls": []any{
			map[string]any{
				"id":   "call-1",
				"type": "function",
				"function": map[string]any{
					"name":      "bash_run",
					"arguments": `{"command":"ls -la"}`,
				},
			},
		},
	}, false, nil)

	finalPending := 0
	for _, line := range tr.Lines() {
		if strings.Contains(line, "tool-pending|") {
			finalPending++
		}
	}
	if finalPending != 1 {
		t.Fatalf("final tool_call should upsert single pending block, got %d: %v", finalPending, tr.Lines())
	}
}
