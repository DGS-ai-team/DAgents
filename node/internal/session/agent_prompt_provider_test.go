package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type promptProviderCaptureClient struct {
	mu        sync.Mutex
	requests  []llm.ChatRequest
	seen      chan struct{}
	toolBatch bool
	release   chan struct{}
}

func TestAgentPromptProviderFailureSkipsModel(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "probe.txt"), []byte("probe"), 0644); err != nil {
		t.Fatal(err)
	}
	client := &promptProviderCaptureClient{seen: make(chan struct{}, 1)}
	providerCalled := make(chan struct{}, 1)
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{
		WorkspaceRoot: root,
		AgentPromptProvider: func(context.Context, string) (turn.AgentPromptSnapshot, error) {
			providerCalled <- struct{}{}
			return turn.AgentPromptSnapshot{}, errors.New("prompt unavailable")
		},
	}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "触发", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-providerCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("prompt provider was not called")
	}
	select {
	case <-client.seen:
		t.Fatal("model was called after prompt provider failure")
	case <-time.After(500 * time.Millisecond):
	}
}

func (c *promptProviderCaptureClient) StreamChat(_ context.Context, req llm.ChatRequest, handler llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	c.mu.Lock()
	call := len(c.requests)
	batch := c.toolBatch
	c.mu.Unlock()
	select {
	case c.seen <- struct{}{}:
	default:
	}
	if batch && call == 1 {
		if c.release != nil {
			<-c.release
		}
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "prompt-freeze-read", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"probe.txt"}`}}}, FinishReason: "tool_calls"}, nil
	}
	if handler.OnDelta != nil {
		handler.OnDelta("完成")
	}
	return llm.ChatResult{Content: "完成", FinishReason: "stop"}, nil
}

func (*promptProviderCaptureClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*promptProviderCaptureClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func (c *promptProviderCaptureClient) promptAt(i int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i >= len(c.requests) {
		return ""
	}
	return c.requests[i].SystemPrompt
}

func TestAgentPromptProviderFreezesPerTurnAndRefreshesNextTurn(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "probe.txt"), []byte("probe"), 0644); err != nil {
		t.Fatal(err)
	}
	client := &promptProviderCaptureClient{seen: make(chan struct{}, 4), release: make(chan struct{}), toolBatch: true}
	var mu sync.Mutex
	current := turn.AgentPromptSnapshot{Responsibilities: "职责一", Experience: "经验一"}
	provider := func(_ context.Context, agentID string) (turn.AgentPromptSnapshot, error) {
		if agentID != "agent-1" {
			t.Fatalf("provider received wrong agent: %q", agentID)
		}
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}
	pol := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, pol, nil, TurnOptions{
		WorkspaceRoot:       root,
		SkillsEnabled:       false,
		AgentPromptProvider: provider,
	}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	enqueue := func(text string) {
		if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", text, nil, nil, ""); err != nil {
			t.Fatal(err)
		}
		select {
		case <-client.seen:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for model request")
		}
	}
	enqueue("第一轮")
	first := client.promptAt(0)
	if !strings.Contains(first, "## Agent 职责") || !strings.Contains(first, "职责一") || !strings.Contains(first, "## Agent 经验") || !strings.Contains(first, "经验一") {
		t.Fatalf("first system prompt missing agent-owned sections: %q", first)
	}
	mu.Lock()
	current = turn.AgentPromptSnapshot{Responsibilities: "职责二", Experience: "经验二"}
	mu.Unlock()
	close(client.release)
	select {
	case <-client.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for tool continuation")
	}
	second := client.promptAt(1)
	if !strings.Contains(second, "职责一") || !strings.Contains(second, "经验一") || strings.Contains(second, "职责二") {
		t.Fatalf("same turn did not freeze agent prompt: %q", second)
	}
	if err := mgr.EnqueueTriggerMessage(s.ID, "prompt-refresh-trigger", "第二轮触发"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for trigger model request")
	}
	third := client.promptAt(2)
	if !strings.Contains(third, "职责二") || !strings.Contains(third, "经验二") || strings.Contains(third, "职责一") || strings.Contains(third, "经验一") {
		t.Fatalf("next turn did not refresh agent prompt: %q", third)
	}
}

func TestAgentPromptProviderTodoIsRequestOnlyAndRefreshesForTrigger(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	client := &promptProviderCaptureClient{seen: make(chan struct{}, 4)}
	var mu sync.Mutex
	current := turn.AgentPromptSnapshot{Responsibilities: "职责", Todo: "- [pending] 初始待办"}
	provider := func(_ context.Context, agentID string) (turn.AgentPromptSnapshot, error) {
		if agentID != "agent-1" {
			t.Fatalf("provider received wrong agent: %q", agentID)
		}
		mu.Lock()
		defer mu.Unlock()
		return current, nil
	}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{
		WorkspaceRoot: root, SkillsEnabled: false, AgentPromptProvider: provider,
	}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.CreateWithOptions("todo-session", TurnOptions{AgentPromptProvider: provider, WorkspaceRoot: root}, reg, policy.NewDefaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	view, err := mgr.GetContextView(s.ID)
	if err != nil || !strings.Contains(view.SystemPrompt, "职责") {
		t.Fatalf("idle prompt missing current responsibilities: view=%+v err=%v", view, err)
	}
	mu.Lock()
	current = turn.AgentPromptSnapshot{Responsibilities: "职责更新", Todo: "- [pending] 初始待办"}
	mu.Unlock()
	view, err = mgr.GetContextView(s.ID)
	if err != nil || !strings.Contains(view.SystemPrompt, "职责更新") || strings.Contains(view.SystemPrompt, "\n\n职责\n\n") {
		t.Fatalf("idle prompt did not refresh provider: view=%+v err=%v", view, err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "查看待办", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for initial request")
	}
	first := client.promptAt(0)
	if strings.Contains(first, "初始待办") {
		t.Fatalf("todo leaked into system prompt: %q", first)
	}
	if _, messages, err := mgr.ContextSummary(s.ID); err != nil {
		t.Fatal(err)
	} else {
		for _, message := range messages {
			if strings.Contains(llm.MessageTextSummary(message), "初始待办") {
				t.Fatalf("todo was persisted in session history: %+v", message)
			}
		}
	}
	mu.Lock()
	current = turn.AgentPromptSnapshot{Responsibilities: "职责", Todo: "- [pending] 更新待办"}
	mu.Unlock()
	if err := mgr.EnqueueTriggerMessage(s.ID, "todo-refresh", "检查最新待办"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for trigger request")
	}
	request := client.requests[1]
	if strings.Contains(request.SystemPrompt, "更新待办") {
		t.Fatalf("todo leaked into refreshed system prompt: %q", request.SystemPrompt)
	}
	found := false
	for _, message := range request.Messages {
		if strings.Contains(llm.MessageTextSummary(message), "更新待办") {
			found = true
		}
	}
	if !found {
		t.Fatalf("trigger request did not contain refreshed todo context: %+v", request.Messages)
	}
}

func TestAgentPromptProviderIdleFailureIsDiagnosable(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), &promptProviderCaptureClient{seen: make(chan struct{}, 1)}, reg, policy.NewDefaultEngine(), nil, TurnOptions{
		WorkspaceRoot: root,
		AgentPromptProvider: func(context.Context, string) (turn.AgentPromptSnapshot, error) {
			return turn.AgentPromptSnapshot{}, errors.New("todo store unavailable")
		},
	}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	view, err := mgr.GetContextView(s.ID)
	if err != nil || !strings.Contains(view.SystemPrompt, "Agent prompt unavailable") {
		t.Fatalf("idle provider failure was not diagnosable: view=%+v err=%v", view, err)
	}
}
