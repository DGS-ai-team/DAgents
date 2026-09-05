package session

import (
	"encoding/base64"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/media"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
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

func TestMessagesToTranscriptEntries_userFileReferences(t *testing.T) {
	t.Parallel()
	messages := []llm.Message{{
		Role:           "user",
		Content:        "请读取",
		FileReferences: []llm.FileReference{{Path: `D:\work\README.md`, Name: "README.md"}},
	}}
	entries := MessagesToTranscriptEntries(messages)
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	if entries[0]["text"] != "请读取" {
		t.Fatalf("text = %#v", entries[0]["text"])
	}
	refs, ok := entries[0]["file_refs"].([]llm.FileReference)
	if !ok || len(refs) != 1 || refs[0].Path != `D:\work\README.md` {
		t.Fatalf("file_refs = %#v", entries[0]["file_refs"])
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

func TestMessagesToTranscriptEntries_skipsInjectedUserMessages(t *testing.T) {
	t.Parallel()
	messages := []llm.Message{
		llm.UserMessage("当天日期为：20260720", llm.UserNameDate),
		llm.UserMessage("异步工具结果回灌", llm.UserNameAsyncTool),
		llm.UserMessage("压缩摘要", llm.UserNameCompression),
		llm.UserMessage("<skill_instructions>正文</skill_instructions>", llm.UserNameSkill),
		{Role: "user", Content: "真实用户问题"},
	}
	entries := MessagesToTranscriptEntries(messages)
	if len(entries) != 1 {
		t.Fatalf("len = %d, want 1", len(entries))
	}
	if entries[0]["kind"] != "user" || entries[0]["text"] != "真实用户问题" {
		t.Fatalf("entry = %#v", entries[0])
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

func TestMessagesToTranscriptEntries_userMediaRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	reg, err := media.NewRegistry("sess-tr", dir)
	if err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte{0x89, 0x50, 0x4e, 0x47})
	stored, err := media.PersistUserMessageImages(reg, llm.Message{
		Role:    "user",
		Content: "pic",
		ContentParts: []llm.ContentPart{{
			Type:     "image_url",
			ImageURL: &llm.ImageURLPart{URL: dataURL},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := MessagesToTranscriptEntriesWithMedia([]llm.Message{stored}, reg)
	images, _ := entries[0]["images"].([]string)
	mediaItems, _ := entries[0]["media"].([]map[string]any)
	if len(images) != 1 || len(mediaItems) != 1 {
		t.Fatalf("entry=%#v", entries[0])
	}
	if images[0] != mediaItems[0]["url"] {
		t.Fatalf("images=%v media=%v", images, mediaItems)
	}
}

func TestMessagesToTranscriptEntries_toolResultBackfillName(t *testing.T) {
	t.Parallel()
	messages := []llm.Message{
		{Role: "user", Content: "run"},
		{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{{
				ID:   "call-x",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "bash_run",
					Arguments: `{"command":"echo hi","call_purpose":"test weather"}`,
				},
			}},
		},
		{Role: "tool", ToolCallID: "call-x", Content: "hi\n"},
	}
	entries := MessagesToTranscriptEntries(messages)
	if len(entries) != 3 {
		t.Fatalf("len = %d, want 3 (user + tool_call + tool_result)", len(entries))
	}
	res := entries[2]
	if res["kind"] != "tool_result" {
		t.Fatalf("kind = %#v", res["kind"])
	}
	data, _ := res["data"].(map[string]any)
	if data["tool_name"] != "bash_run" {
		t.Fatalf("tool_name = %#v", data["tool_name"])
	}
	args, _ := data["arguments"].(map[string]any)
	if args["call_purpose"] != "test weather" {
		t.Fatalf("arguments = %#v", data["arguments"])
	}
}

func TestMessagesToTranscriptEntries_preservesToolResultMetadata(t *testing.T) {
	t.Parallel()
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID:       "call-meta",
			Function: llm.ToolCallFunction{Name: "terminal_command", Arguments: `{"command":"pwd"}`},
		}}},
		llm.ToolResultMessageWithMetadata("call-meta", "terminal_command", "命令已超时。", tools.ResultMetadata{
			Status: tools.ResultStatusTimedOut,
			Error:  &tools.ResultError{Code: "timeout", Message: "command exceeded timeout", Retryable: true},
		}),
	}

	entries := MessagesToTranscriptEntries(messages)
	if len(entries) != 2 {
		t.Fatalf("len = %d, want 2", len(entries))
	}
	data, _ := entries[1]["data"].(map[string]any)
	if data["status"] != string(tools.ResultStatusTimedOut) {
		t.Fatalf("status = %#v", data["status"])
	}
	if _, ok := data["rejected"]; ok {
		t.Fatalf("tool transcript must not expose duplicate rejected field: %#v", data)
	}
	errData, _ := data["error"].(map[string]any)
	if errData["code"] != "timeout" || errData["retryable"] != true {
		t.Fatalf("error = %#v", data["error"])
	}
}

func TestEnrichTranscriptMedia(t *testing.T) {
	t.Parallel()
	entries := []TranscriptEntry{
		{
			"kind":    "tool_result",
			"blockId": "call-img",
			"data":    map[string]any{"tool_name": "show_image"},
		},
	}
	EnrichTranscriptMedia(entries, map[string][]map[string]any{
		"call-img": {{"id": "med_1", "url": "/v1/agents/s/media/med_1"}},
	})
	data, _ := entries[0]["data"].(map[string]any)
	media, _ := data["media"].([]map[string]any)
	if len(media) != 1 || media[0]["id"] != "med_1" {
		t.Fatalf("media = %#v", data["media"])
	}
}
