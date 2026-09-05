package turn

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestHistoryWriteReplacesRecoveryPlaceholder(t *testing.T) {
	o := NewOrchestrator("agent-1", t.TempDir(), nil, &llm.MockClient{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, nil)
	call := llm.ToolCall{
		ID: "call-recovery", Type: "function",
		Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`},
	}
	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		llm.RecoveryPlaceholderToolResult(call),
	}

	o.appendHistory("session-1", &history, llm.ToolResultMessage(call.ID, call.Function.Name, "hi"))
	if len(history) != 2 || llm.IsRecoveryPlaceholderToolResult(history[1]) || history[1].Content != "hi" {
		t.Fatalf("history after replacement = %#v", history)
	}
	if err := llm.ValidateToolProtocol(history); err != nil {
		t.Fatalf("replaced history rejected: %v", err)
	}
}

func TestHistoryInsertReplacesRecoveryPlaceholderWithoutDuplicate(t *testing.T) {
	o := NewOrchestrator("agent-1", t.TempDir(), nil, &llm.MockClient{}, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{}, nil)
	call := llm.ToolCall{
		ID: "call-recovery", Type: "function",
		Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"echo hi"}`},
	}
	history := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
		llm.RecoveryPlaceholderToolResult(call),
		{Role: "user", Content: "after restart"},
	}

	o.insertHistory("session-1", &history, 1, llm.ToolResultMessage(call.ID, call.Function.Name, "hi"))
	if len(history) != 3 || llm.IsRecoveryPlaceholderToolResult(history[1]) || history[1].Content != "hi" {
		t.Fatalf("history after replacement = %#v", history)
	}
	if err := llm.ValidateToolProtocol(history); err != nil {
		t.Fatalf("replaced history rejected: %v", err)
	}
}
