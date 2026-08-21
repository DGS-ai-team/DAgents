package session

import (
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// 集成验证：等审批时 consumeLoop 不暂停，async_tool_result 仍会被消费；
// 若 history 为「auto 已写 tool、approval 未写 tool」，可能破坏 tool batch 合法性。
func TestAsyncToolResultDuringApprovalOpenBatchDoesNotViolateHistory(t *testing.T) {
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
		ID:   "call-sync-1",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"path":"a.txt"}`,
		},
	}

	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "bg + sync"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
			{
				ID: "call-bg-1", Type: "function",
				Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"sleep 9","run_in_background":true}`},
			},
			approvalCall,
		}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1 status=accepted"},
	}
	pending := &turn.PendingHITL{
		Items: []turn.PendingHITLItem{{ToolCall: approvalCall}},
	}
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, pending)

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-job-1",
		Status: "succeeded", ResultText: "done",
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

	rt.mu.Lock()
	msgs := append([]llm.Message(nil), rt.messages...)
	rt.mu.Unlock()
	pending = rt.pendingSnapshot()

	if pending == nil || len(pending.Items) != 1 {
		t.Fatalf("pending approval should be preserved, got %#v", pending)
	}
	if turnHistoryHasOpenToolBatchViolation(msgs) {
		t.Fatalf("async during approval produced illegal history:\n%s", turnFormatHistoryForTest(msgs))
	}
}

// turnHistoryHasOpenToolBatchViolation 检测 assistant tool_calls 后是否存在未闭合 batch。
func turnHistoryHasOpenToolBatchViolation(messages []llm.Message) bool {
outer:
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		pending := make(map[string]struct{}, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			if id := tc.ID; id != "" {
				pending[id] = struct{}{}
			}
		}
		for j := i + 1; j < len(messages); j++ {
			next := messages[j]
			switch next.Role {
			case "tool":
				delete(pending, next.ToolCallID)
			case "assistant":
				if len(pending) > 0 {
					return true
				}
				i = j - 1
				continue outer
			}
		}
	}
	return false
}

func turnFormatHistoryForTest(messages []llm.Message) string {
	var b strings.Builder
	for i, m := range messages {
		switch m.Role {
		case "assistant":
			names := make([]string, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				names = append(names, tc.Function.Name+"("+tc.ID+")")
			}
			if len(names) == 0 {
				b.WriteString(m.Content)
			} else {
				b.WriteString("assistant tool_calls=[")
				b.WriteString(strings.Join(names, ", "))
				b.WriteString("]")
			}
		default:
			b.WriteString(m.Role)
			if m.Role == "tool" {
				b.WriteString(" ")
				b.WriteString(m.ToolCallID)
			}
			b.WriteString(": ")
			if len(m.Content) > 60 {
				b.WriteString(m.Content[:60])
				b.WriteString("…")
			} else {
				b.WriteString(m.Content)
			}
		}
		b.WriteString("\n")
		_ = i
	}
	return b.String()
}
