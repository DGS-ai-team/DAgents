package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestSideEffectProduceAsyncDoesNotMutateHistoryDuringHITL(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)

	approvalCall := llm.ToolCall{
		ID: "call-sync-1", Type: "function",
		Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`},
	}
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "bg"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
			{ID: "call-bg-1", Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"run_in_background":true}`}},
			approvalCall,
		}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1"},
	}
	rt.pending = &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: approvalCall}}}
	rt.mu.Unlock()

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "succeeded", ResultText: "done",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.messages) != 3 {
		t.Fatalf("history len = %d, want 3", len(rt.messages))
	}
	if !rt.sideEffects.HasReady() {
		t.Fatal("expected buffered side effect")
	}
}

func TestSideEffectContinueAppliesExternalOnEmptyHistory(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("sess-empty-bridge")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)

	if _, err := mgr.EnqueueA2AInboxMessage(context.Background(), sess.ID, "inbox hello"); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 5*time.Second)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if len(rt.messages) < 1 {
		t.Fatalf("history len = %d, want bridge user after continue", len(rt.messages))
	}
	foundInbox := false
	for _, m := range rt.messages {
		if m.Role == "user" && strings.Contains(m.Content, "inbox hello") {
			foundInbox = true
			break
		}
	}
	if !foundInbox {
		t.Fatalf("messages = %+v", rt.messages)
	}
}

func TestSideEffectContinueDedupOnTaskComplete(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "done"},
	}
	rt.mu.Unlock()

	if err := mgr.EnqueueTriggerMessage(sess.ID, "trig-1", "first"); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueTriggerMessage(sess.ID, "trig-2", "second"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if sideEffectContinueDepth(rt) > 1 {
		t.Fatalf("continue should be deduped, depth=%d", sideEffectContinueDepth(rt))
	}
	waitQueueDrain(t, rt, 5*time.Second)
	if rt.sideEffects.HasReady() {
		t.Fatal("both triggers should be applied after single continue")
	}
}

func TestSideEffectFIFOApplyOrder(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "bg"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"run_in_background":true}`},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1"},
	}
	rt.mu.Unlock()

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "succeeded", ResultText: "first",
	}); err != nil {
		t.Fatal(err)
	}
	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-2", ToolName: "bash_run", ToolCallID: "async-2", Status: "succeeded", ResultText: "second",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)
	if rt.sideEffects.Len() != 2 {
		t.Fatalf("buffer len = %d, want 2", rt.sideEffects.Len())
	}
	_ = mgr.CancelTurn(sess.ID)
	waitQueueDrain(t, rt, 8*time.Second)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	var mergedContent string
	for _, m := range rt.messages {
		if strings.Contains(m.Content, "callbacks") {
			mergedContent = m.Content
			break
		}
	}
	if mergedContent == "" {
		// 未合并时仍须按 FIFO 出现在独立 tool 消息中
		idx1, idx2 := -1, -1
		for i, m := range rt.messages {
			if m.Role == "tool" && strings.Contains(m.Content, "job_id：job-1") {
				idx1 = i
			}
			if m.Role == "tool" && strings.Contains(m.Content, "job_id：job-2") {
				idx2 = i
			}
		}
		if idx1 < 0 || idx2 < 0 || idx1 > idx2 {
			t.Fatalf("FIFO violated in separate tool messages: idx1=%d idx2=%d", idx1, idx2)
		}
		return
	}
	pos1 := strings.Index(mergedContent, "job-1")
	pos2 := strings.Index(mergedContent, "job-2")
	if pos1 < 0 || pos2 < 0 || pos1 > pos2 {
		t.Fatalf("FIFO violated in merged callbacks: %q", mergedContent)
	}
}

func waitQueueDrain(t *testing.T, rt *runtime, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		if rt.queue.Len() == 0 {
			time.Sleep(50 * time.Millisecond)
			if rt.queue.Len() == 0 {
				return
			}
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for queue drain; depth=%d", rt.queue.Len())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
