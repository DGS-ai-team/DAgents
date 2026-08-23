package llm

import (
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

const toolResultMetadataMarker = "[TOOL_RESULT_METADATA]"

// ToolResultMessage constructs a history tool message and derives a stable
// status projection from its legacy-compatible body.  Callers that already
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
	return tools.ClassifyToolResult(name, content, rejected)
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
// terminal output and transcript rendering backwards compatible.
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
