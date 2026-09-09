package session

import (
	"context"
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
