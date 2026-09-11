package llm

import (
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

const toolResultMetadataMarker = "[TOOL_RESULT_METADATA]"

const recoveryPlaceholderErrorCode = "node_restart_unknown"

// RecoveryPlaceholderToolResult creates a provider-valid tool result for a
// call whose side effect could not be proven after a process restart. The
// placeholder closes the assistant/tool protocol pair, but its explicit
// unknown status prevents recovery from treating it as a completed result.
func RecoveryPlaceholderToolResult(call ToolCall) Message {
	return ToolResultMessageWithMetadata(
		call.ID,
		call.Function.Name,
		"工具执行状态在 Node 重启后无法核实，等待恢复确认。",
		tools.ResultMetadata{
			Status: tools.ResultStatusUnknown,
			Error: &tools.ResultError{
				Code:      recoveryPlaceholderErrorCode,
				Message:   "tool execution state is unknown after node restart",
				Retryable: false,
			},
		},
	)
}

// IsRecoveryPlaceholderToolResult reports whether a tool message is the
// internal protocol-closing placeholder written during restart recovery.
// Checking both status and error code avoids mistaking a provider's ordinary
// unknown result for this recovery marker.
func IsRecoveryPlaceholderToolResult(message Message) bool {
	if message.Role != "tool" || message.ToolResultMetadata == nil {
		return false
	}
	return strings.TrimSpace(message.ToolResultMetadata.Status) == string(tools.ResultStatusUnknown) &&
		message.ToolResultMetadata.Error != nil &&
		strings.TrimSpace(message.ToolResultMetadata.Error.Code) == recoveryPlaceholderErrorCode
}

// ToolResultMessage constructs a history tool message and derives a stable
// status projection from its raw body. Callers that already
// have the authoritative runtime classification should prefer
// ToolResultMessageWithMetadata.
func ToolResultMessage(toolCallID, name, content string) Message {
	return ToolResultMessageWithMetadata(toolCallID, name, content, classifyToolResultMetadata(name, content, false))
}

// ToolResultMessageWithMetadata preserves the raw history body while storing
// the runtime status separately for the model outbound adapter.
func ToolResultMessageWithMetadata(toolCallID, name, content string, metadata tools.ResultMetadata) Message {
	source := MessageSource{Kind: MessageSourceTool, Form: MessageFormToolResult}
	provenance := MessageProvenance{
		Producer:  strings.TrimSpace(name),
		Reference: strings.TrimSpace(toolCallID),
	}
	return Message{
		Role:               "tool",
		ToolCallID:         strings.TrimSpace(toolCallID),
		Name:               strings.TrimSpace(name),
		Content:            content,
		Source:             &source,
		Provenance:         &provenance,
		ToolResultMetadata: toolResultMetadataFromResult(metadata),
	}
}

func classifyToolResultMetadata(name, content string, rejected bool) tools.ResultMetadata {
	return tools.ClassifyResult(name, content, rejected)
}

func toolResultMetadataFromResult(metadata tools.ResultMetadata) *ToolResultMetadata {
	status := strings.TrimSpace(string(metadata.Status))
	if status == "" {
		status = string(tools.ResultStatusUnknown)
	}
	out := &ToolResultMetadata{Status: status}
	if metadata.Error != nil {
		out.Error = &ToolResultErrorDetail{
			Code:      metadata.Error.Code,
			Message:   metadata.Error.Message,
			Retryable: metadata.Error.Retryable,
		}
	}
	return out
}

// PrepareToolResultMessagesForModel creates a defensive model-facing copy of
// messages.  Runtime/UI history keeps the original tool body; only the copy
// sent to the LLM receives the compact metadata header. This keeps tool JSON,
// terminal output and transcript rendering independent from the model copy.
func PrepareToolResultMessagesForModel(messages []Message) []Message {
	if len(messages) == 0 {
		return nil
	}
	out := make([]Message, len(messages))
	for i, message := range messages {
		out[i] = CloneMessage(message)
		if out[i].Role != "tool" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(out[i].Content), toolResultMetadataMarker) {
			continue
		}
		metadata := out[i].ToolResultMetadata
		if metadata == nil || strings.TrimSpace(metadata.Status) == "" {
			metadata = toolResultMetadataFromResult(classifyToolResultMetadata(out[i].Name, out[i].Content, false))
		}
		out[i].Content = formatToolResultMetadataForModel(metadata, out[i].Content)
	}
	return out
}

func formatToolResultMetadataForModel(metadata *ToolResultMetadata, content string) string {
	if metadata == nil {
		return content
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		return content
	}
	header := toolResultMetadataMarker + " " + string(payload) + " [/TOOL_RESULT_METADATA]"
	if strings.TrimSpace(content) == "" {
		return header
	}
	return header + "\n" + content
}
