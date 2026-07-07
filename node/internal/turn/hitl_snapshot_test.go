package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestStableHITLID_deterministic(t *testing.T) {
	t.Parallel()
	pending := &PendingHITL{Items: []PendingHITLItem{{
		ToolCall: llm.ToolCall{ID: "call-b", Function: llm.ToolCallFunction{Name: "bash_run"}},
	}, {
		ToolCall: llm.ToolCall{ID: "call-a", Function: llm.ToolCallFunction{Name: "write_file"}},
	}}}
	id1 := StableHITLID(pending)
	id2 := StableHITLID(pending)
	if id1 == "" || id1 != id2 {
		t.Fatalf("stable id = %q %q", id1, id2)
	}
	if got := BuildHITLRequiredSnapshot(nil); got != nil {
		t.Fatalf("nil pending snapshot = %#v", got)
	}
	snap := BuildHITLRequiredSnapshot(pending)
	if snap["hitl_id"] != id1 {
		t.Fatalf("hitl_id = %v", snap["hitl_id"])
	}
	items, ok := snap["items"].([]map[string]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v", snap["items"])
	}
}
