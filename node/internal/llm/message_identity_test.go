package llm

import "testing"

func TestToolResultRecordIDForExecutionScopesReusedProviderIDs(t *testing.T) {
	first := ToolResultRecordIDForExecution("reused-call", "turn-one/step-one")
	second := ToolResultRecordIDForExecution("reused-call", "turn-two/step-one")
	if first == second {
		t.Fatalf("reused provider call ID mapped to the same durable record: %q", first)
	}
	if first != "tool-result/turn-one/step-one/reused-call" || second != "tool-result/turn-two/step-one/reused-call" {
		t.Fatalf("unexpected execution-scoped identities: %q and %q", first, second)
	}
	if got := ToolResultRecordIDForExecution("reused-call", ""); got != ToolResultRecordID("reused-call") {
		t.Fatalf("empty execution scope should retain the legacy fallback, got %q", got)
	}
}
