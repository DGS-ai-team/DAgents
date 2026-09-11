package llm

import (
	"errors"
	"testing"
)

func validHistoryToolCall(id, name, args string) ToolCall {
	return ToolCall{
		ID: id, Type: "function",
		Function: ToolCallFunction{Name: name, Arguments: args},
	}
}

func TestValidateAssistantMessageRejectsIncompleteArguments(t *testing.T) {
	err := ValidateAssistantMessage(Message{
		Role: "assistant",
		ToolCalls: []ToolCall{validHistoryToolCall(
			"call-1", "bash_run", `{"command":"Get-Process`,
		)},
	})
	if err == nil || !errors.Is(err, ErrInvalidMessageHistory) {
		t.Fatalf("error = %v, want invalid history", err)
	}
	var validationErr *HistoryValidationError
	if !errors.As(err, &validationErr) || len(validationErr.Violations) != 1 {
		t.Fatalf("validation error = %#v", err)
	}
	if got := validationErr.Violations[0].Code; got != "assistant_tool_call_invalid_arguments_json" {
		t.Fatalf("violation code = %q", got)
	}
}

func TestValidateToolProtocolAllowsConsecutiveUsersAndRepeatedIDsInLaterBatches(t *testing.T) {
	history := []Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", ToolCalls: []ToolCall{validHistoryToolCall("call-1", "bash_run", `{ "command": "echo one" }`)}},
		ToolResultMessage("call-1", "bash_run", "one"),
		{Role: "user", Content: "second"},
		{Role: "assistant", ToolCalls: []ToolCall{validHistoryToolCall("call-1", "bash_run", `{ "command": "echo two" }`)}},
		ToolResultMessage("call-1", "bash_run", "two"),
		{Role: "user", Content: "third"},
	}
	if err := ValidateToolProtocol(history); err != nil {
		t.Fatalf("valid history rejected: %v", err)
	}
}

func TestValidateToolProtocolRejectsOrphanAndUnclosedCalls(t *testing.T) {
	history := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{validHistoryToolCall("call-1", "bash_run", `{}`)}},
		{Role: "user", Content: "follow up"},
	}
	err := ValidateToolProtocol(history)
	if err == nil {
		t.Fatal("expected invalid history")
	}
	var validationErr *HistoryValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %#v", err)
	}
	if len(validationErr.Violations) != 1 || validationErr.Violations[0].Code != "tool_batch_unclosed_before_next_message" {
		t.Fatalf("violations = %#v", validationErr.Violations)
	}
}

func TestValidateAssistantMessageDoesNotValidateToolSchema(t *testing.T) {
	message := Message{
		Role:      "assistant",
		ToolCalls: []ToolCall{validHistoryToolCall("call-1", "bash_run", `{}`)},
	}
	if err := ValidateAssistantMessage(message); err != nil {
		t.Fatalf("schema-validity-independent tool call rejected: %v", err)
	}
}

func TestValidateHistoryAcceptsPlainTextAndParallelToolBatch(t *testing.T) {
	history := []Message{
		{Role: "system", Content: "instructions"},
		{Role: "user", Content: "run both"},
		{Role: "assistant", ToolCalls: []ToolCall{
			validHistoryToolCall("call-a", "read_file", `{ "path": "a" }`),
			validHistoryToolCall("call-b", "list_dir", `{ "path": "." }`),
		}},
		ToolResultMessage("call-b", "list_dir", "b"),
		ToolResultMessage("call-a", "read_file", "a"),
		{Role: "assistant", Content: "done"},
	}
	if err := ValidateToolProtocol(history); err != nil {
		t.Fatalf("valid history rejected: %v", err)
	}
}

func TestValidateAssistantMessageRejectsNonObjectArguments(t *testing.T) {
	for name, arguments := range map[string]string{
		"array":  `[]`,
		"string": `"value"`,
		"number": `42`,
	} {
		t.Run(name, func(t *testing.T) {
			err := ValidateAssistantMessage(Message{
				Role:      "assistant",
				ToolCalls: []ToolCall{validHistoryToolCall("call-1", "tool", arguments)},
			})
			if err == nil || !errors.Is(err, ErrInvalidMessageHistory) {
				t.Fatalf("error=%v, want invalid history", err)
			}
			var validationErr *HistoryValidationError
			if !errors.As(err, &validationErr) || len(validationErr.Violations) != 1 || validationErr.Violations[0].Code != "assistant_tool_call_arguments_not_object" {
				t.Fatalf("validation error=%#v", err)
			}
		})
	}
}

func TestValidateToolProtocolRejectsDuplicateAndOrphanResults(t *testing.T) {
	history := []Message{
		{Role: "assistant", ToolCalls: []ToolCall{validHistoryToolCall("call-1", "tool", `{}`)}},
		ToolResultMessage("call-1", "tool", "first"),
		ToolResultMessage("call-1", "tool", "duplicate"),
		ToolResultMessage("call-orphan", "tool", "orphan"),
	}
	err := ValidateToolProtocol(history)
	if err == nil {
		t.Fatal("expected duplicate/orphan result error")
	}
	var validationErr *HistoryValidationError
	if !errors.As(err, &validationErr) || len(validationErr.Violations) != 2 {
		t.Fatalf("validation error=%#v", err)
	}
	if validationErr.Violations[0].Code != "tool_result_duplicate" || validationErr.Violations[1].Code != "tool_result_orphan" {
		t.Fatalf("violations=%+v", validationErr.Violations)
	}
}
