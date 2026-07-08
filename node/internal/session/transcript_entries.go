package session

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// TranscriptEntry 为 hydrate transcript 单条（JSON 形态对齐 Web UI transcriptStore.entries）。
type TranscriptEntry map[string]any

// MessagesToTranscriptEntries 将持久化 messages 映射为静态 transcript 快照（F-H14）。
func MessagesToTranscriptEntries(messages []llm.Message) []TranscriptEntry {
	if len(messages) == 0 {
		return []TranscriptEntry{}
	}
	callIndex := buildToolCallIndex(messages)
	out := make([]TranscriptEntry, 0, len(messages))
	for _, msg := range messages {
		out = append(out, messageToTranscriptEntries(msg, callIndex)...)
	}
	return out
}

func buildToolCallIndex(messages []llm.Message) map[string]llm.ToolCall {
	index := make(map[string]llm.ToolCall)
	for _, msg := range messages {
		if strings.TrimSpace(msg.Role) != "assistant" {
			continue
		}
		for _, tc := range msg.ToolCalls {
			id := strings.TrimSpace(tc.ID)
			if id != "" {
				index[id] = tc
			}
		}
	}
	return index
}

func messageToTranscriptEntries(msg llm.Message, callIndex map[string]llm.ToolCall) []TranscriptEntry {
	switch strings.TrimSpace(msg.Role) {
	case "user":
		return []TranscriptEntry{userEntry(msg)}
	case "assistant":
		return assistantEntries(msg)
	case "tool":
		if entry := toolResultEntry(msg, callIndex); entry != nil {
			return []TranscriptEntry{entry}
		}
	}
	return nil
}

func userEntry(msg llm.Message) TranscriptEntry {
	entry := TranscriptEntry{
		"kind": "user",
		"text": llm.MessageTextSummary(msg),
	}
	if images := userImageURLs(msg); len(images) > 0 {
		entry["images"] = images
	} else {
		entry["images"] = []string{}
	}
	return entry
}

func userImageURLs(msg llm.Message) []string {
	var urls []string
	for _, part := range msg.ContentParts {
		if part.Type != "image_url" || part.ImageURL == nil {
			continue
		}
		if u := strings.TrimSpace(part.ImageURL.URL); u != "" {
			urls = append(urls, u)
		}
	}
	return urls
}

func assistantEntries(msg llm.Message) []TranscriptEntry {
	var out []TranscriptEntry
	if rc := strings.TrimSpace(msg.ReasoningContent); rc != "" {
		out = append(out, TranscriptEntry{
			"kind": "reasoning",
			"text": rc,
		})
	}
	if c := strings.TrimSpace(msg.Content); c != "" {
		out = append(out, TranscriptEntry{
			"kind": "assistant",
			"text": c,
		})
	}
	for _, tc := range msg.ToolCalls {
		if tools.IsAskUserInformation(tc.Function.Name) {
			continue
		}
		out = append(out, toolCallEntryFromCall(tc, nil))
	}
	return out
}

func toolCallEntryFromCall(tc llm.ToolCall, duplicateMeta *hooks.DuplicateMeta) TranscriptEntry {
	data := turn.BuildApprovalToolItem(tc, duplicateMeta)
	data["tool_name"] = tc.Function.Name
	data["tool_call_id"] = tc.ID
	if args := parseToolArgumentsMap(tc.Function.Arguments); len(args) > 0 {
		data["arguments"] = args
	}
	summary := toolCallSummary(data)
	data["summary"] = summary
	return TranscriptEntry{
		"kind":    "tool_call",
		"blockId": tc.ID,
		"partial": false,
		"data":    data,
		"summary": summary,
	}
}

func parseToolArgumentsMap(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}

func toolCallSummary(data map[string]any) string {
	if s, ok := data["approval_reason"].(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	name := strings.TrimSpace(fmtToolName(data))
	if name == "" {
		return "tool"
	}
	if purpose, ok := data["arguments"].(map[string]any); ok {
		if cp := strings.TrimSpace(stringField(purpose, "call_purpose")); cp != "" {
			return name + "(" + cp + ")"
		}
	}
	return name + "()"
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func fmtToolName(data map[string]any) string {
	if s, ok := data["tool_name"].(string); ok {
		return s
	}
	if s, ok := data["name"].(string); ok {
		return s
	}
	return ""
}

func toolResultEntry(msg llm.Message, callIndex map[string]llm.ToolCall) TranscriptEntry {
	callID := strings.TrimSpace(msg.ToolCallID)
	if callID == "" {
		return nil
	}
	toolName := strings.TrimSpace(msg.Name)
	var args map[string]any
	if tc, ok := callIndex[callID]; ok {
		if toolName == "" {
			toolName = strings.TrimSpace(tc.Function.Name)
		}
		if parsed := parseToolArgumentsMap(tc.Function.Arguments); len(parsed) > 0 {
			args = parsed
		}
	}
	data := map[string]any{
		"tool_call_id": callID,
		"id":           callID,
		"content":      msg.Content,
	}
	if toolName != "" {
		data["tool_name"] = toolName
		data["name"] = toolName
	}
	if len(args) > 0 {
		data["arguments"] = args
	}
	summary := toolResultSummary(data)
	data["summary"] = summary
	return TranscriptEntry{
		"kind":    "tool_result",
		"blockId": callID,
		"partial": false,
		"data":    data,
		"summary": summary,
	}
}

func toolResultSummary(data map[string]any) string {
	name := strings.TrimSpace(fmtToolName(data))
	if name == "" {
		return "tool"
	}
	if args, ok := data["arguments"].(map[string]any); ok {
		if cp := strings.TrimSpace(stringField(args, "call_purpose")); cp != "" {
			return name + "(" + cp + ")"
		}
	}
	return name + "()"
}
