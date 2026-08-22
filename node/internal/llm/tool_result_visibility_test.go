package llm

import (
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestToolResultMessageDerivesPersistableStatus(t *testing.T) {
	success := ToolResultMessage("call-1", "read_file", "file body")
	if success.ToolResultMetadata == nil || success.ToolResultMetadata.Status != string(tools.ResultStatusSucceeded) {
		t.Fatalf("success metadata = %+v", success.ToolResultMetadata)
	}

	denied := ToolResultMessage("call-2", "write_file", "rejected: policy_denied")
	if denied.ToolResultMetadata == nil || denied.ToolResultMetadata.Status != string(tools.ResultStatusDenied) {
		t.Fatalf("denied metadata = %+v", denied.ToolResultMetadata)
	}
	if denied.ToolResultMetadata.Error == nil || denied.ToolResultMetadata.Error.Code != "policy_denied" {
		t.Fatalf("denied error = %+v", denied.ToolResultMetadata.Error)
	}
}

func TestPrepareToolResultMessagesForModelKeepsHistoryBodyAndAddsMetadata(t *testing.T) {
	const body = `{"items":[],"next_offset":0}`
	history := []Message{
		{Role: "user", Content: "查找"},
		ToolResultMessage("call-1", "grep_file", body),
	}
	out := PrepareToolResultMessagesForModel(history)
	if len(out) != len(history) {
		t.Fatalf("out length = %d", len(out))
	}
	if history[1].Content != body {
		t.Fatalf("history body mutated = %q", history[1].Content)
	}
	if out[1].Content == body || !strings.Contains(out[1].Content, "[TOOL_RESULT_METADATA]") {
		t.Fatalf("model content missing metadata: %q", out[1].Content)
	}
	if !strings.Contains(out[1].Content, `"status":"succeeded"`) || !strings.Contains(out[1].Content, body) {
		t.Fatalf("model content lost status/body: %q", out[1].Content)
	}
	if out[0].Content != history[0].Content {
		t.Fatalf("non-tool message changed: %+v", out[0])
	}
}

func TestPrepareToolResultMessagesForModelIncludesFailureEvidence(t *testing.T) {
	history := []Message{ToolResultMessage("call-1", "bash_run", "ERROR: exit 1")}
	out := PrepareToolResultMessagesForModel(history)
	content := out[0].Content
	for _, part := range []string{`"status":"failed"`, `"code":"tool_failed"`, `"retryable":false`, "ERROR: exit 1"} {
		if !strings.Contains(content, part) {
			t.Fatalf("failure metadata missing %q: %q", part, content)
		}
	}
}

func TestPrepareToolResultMessagesForModelDoesNotDoubleAnnotate(t *testing.T) {
	content := `[TOOL_RESULT_METADATA] {"status":"succeeded"} [/TOOL_RESULT_METADATA]\nbody`
	out := PrepareToolResultMessagesForModel([]Message{{Role: "tool", Content: content}})
	if out[0].Content != content {
		t.Fatalf("metadata was duplicated: %q", out[0].Content)
	}
}
