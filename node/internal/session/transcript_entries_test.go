package session

import (
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestMessagesToTranscriptEntries_basicTurn(t *testing.T) {
	t.Parallel()
	messages := []llm.Message{
		{Role: "user", Content: "hello"},
		{
			Role:             "assistant",
			ReasoningContent: "think",
			Content:          "hi",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "bash_run",
					Arguments: `{"command":"echo hi"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call-1", Name: "bash_run", Content: "hi\n"},
	}
	entries := MessagesToTranscriptEntries(messages)
	if len(entries) != 5 {
		t.Fatalf("len = %d, want 5", len(entries))
	}
	if entries[0]["kind"] != "user" || entries[0]["text"] != "hello" {
		t.Fatalf("user entry = %#v", entries[0])
	}
	if entries[1]["kind"] != "reasoning" {
		t.Fatalf("reasoning entry = %#v", entries[1])
	}
	if entries[2]["kind"] != "assistant" {
		t.Fatalf("assistant entry = %#v", entries[2])
	}
	if entries[3]["kind"] != "tool_call" || entries[3]["blockId"] != "call-1" {
		t.Fatalf("tool_call entry = %#v", entries[3])
	}
	if entries[4]["kind"] != "tool_result" || entries[4]["blockId"] != "call-1" {
		t.Fatalf("tool_result entry = %#v", entries[4])
	}
}

func TestMessagesToTranscriptEntries_skipsAskUserInformationToolCall(t *testing.T) {
	t.Parallel()
	messages := []llm.Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-ui",
			Function: llm.ToolCallFunction{
				Name:      "ask_user_information",
				Arguments: `{"question":"name?"}`,
			},
		}}},
	}
	entries := MessagesToTranscriptEntries(messages)
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1 (ask_user_information skipped)", len(entries))
	}
}

func TestMessagesToTranscriptEntries_userImages(t *testing.T) {
	t.Parallel()
	messages := []llm.Message{
		{
			Role:    "user",
			Content: "see",
			ContentParts: []llm.ContentPart{{
				Type:     "image_url",
				ImageURL: &llm.ImageURLPart{URL: "data:image/png;base64,abc"},
			}},
		},
	}
	entries := MessagesToTranscriptEntries(messages)
	images, ok := entries[0]["images"].([]string)
	if !ok || len(images) != 1 || images[0] != "data:image/png;base64,abc" {
		t.Fatalf("images = %#v", entries[0]["images"])
	}
}
