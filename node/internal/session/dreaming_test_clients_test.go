package session

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// These clients exercise the dreaming lifecycle; they are intentionally kept
// separate from the retired handbook maintenance execution tests.
type handbookRoundClient struct {
	calls    int
	requests []llm.ChatRequest
}

func (c *handbookRoundClient) StreamChat(_ context.Context, request llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	c.requests = append(c.requests, request)
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4})
	}
	if c.calls == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "read", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"handbook/guide.md"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}

func (*handbookRoundClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*handbookRoundClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

type handbookBlockingClient struct{ started chan struct{} }

func (c *handbookBlockingClient) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	close(c.started)
	<-ctx.Done()
	return llm.ChatResult{}, ctx.Err()
}

func (*handbookBlockingClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*handbookBlockingClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}

type handbookEditClient struct {
	calls         int
	noUsageSecond bool
}

func (c *handbookEditClient) StreamChat(_ context.Context, _ llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls++
	if h.OnUsage != nil && !(c.noUsageSecond && c.calls == 2) {
		h.OnUsage(llm.Usage{PromptTokens: 2, CompletionTokens: 2, TotalTokens: 4})
	}
	var tc llm.ToolCall
	switch c.calls {
	case 1:
		tc = llm.ToolCall{ID: "w", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/a/b/c.md","content":"old","call_purpose":"maintain"}`}}
	case 2:
		tc = llm.ToolCall{ID: "r", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"handbook/a/b/c.md","call_purpose":"maintain"}`}}
	case 3:
		tc = llm.ToolCall{ID: "s", Type: "function", Function: llm.ToolCallFunction{Name: "search_replace", Arguments: `{"path":"handbook/a/b/c.md","old_string":"old","new_string":"new","call_purpose":"maintain"}`}}
	default:
		return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
	}
	return llm.ChatResult{ToolCalls: []llm.ToolCall{tc}, FinishReason: "tool_calls"}, nil
}

func (*handbookEditClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*handbookEditClient) NormalizeAssistant(e []llm.Message, m llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(e, m)
}
