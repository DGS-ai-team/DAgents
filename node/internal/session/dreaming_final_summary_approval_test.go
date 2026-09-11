package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// dreamingApprovalSummaryClient forces the first tool round to wait for ASK,
// then returns the final answer. It captures the actual provider requests so
// the approval continuation cannot accidentally bypass final-summary policy.
type dreamingApprovalSummaryClient struct {
	mu       sync.Mutex
	requests []llm.ChatRequest
}

func (c *dreamingApprovalSummaryClient) StreamChat(_ context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	call := len(c.requests)
	c.mu.Unlock()
	if h.OnUsage != nil {
		h.OnUsage(llm.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2})
	}
	if call == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "write-final", Type: "function", Function: llm.ToolCallFunction{Name: "write_file", Arguments: `{"path":"handbook/approved.md","content":"approved","call_purpose":"record"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "已完成可执行的整理；受本轮工具轮次限制，其余未执行。", FinishReason: "stop"}, nil
}

func (*dreamingApprovalSummaryClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}

func (*dreamingApprovalSummaryClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestDreamingApprovalContinuationFinalSummaryPromptIsToolFree(t *testing.T) {
	client := &dreamingApprovalSummaryClient{}
	mgr, _, _, _, sessionID, cleanup := dreamingResumeFixture(t, client, policy.ModeAlways)
	defer cleanup()
	leaseCtx, release, ok, err := mgr.TryAcquireMaintenanceContext(context.Background(), "agent-1")
	if err != nil || !ok {
		t.Fatalf("maintenance lease: ok=%v err=%v", ok, err)
	}
	result, runErr := mgr.RunDreaming(leaseCtx, sessionID, "整理经验", 1)
	release()
	if runErr == nil || result.Content != "" {
		t.Fatalf("first request should await ASK: result=%+v err=%v", result, runErr)
	}
	waitDreamingPending(t, mgr, sessionID, "write_file", "write-final")
	if _, err := mgr.EnqueueMessage(context.Background(), sessionID, "resume", "", nil, map[string]any{
		"type": "selection", "approved": []string{"write-final"}, "rejected": []string{},
	}, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if attempt, found, _ := mgr.GetDreamingAttempt(sessionID); found && attempt.State == DreamingAttemptCompleted {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	attempt, found, err := mgr.GetDreamingAttempt(sessionID)
	if err != nil || !found || attempt.State != DreamingAttemptCompleted {
		client.mu.Lock()
		requestCount := len(client.requests)
		client.mu.Unlock()
		t.Fatalf("approval continuation did not complete: found=%v err=%v attempt=%+v requests=%d", found, err, attempt, requestCount)
	}
	client.clientRequests(t, mgr, sessionID)
}

func (c *dreamingApprovalSummaryClient) clientRequests(t *testing.T, mgr *Manager, sessionID string) {
	t.Helper()
	c.mu.Lock()
	requests := append([]llm.ChatRequest(nil), c.requests...)
	c.mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("provider requests=%d, want initial ASK plus final summary", len(requests))
	}
	final := requests[1]
	if len(final.Tools) != 0 {
		t.Fatalf("approval continuation final request exposed tools: %d", len(final.Tools))
	}
	for _, phrase := range []string{"工具轮次已达到上限", "不要发起或请求任何工具调用", "不要输出模拟的 <tool_call>", "如实说明已经完成的工作、未完成的工作"} {
		if !strings.Contains(final.SystemPrompt, phrase) {
			t.Fatalf("approval continuation prompt missing %q: %q", phrase, final.SystemPrompt)
		}
	}
	assertFinalSummaryTailAndNoDurableLeak(t, mgr, sessionID, final)
}
