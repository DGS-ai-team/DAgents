package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestTaskComplete_pureAssistantStop(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "done"},
	}
	if !TaskComplete(msgs, nil) {
		t.Fatal("expected complete")
	}
	if TaskPhaseOf(msgs, nil) != TaskPhaseComplete {
		t.Fatalf("phase = %q", TaskPhaseOf(msgs, nil))
	}
}

func TestTaskComplete_tailToolIncomplete(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "c1", Function: llm.ToolCallFunction{Name: "bash"}}}},
		{Role: "tool", ToolCallID: "c1", Content: "ok"},
	}
	if TaskComplete(msgs, nil) {
		t.Fatal("tail tool should be incomplete")
	}
	if TaskPhaseOf(msgs, nil) != TaskPhaseToolLoop {
		t.Fatalf("phase = %q", TaskPhaseOf(msgs, nil))
	}
}

func TestTaskComplete_pendingHITL(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "x"},
		{Role: "assistant", Content: "wait"},
	}
	pending := &PendingHITL{Items: []PendingHITLItem{{ToolCall: llm.ToolCall{ID: "c1"}}}}
	if TaskComplete(msgs, pending) {
		t.Fatal("pending should block complete")
	}
	if TaskPhaseOf(msgs, pending) != TaskPhaseAwaitingHITL {
		t.Fatalf("phase = %q", TaskPhaseOf(msgs, pending))
	}
}

func TestTaskComplete_openBatch(t *testing.T) {
	msgs := []llm.Message{
		{Role: "user", Content: "run"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{
			{ID: "bg", Function: llm.ToolCallFunction{Name: "bash_run"}},
			{ID: "sync", Function: llm.ToolCallFunction{Name: "read_file"}},
		}},
		{Role: "tool", ToolCallID: "bg", Content: "[TOOL_BACKGROUND] job_id=j1"},
	}
	if TaskComplete(msgs, nil) {
		t.Fatal("open batch should be incomplete")
	}
	if TaskPhaseOf(msgs, nil) != TaskPhaseOpenBatch {
		t.Fatalf("phase = %q", TaskPhaseOf(msgs, nil))
	}
}
