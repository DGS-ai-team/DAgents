package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// issue #25：async 工具回灌不得清掉无关 pending HITL，否则 resume 会 409。
func TestAsyncToolResultPreservesPendingHITL_issue25(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if rt == nil {
		t.Fatal("runtime missing")
	}

	approvalCall := llm.ToolCall{
		ID:   "call-approve-1",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "bash_run",
			Arguments: `{"command":"echo hi","call_purpose":"test"}`,
		},
	}

	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "run background then approve"},
		{Role: "assistant", Content: "ok", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"sleep 1","run_in_background":true}`},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: `{"job_id":"job-old","status":"accepted"}`},
		{Role: "user", Content: "now run sync"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{approvalCall}},
	}
	rt.pending = &turn.PendingHITL{
		Kind:      turn.HITLApproval,
		ToolCalls: []llm.ToolCall{approvalCall},
	}
	rt.toolLoopCount = 1
	rt.mu.Unlock()

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID:      "job-old",
		ToolName:   "bash_run",
		ToolCallID: "async-job-old",
		Status:     "failed",
		ErrorText:  "exit 1",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	for {
		if rt.queue.Len() == 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timeout waiting for async_tool_result; queue depth=%d", rt.queue.Len())
		case <-time.After(10 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond)

	rt.mu.Lock()
	pending := rt.pending
	loopCount := rt.toolLoopCount
	msgCount := len(rt.messages)
	rt.mu.Unlock()

	if pending == nil || pending.Kind != turn.HITLApproval {
		t.Fatalf("pending HITL should be preserved, got %#v", pending)
	}
	if loopCount != 1 {
		t.Fatalf("toolLoopCount = %d, want 1", loopCount)
	}
	if msgCount < 7 {
		t.Fatalf("async callback should append history, got %d messages", msgCount)
	}

	resume := map[string]any{
		"type":         "approval",
		"tool_call_id": approvalCall.ID,
		"decision":     "approve",
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, "resume", "", resume); err != nil {
		t.Fatalf("resume enqueue should succeed while pending preserved: %v", err)
	}
}
