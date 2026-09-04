package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidMessageHistory marks a model message sequence that cannot be
// sent to an OpenAI-compatible chat completion endpoint.
var ErrInvalidMessageHistory = errors.New("invalid model message history")

// HistoryViolation identifies one protocol-level history problem without
// retaining the full tool arguments in the error.  Callers can safely log the
// result together with session/turn metadata.
type HistoryViolation struct {
	Code         string
	MessageIndex int
	ToolIndex    int
	ToolCallID   string
	Detail       string
}

// HistoryValidationError is returned by the canonical history boundary.
type HistoryValidationError struct {
	Violations []HistoryViolation
}

func (e *HistoryValidationError) Error() string {
	if e == nil || len(e.Violations) == 0 {
		return ErrInvalidMessageHistory.Error()
	}
	first := e.Violations[0]
	if first.Detail == "" {
		return fmt.Sprintf("%s at message %d", first.Code, first.MessageIndex)
	}
	return fmt.Sprintf("%s at message %d: %s", first.Code, first.MessageIndex, first.Detail)
}

func (e *HistoryValidationError) Unwrap() error { return ErrInvalidMessageHistory }

func appendHistoryViolation(out *[]HistoryViolation, violation HistoryViolation) {
	if out == nil {
		return
	}
	*out = append(*out, violation)
}

func invalidHistoryError(violations []HistoryViolation) error {
	if len(violations) == 0 {
		return nil
	}
	return &HistoryValidationError{Violations: violations}
}

// ValidateAssistantMessage validates only the wire-level tool-call contract.
// Tool-specific required fields belong to the tool executor/schema layer.
func ValidateAssistantMessage(message Message) error {
	if strings.TrimSpace(message.Role) != "assistant" || len(message.ToolCalls) == 0 {
		return nil
	}
	violations := make([]HistoryViolation, 0)
	seen := make(map[string]struct{}, len(message.ToolCalls))
	for index, call := range message.ToolCalls {
		id := strings.TrimSpace(call.ID)
		if id == "" {
			appendHistoryViolation(&violations, HistoryViolation{
				Code:      "assistant_tool_call_missing_id",
				ToolIndex: index,
			})
		} else if _, exists := seen[id]; exists {
			appendHistoryViolation(&violations, HistoryViolation{
				Code:       "assistant_tool_call_duplicate_id",
				ToolIndex:  index,
				ToolCallID: id,
			})
		} else {
			seen[id] = struct{}{}
		}

		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			appendHistoryViolation(&violations, HistoryViolation{
				Code:       "assistant_tool_call_missing_name",
				ToolIndex:  index,
				ToolCallID: id,
			})
		}

		arguments := strings.TrimSpace(call.Function.Arguments)
		var object map[string]any
		if arguments == "" || !json.Valid([]byte(arguments)) {
			appendHistoryViolation(&violations, HistoryViolation{
				Code:       "assistant_tool_call_invalid_arguments_json",
				ToolIndex:  index,
				ToolCallID: id,
			})
			continue
		}
		if err := json.Unmarshal([]byte(arguments), &object); err != nil || object == nil {
			appendHistoryViolation(&violations, HistoryViolation{
				Code:       "assistant_tool_call_arguments_not_object",
				ToolIndex:  index,
				ToolCallID: id,
			})
		}
	}
	return invalidHistoryError(violations)
}

// ValidateToolProtocol validates the complete model-facing message sequence.
// It intentionally allows consecutive user messages and does not validate
// tool-specific JSON Schema fields.
func ValidateToolProtocol(messages []Message) error {
	violations := make([]HistoryViolation, 0)
	open := make(map[string]int)
	seenResult := make(map[string]int)

	flushOpen := func(messageIndex int, code string) {
		for id, toolIndex := range open {
			appendHistoryViolation(&violations, HistoryViolation{
				Code:         code,
				MessageIndex: messageIndex,
				ToolIndex:    toolIndex,
				ToolCallID:   id,
			})
		}
		open = make(map[string]int)
	}

	for messageIndex, message := range messages {
		switch strings.TrimSpace(message.Role) {
		case "assistant":
			if len(open) > 0 {
				flushOpen(messageIndex, "tool_batch_unclosed_before_next_message")
			}
			// Tool-call/result IDs are scoped to an assistant batch. Some
			// compatible test/model backends may reuse an ID in a later batch;
			// only duplicate results within the current batch are invalid.
			seenResult = make(map[string]int)
			if err := ValidateAssistantMessage(message); err != nil {
				var validationErr *HistoryValidationError
				if errors.As(err, &validationErr) {
					for _, violation := range validationErr.Violations {
						violation.MessageIndex = messageIndex
						appendHistoryViolation(&violations, violation)
					}
				} else {
					appendHistoryViolation(&violations, HistoryViolation{
						Code:         "assistant_tool_call_invalid",
						MessageIndex: messageIndex,
					})
				}
			}
			// Only calls that pass the per-call wire checks can be opened. This
			// keeps the remaining diagnostics useful without treating a malformed
			// call as a valid call/result pair.
			for toolIndex, call := range message.ToolCalls {
				if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Function.Name) == "" {
					continue
				}
				arguments := strings.TrimSpace(call.Function.Arguments)
				var object map[string]any
				if arguments == "" || !json.Valid([]byte(arguments)) || json.Unmarshal([]byte(arguments), &object) != nil || object == nil {
					continue
				}
				id := strings.TrimSpace(call.ID)
				if _, duplicate := open[id]; duplicate {
					continue
				}
				open[id] = toolIndex
			}
		case "tool":
			id := strings.TrimSpace(message.ToolCallID)
			if id == "" {
				appendHistoryViolation(&violations, HistoryViolation{
					Code:         "tool_result_missing_call_id",
					MessageIndex: messageIndex,
				})
				continue
			}
			if _, duplicate := seenResult[id]; duplicate {
				appendHistoryViolation(&violations, HistoryViolation{
					Code:         "tool_result_duplicate",
					MessageIndex: messageIndex,
					ToolCallID:   id,
				})
				continue
			}
			if _, exists := open[id]; !exists {
				appendHistoryViolation(&violations, HistoryViolation{
					Code:         "tool_result_orphan",
					MessageIndex: messageIndex,
					ToolCallID:   id,
				})
				continue
			}
			seenResult[id] = messageIndex
			delete(open, id)
		case "user", "system":
			if len(open) > 0 {
				flushOpen(messageIndex, "tool_batch_unclosed_before_next_message")
			}
			seenResult = make(map[string]int)
		case "":
			appendHistoryViolation(&violations, HistoryViolation{
				Code:         "message_role_missing",
				MessageIndex: messageIndex,
			})
		default:
			appendHistoryViolation(&violations, HistoryViolation{
				Code:         "message_role_unsupported",
				MessageIndex: messageIndex,
				Detail:       strings.TrimSpace(message.Role),
			})
		}
	}
	if len(open) > 0 {
		flushOpen(len(messages), "tool_batch_unclosed_at_history_end")
	}
	return invalidHistoryError(violations)
}
