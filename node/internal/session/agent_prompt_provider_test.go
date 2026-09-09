package session

import (
	"context"
	"errors"
	"fmt"
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

type triggerRoundCaptureClient struct {
	mu       sync.Mutex
	calls    int
	requests []llm.ChatRequest
	seen     chan int
	toolAt   map[int]bool
}

type inputPriorityCaptureClient struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
	started  chan struct{}
	release  chan struct{}
}

type invalidAutoDeliveryTracker struct{}

func (invalidAutoDeliveryTracker) HasPendingDelivery(string) bool             { return false }
func (invalidAutoDeliveryTracker) MarkPendingDelivery(string)                 {}
func (invalidAutoDeliveryTracker) ClearPendingDelivery(string)                {}
func (invalidAutoDeliveryTracker) ClearPendingDeliveryIfMatch(string, string) {}
func (invalidAutoDeliveryTracker) IsPendingDelivery(string, string) bool      { return false }

func (c *inputPriorityCaptureClient) StreamChat(_ context.Context, req llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	call := len(c.requests)
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	if call == 0 {
		close(c.started)
		<-c.release
	}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}
func (*inputPriorityCaptureClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*inputPriorityCaptureClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func requestContains(req llm.ChatRequest, text string) bool {
	for _, message := range req.Messages {
		if strings.Contains(message.Content, text) {
			return true
		}
	}
	return false
}

func TestRuntimeIdleUserInputPrecedesQueuedSystemAuto(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	client := &inputPriorityCaptureClient{started: make(chan struct{}), release: make(chan struct{})}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{
		WorkspaceRoot: root,
		TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			return 1, agentID == "agent-1" && triggerID == "auto-default:agent-1" && deliveryID == "auto-delivery", nil
		},
	}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "首个用户", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first user model call did not start")
	}
	if err := mgr.EnqueueAutoTriggerMessage(s.ID, "auto-default:agent-1", "系统自动", "auto-delivery"); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "第二个用户", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	close(client.release)
	deadline := time.After(3 * time.Second)
	for {
		client.mu.Lock()
		count := len(client.requests)
		requests := append([]llm.ChatRequest(nil), client.requests...)
		client.mu.Unlock()
		if count >= 3 {
			if !requestContains(requests[1], "第二个用户") || !requestContains(requests[2], "系统自动") {
				t.Fatalf("idle order was not user before system auto: requests=%d", count)
			}
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for queued inputs, calls=%d", count)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestSystemAutoWithInvalidDeliveryIsDroppedBeforeModel(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	client := &inputPriorityCaptureClient{started: make(chan struct{}), release: make(chan struct{})}
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{WorkspaceRoot: root}, logx.Discard())
	defer mgr.Stop()
	mgr.SetTriggerDeliveryTracker(invalidAutoDeliveryTracker{})
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueAutoTriggerMessage(s.ID, "auto-default:agent-1", "系统自动", "stale-delivery"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.started:
		t.Fatal("invalid system auto delivery reached model")
	case <-time.After(500 * time.Millisecond):
	}
}

func (c *triggerRoundCaptureClient) StreamChat(_ context.Context, req llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.calls++
	n := c.calls
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	c.seen <- n
	if (c.toolAt == nil && (n == 1 || n == 3)) || c.toolAt[n] {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{
			{ID: fmt.Sprintf("round-%d-a", n), Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"probe.txt"}`}},
			{ID: fmt.Sprintf("round-%d-b", n), Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"probe.txt"}`}},
		}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "done", FinishReason: "stop"}, nil
}

