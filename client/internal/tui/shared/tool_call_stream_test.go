package shared

import (
	"strings"
	"testing"
)

func TestToolCallStreamStatePartialUpsertAndMigrate(t *testing.T) {
	t.Parallel()

	tr := NewTranscript(0)
	state := NewToolCallStreamState()

	HandleToolCallEvent(tr, state, map[string]any{
		"partial":    true,
		"tool_index": 0,
		"tool_calls": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":      "bash_run",
					"arguments": `{"command":"ls`,
				},
			},
		},
	}, false, nil)

	HandleToolCallEvent(tr, state, map[string]any{
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

	if !strings.Contains(strings.Join(tr.Lines(), "\n"), "partial-0") && !strings.Contains(strings.Join(tr.Lines(), "\n"), "call-1") {
		t.Fatalf("expected pending block, got %v", tr.Lines())
	}
	pendingCount := 0
	for _, line := range tr.Lines() {
		if strings.Contains(line, "tool-pending|") || strings.Contains(line, "tool-call-code|") {
			pendingCount++
		}
	}
	if pendingCount > 4 {
		t.Fatalf("partial upsert should not accumulate blocks: %d lines %v", pendingCount, tr.Lines())
	}

	HandleToolCallEvent(tr, state, map[string]any{
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
