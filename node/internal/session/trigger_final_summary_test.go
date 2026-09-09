package session

import (
	"context"
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
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type triggerApprovalSummaryClient struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
}

type autoIdleClient struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
}

type readThenAutoIdleClient struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
}

func (c *readThenAutoIdleClient) StreamChat(_ context.Context, req llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	call := len(c.requests)
	c.mu.Unlock()
	if call == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "read-idle", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"probe.txt"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "idle-after-read", Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}}, FinishReason: "tool_calls"}, nil
}

func (*readThenAutoIdleClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*readThenAutoIdleClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func (c *autoIdleClient) StreamChat(_ context.Context, req llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "idle", Type: "function", Function: llm.ToolCallFunction{Name: "auto_idle", Arguments: `{}`}}}, FinishReason: "tool_calls"}, nil
}
func (*autoIdleClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*autoIdleClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func (c *triggerApprovalSummaryClient) StreamChat(_ context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	call := len(c.requests)
	c.mu.Unlock()
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	if call == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "trigger-write", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"trigger-result.md","content":"approved","call_purpose":"record"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "已完成；本轮工具轮次已用尽，其余工作未执行。", FinishReason: "stop"}, nil
}

func (*triggerApprovalSummaryClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*triggerApprovalSummaryClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestTriggerOriginApprovalFinalSummaryPromptIsToolFree(t *testing.T) {
	workspace := t.TempDir()
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	client := &triggerApprovalSummaryClient{}
	pol := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"write_file": policy.ModeAlways}})
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), client, reg, pol, nil, TurnOptions{
		AutoAgent:     true,
		WorkspaceRoot: workspace,
		TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			return 1, agentID == "agent-1" && triggerID == "default-trigger" && deliveryID == "delivery-1", nil
		},
	}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueTriggerMessage(sess.ID, "default-trigger", "执行一次整理", "delivery-1"); err != nil {
		t.Fatal(err)
	}
	waitForTriggerApproval(t, mgr, sess.ID, "trigger-write")
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, "resume", "", nil, map[string]any{
		"type": "selection", "approved": []string{"trigger-write"}, "rejected": []string{},
	}, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client.mu.Lock()
		count := len(client.requests)
		client.mu.Unlock()
		if count >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	client.mu.Lock()
	requests := append([]llm.ChatRequest(nil), client.requests...)
	client.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("trigger requests=%d, want tool request plus final summary", len(requests))
	}
	final := requests[1]
	if len(final.Tools) != 0 {
		t.Fatalf("trigger final summary exposed tools: %d", len(final.Tools))
	}
	for _, phrase := range []string{"工具轮次已达到上限", "不要发起或请求任何工具调用", "不要输出模拟的 <tool_call>", "如实说明已经完成的工作、未完成的工作"} {
		if !strings.Contains(final.SystemPrompt, phrase) {
			t.Fatalf("trigger final summary prompt missing %q: %q", phrase, final.SystemPrompt)
		}
	}
	assertFinalSummaryTailAndNoDurableLeak(t, mgr, sess.ID, final)
}