func TestTrustedTriggerRoundBudgetReplacesLegacyAutoLimits(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "probe.txt"), []byte("probe"), 0644); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	client := &triggerRoundCaptureClient{seen: make(chan int, 4), toolAt: map[int]bool{1: true, 2: true}}
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg,
		policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever}}), nil, TurnOptions{
			WorkspaceRoot: root,
			Budget:        turn.TurnBudget{MaxSteps: 1, MaxToolCalls: 1, MaxTotalTokens: 1},
			TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
				return 2, agentID == "agent-1" && triggerID == "trusted-default" && deliveryID == "delivery-old-limits", nil
			},
		}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueTriggerMessage(s.ID, "trusted-default", "激活", "delivery-old-limits"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		select {
		case <-client.seen:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for capped trigger completion")
		}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.requests) != 3 {
		t.Fatalf("trusted trigger stopped under legacy budget: %d model requests", len(client.requests))
	}
	if len(client.requests[2].Tools) != 0 {
		t.Fatal("final summary unexpectedly exposed tools")
	}
}
func (*triggerRoundCaptureClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*triggerRoundCaptureClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
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

func TestTrustedTriggerToolRoundLimitLeavesSummaryAndDoesNotAffectChat(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "probe.txt"), []byte("probe"), 0644); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	pol := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"read_file": policy.ModeNever}})
	client := &triggerRoundCaptureClient{seen: make(chan int, 8)}
	var providerMu sync.Mutex
	triggerLimit := 1
	providerLimits := make([]int, 0, 2)
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, pol, nil, TurnOptions{
		WorkspaceRoot: root,
		TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			if agentID == "agent-1" && triggerID == "trusted-default" && deliveryID == "delivery-1" {
				providerMu.Lock()
				defer providerMu.Unlock()
				providerLimits = append(providerLimits, triggerLimit)
				return triggerLimit, true, nil
			}
			if agentID == "agent-1" && triggerID == "trusted-default-2" && deliveryID == "delivery-2" {
				providerMu.Lock()
				defer providerMu.Unlock()
				providerLimits = append(providerLimits, triggerLimit)
				return triggerLimit, true, nil
			}
			return 0, false, nil
		},
	}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueTriggerMessage(s.ID, "trusted-default", "激活", "delivery-1"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-client.seen:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for trusted trigger request")
		}
	}
	client.mu.Lock()
	if len(client.requests) < 2 || len(client.requests[1].Tools) != 0 {
		got := 0
		if len(client.requests) >= 2 {
			got = len(client.requests[1].Tools)
		}
		client.mu.Unlock()
		t.Fatalf("reserved summary still exposed tools: %d", got)
	}
	toolResults := 0
	for _, message := range client.requests[1].Messages {
		if message.Role == "tool" {
			toolResults++
		}
	}
	if toolResults != 2 {
		client.mu.Unlock()
		t.Fatalf("first tool batch results=%d, want 2", toolResults)
	}
	client.mu.Unlock()
	if _, err := mgr.EnqueueMessage(context.Background(), s.ID, "message", "普通聊天", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-client.seen:
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for ordinary chat request")
		}
	}
	client.mu.Lock()
	chatTools := len(client.requests[2].Tools)
	client.mu.Unlock()
	if chatTools == 0 {
		t.Fatal("ordinary chat unexpectedly inherited trigger round cap")
	}
	providerMu.Lock()
	triggerLimit = 2
	providerMu.Unlock()
	if err := mgr.EnqueueTriggerMessage(s.ID, "trusted-default-2", "再次激活", "delivery-2"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.seen:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for second trusted trigger request")
	}
	providerMu.Lock()
	gotLimits := append([]int(nil), providerLimits...)
	providerMu.Unlock()
	if len(gotLimits) != 2 || gotLimits[0] != 1 || gotLimits[1] != 2 {
		t.Fatalf("trigger profile was not refreshed per activation: %v", gotLimits)
	}
}

func TestTrustedTriggerToolRoundProviderErrorSkipsModel(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	client := &triggerRoundCaptureClient{seen: make(chan int, 1)}
	providerCalled := make(chan struct{}, 1)
	mgr := NewManager("agent-1", stream.NewHub(8, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{
		WorkspaceRoot: root,
		TriggerToolRoundProvider: func(context.Context, string, string, string) (int, bool, error) {
			providerCalled <- struct{}{}
			return 0, false, errors.New("profile unavailable")
		},
	}, logx.Discard())
	defer mgr.Stop()
	s, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueTriggerMessage(s.ID, "trusted-default", "激活", "delivery-error"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-providerCalled:
	case <-time.After(3 * time.Second):
		t.Fatal("trigger round provider was not called")
	}
	select {
	case n := <-client.seen:
		t.Fatalf("model call %d occurred after trigger budget provider failure", n)
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
