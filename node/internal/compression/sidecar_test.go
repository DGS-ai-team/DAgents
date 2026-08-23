package compression

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type sidecarStreamLLM struct {
	lastReq llm.ChatRequest
	content string
	usage   llm.Usage
}

func (m *sidecarStreamLLM) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	m.lastReq = req
	if handler.OnDelta != nil {
		handler.OnDelta(m.content)
	}
	if handler.OnUsage != nil {
		handler.OnUsage(m.usage)
	}
	return llm.ChatResult{Content: m.content}, nil
}

func (m *sidecarStreamLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *sidecarStreamLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func sampleSidecarTools() []tools.ToolDef {
	return []tools.ToolDef{{
		Type: "function",
		Function: tools.FunctionDef{
			Name:        "bash_run",
			Description: "run shell",
			Parameters:  map[string]any{"type": "object"},
		},
	}}
}

func sampleSidecarMessages() []llm.Message {
	return []llm.Message{
		{Role: "user", Content: "task"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "follow"},
		{Role: "assistant", Content: "tail"},
	}
}

func TestBuildSidecarChatRequest_includesToolsAndSystem(t *testing.T) {
	t.Parallel()

	toolsDef := sampleSidecarTools()
	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{SystemPrompt: "agent-system", Tools: toolsDef},
		Messages:      sampleSidecarMessages(),
		End:           2,
		SidecarAppend: sidecarAppendUserOnly,
	}, summaryUserPrompt)

	if req.SystemPrompt != "agent-system" {
		t.Fatalf("system = %q", req.SystemPrompt)
	}
	if len(req.Tools) != 1 || req.Tools[0].Function.Name != "bash_run" {
		t.Fatalf("tools = %+v", req.Tools)
	}
}

func TestBuildSidecarChatRequest_omitsLegacyDateMessages(t *testing.T) {
	req := BuildSidecarChatRequest(SidecarInput{
		Messages: []llm.Message{
			llm.UserMessage("当天日期为：20260719", llm.UserNameDate),
			llm.UserMessage("hello", llm.UserNameHuman),
		},
		End: 1,
	}, "summary")
	if len(req.Messages) != 2 || req.Messages[0].Content != "hello" || req.Messages[1].Content != "summary" {
		t.Fatalf("sidecar messages = %+v", req.Messages)
	}
}

func TestBuildSidecarChatRequest_prefixLengthAndLastUser(t *testing.T) {
	t.Parallel()

	msgs := sampleSidecarMessages()
	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{SystemPrompt: "sys", Tools: sampleSidecarTools()},
		Messages:      msgs,
		End:           2,
		SidecarAppend: sidecarAppendUserOnly,
	}, summaryUserPrompt)

	if len(req.Messages) != 4 { // [0:3] + user
		t.Fatalf("messages len = %d, want 4: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[2].Content != "follow" {
		t.Fatalf("prefix last = %+v", req.Messages[2])
	}
	last := req.Messages[len(req.Messages)-1]
	if last.Role != "user" || last.Content != summaryUserPrompt || last.Name != llm.UserNameCompressionSidecar {
		t.Fatalf("last message = %+v", last)
	}
}

func TestBuildSidecarChatRequest_case1AssistantAndUser(t *testing.T) {
	t.Parallel()

	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{SystemPrompt: "sys", Tools: sampleSidecarTools()},
		Messages:      sampleSidecarMessages(),
		End:           1,
		SidecarAppend: sidecarAppendAssistantAndUser,
	}, "custom summary prompt")

	if len(req.Messages) != 4 { // [0:2] + assistant + user
		t.Fatalf("len = %d", len(req.Messages))
	}
	if req.Messages[2].Role != "assistant" || req.Messages[2].Content != sidecarSyntheticAssistantContent {
		t.Fatalf("synthetic assistant = %+v", req.Messages[2])
	}
	if req.Messages[3].Role != "user" || req.Messages[3].Content != "custom summary prompt" || req.Messages[3].Name != llm.UserNameCompressionSidecar {
		t.Fatalf("summary user = %+v", req.Messages[3])
	}
}

func TestBuildSidecarChatRequest_defaultUserPrompt(t *testing.T) {
	t.Parallel()

	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{Tools: sampleSidecarTools()},
		Messages:      sampleSidecarMessages(),
		End:           0,
	}, "  ")
	last := req.Messages[len(req.Messages)-1]
	if last.Content != summaryUserPrompt {
		t.Fatalf("got %q", last.Content)
	}
}

func TestSummarize_collectsContentAndUsage(t *testing.T) {
	t.Parallel()

	client := &sidecarStreamLLM{
		content: "[母任务]\n[—]\n任务目标：t；阶段性总结论：c；修改过的文件和资源：无",
		usage: llm.Usage{
			PromptTokens:          100,
			CompletionTokens:      20,
			PromptCacheHitTokens:  80,
			PromptCacheMissTokens: 20,
		},
	}
	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{SystemPrompt: "sys", Tools: sampleSidecarTools()},
		Messages:      sampleSidecarMessages(),
		End:           1,
	}, summaryUserPrompt)

	content, usage, err := Summarize(context.Background(), client, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, "阶段性总结论") {
		t.Fatalf("content = %q", content)
	}
	if usage.PromptCacheHitTokens != 80 {
		t.Fatalf("usage = %+v", usage)
	}
	if client.lastReq.SystemPrompt != "sys" {
		t.Fatalf("stream chat system = %q", client.lastReq.SystemPrompt)
	}
}

func TestSummarize_failsOnEmptyContent(t *testing.T) {
	t.Parallel()

	client := &sidecarStreamLLM{content: "   "}
	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{Tools: sampleSidecarTools()},
		Messages:      sampleSidecarMessages(),
		End:           0,
	}, summaryUserPrompt)

	_, _, err := Summarize(context.Background(), client, req)
	if err == nil {
		t.Fatal("expected error for empty summary")
	}
}

func TestSummarize_usesResultContentWhenNoDeltas(t *testing.T) {
	t.Parallel()

	client := &noDeltaStreamLLM{content: "fallback body"}
	req := BuildSidecarChatRequest(SidecarInput{
		SidecarPrefix: SidecarPrefix{Tools: sampleSidecarTools()},
		Messages:      sampleSidecarMessages(),
		End:           0,
	}, summaryUserPrompt)

	content, _, err := Summarize(context.Background(), client, req)
	if err != nil || content != "fallback body" {
		t.Fatalf("err=%v content=%q", err, content)
	}
}

type noDeltaStreamLLM struct {
	content string
}

func (m *noDeltaStreamLLM) StreamChat(context.Context, llm.ChatRequest, llm.StreamHandler) (llm.ChatResult, error) {
	return llm.ChatResult{Content: m.content}, nil
}

func (m *noDeltaStreamLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (m *noDeltaStreamLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}