func TestTrustedSystemAutoIdlePublishesNoWorkWithoutNotificationBump(t *testing.T) {
	hub := stream.NewHub(16, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	client := &autoIdleClient{}
	mgr := NewManager("agent-1", hub, client, reg, policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{"auto_idle": policy.ModeNever}}), nil, TurnOptions{
		AutoAgent: true,
		TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			return 1, agentID == "agent-1" && triggerID == "auto_idle" && deliveryID == "idle-1", nil
		},
	}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	events := hub.Subscribe(0)
	defer hub.Unsubscribe(events)
	if err := mgr.EnqueueAutoTriggerMessage(sess.ID, "auto_idle", "检查是否有工作", "idle-1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	seenTypes := []string{}
	for {
		select {
		case ev := <-events:
			seenTypes = append(seenTypes, ev.Type)
			if ev.Type != "turn_finished" {
				continue
			}
			if noWork, _ := ev.Data["no_work"].(bool); !noWork {
				t.Fatalf("turn_finished missing trusted no_work: %#v", ev.Data)
			}
			if ShouldBumpNotifySeq(ev) {
				t.Fatal("trusted no_work advanced notification cursor")
			}
			client.mu.Lock()
			requests := append([]llm.ChatRequest(nil), client.requests...)
			client.mu.Unlock()
			if len(requests) != 1 {
				t.Fatalf("provider requests=%d, want one auto_idle request", len(requests))
			}
			found := false
			for _, def := range requests[0].Tools {
				if def.Function.Name == "auto_idle" {
					found = true
				}
			}
			if !found {
				t.Fatal("trusted system-auto request did not expose auto_idle")
			}
			return
		case <-deadline:
			mgr.mu.RLock()
			rt := mgr.sessions[sess.ID]
			mgr.mu.RUnlock()
			state := "missing"
			if rt != nil {
				state = string(rt.turnState())
			}
			t.Fatalf("timed out waiting for no_work turn_finished: events=%v state=%s", seenTypes, state)
		}
	}
}

func TestTrustedSystemAutoIdleSurvivesReadOnlyContinuation(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "probe.txt"), []byte("probe"), 0o644); err != nil {
		t.Fatal(err)
	}
	hub := stream.NewHub(16, logx.Discard())
	client := &readThenAutoIdleClient{}
	reg, err := tools.NewRegistry(workspace, 30)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager("agent-1", hub, client, reg, policy.NewDefaultEngine(), nil, TurnOptions{
		AutoAgent: true, WorkspaceRoot: workspace,
		TriggerToolRoundProvider: func(_ context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			return 2, agentID == "agent-1" && triggerID == "auto-idle" && deliveryID == "read-1", nil
		},
	}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	events := hub.Subscribe(0)
	defer hub.Unsubscribe(events)
	if err := mgr.EnqueueAutoTriggerMessage(sess.ID, "auto-idle", "检查", "read-1"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type != "turn_finished" {
				continue
			}
			if noWork, _ := ev.Data["no_work"].(bool); !noWork {
				t.Fatalf("continuation missing no_work: %#v", ev.Data)
			}
			client.mu.Lock()
			requests := append([]llm.ChatRequest(nil), client.requests...)
			client.mu.Unlock()
			if len(requests) != 2 {
				t.Fatalf("read/idle requests=%d, want 2", len(requests))
			}
			for i, req := range requests {
				found := false
				for _, def := range req.Tools {
					if def.Function.Name == "auto_idle" {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("request %d did not expose auto_idle", i)
				}
			}
			return
		case <-deadline:
			t.Fatal("timed out waiting for read→auto_idle no_work")
		}
	}
}

func assertFinalSummaryTailAndNoDurableLeak(t *testing.T, mgr *Manager, sessionID string, final llm.ChatRequest) {
	t.Helper()
	if len(final.Messages) == 0 || !strings.Contains(final.Messages[len(final.Messages)-1].Content, "工具已在本轮收尾请求中禁用") {
		t.Fatalf("final request missing request-only tail: %+v", final.Messages)
	}
	rt := mgr.getRuntime(sessionID)
	if rt == nil {
		t.Fatal("runtime missing")
	}
	for _, message := range rt.activeMessagesSnapshot() {
		if strings.Contains(message.Content, "工具已在本轮收尾请求中禁用") {
			t.Fatal("request-only final-summary tail leaked into durable history")
		}
	}
}

func waitForTriggerApproval(t *testing.T, mgr *Manager, sessionID, callID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		rt := mgr.getRuntime(sessionID)
		if rt != nil {
			pending := rt.pendingSnapshot()
			if pending != nil && len(pending.Items) == 1 && pending.Items[0].ToolCall.ID == callID {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for trigger approval")
}
