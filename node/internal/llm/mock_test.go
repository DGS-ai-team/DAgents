package llm

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestMockClientEchoPrefix(t *testing.T) {
	m := &MockClient{Prefix: "echo: "}
	var deltas []string
	res, err := m.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	}, StreamHandler{
		OnDelta: func(s string) { deltas = append(deltas, s) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "echo: hello" {
		t.Fatalf("content = %q", res.Content)
	}
	if strings.Join(deltas, "") != "echo: hello" {
		t.Fatalf("deltas = %v", deltas)
	}
}

func TestMockClientFixedReply(t *testing.T) {
	m := &MockClient{FixedReply: "fixed answer"}
	res, err := m.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "ignored"}},
	}, StreamHandler{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "fixed answer" {
		t.Fatalf("content = %q", res.Content)
	}
}

func TestMockClientEnableToolsTwoTurn(t *testing.T) {
	m := &MockClient{EnableTools: true}
	tools := []tools.ToolDef{{Function: tools.FunctionDef{Name: "read_file"}}}

	res1, err := m.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "read"}},
		Tools:    tools,
	}, StreamHandler{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.ToolCalls) != 1 || res1.ToolCalls[0].Function.Name != "read_file" {
		t.Fatalf("turn1 tool calls = %+v", res1.ToolCalls)
	}

	res2, err := m.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "read"},
			{Role: "assistant", ToolCalls: res1.ToolCalls},
			{Role: "tool", Content: "file body", ToolCallID: "call-mock-1"},
		},
		Tools: tools,
	}, StreamHandler{})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Content != "已读取文件" {
		t.Fatalf("turn2 content = %q", res2.Content)
	}
}

func TestMockClientRespectsContextCancel(t *testing.T) {
	m := &MockClient{FixedReply: strings.Repeat("x", 64)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := m.StreamChat(ctx, ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	}, StreamHandler{
		OnDelta: func(string) {},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestChildAgentFlowMockParentThenChild(t *testing.T) {
	flow := &ChildAgentFlowMock{FinalReply: "parent done"}
	parentTools := []tools.ToolDef{{Function: tools.FunctionDef{Name: "create_temporary_agent"}}}

	res1, err := flow.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "delegate"}},
		Tools:    parentTools,
	}, StreamHandler{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res1.ToolCalls) != 1 || res1.ToolCalls[0].Function.Name != "create_temporary_agent" {
		t.Fatalf("expected create tool, got %+v", res1.ToolCalls)
	}

	resChild, err := flow.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{{Role: "user", Content: "child task"}},
		Tools:    nil,
	}, StreamHandler{})
	if err != nil {
		t.Fatal(err)
	}
	if resChild.Content != "child task" {
		t.Fatalf("child echo = %q", resChild.Content)
	}

	res2, err := flow.StreamChat(context.Background(), ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "delegate"},
			{Role: "assistant", ToolCalls: res1.ToolCalls},
			{Role: "tool", Content: "child ok", ToolCallID: "call-create-child-1"},
		},
		Tools: parentTools,
	}, StreamHandler{})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Content != "parent done" {
		t.Fatalf("parent final = %q", res2.Content)
	}
}
