package turn

import (
	"context"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
)

// historyHasOpenToolBatchViolation 检测是否存在「assistant(tool_calls) 尚未全部回应就插入下一条 assistant」的非法序列。
func historyHasOpenToolBatchViolation(messages []llm.Message) bool {
	for i := 0; i < len(messages); i++ {
		msg := messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}
		pending := make(map[string]struct{}, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			if id := strings.TrimSpace(tc.ID); id != "" {
				pending[id] = struct{}{}
			}
		}
		for j := i + 1; j < len(messages); j++ {
			next := messages[j]
			switch next.Role {
			case "tool":
				if id := strings.TrimSpace(next.ToolCallID); id != "" {
					delete(pending, id)
				}
			case "assistant":
				if len(pending) > 0 {
					return true
				}
				i = j - 1 // 外层循环会 i++，从新的 assistant 批次继续扫
				break
			}
		}
	}
	return false
}

// 模拟 processToolCalls 在「t1 auto 已写 tool、t2 仍待审批」时 turn 暂停，随后 async 回灌的场景。
func TestHandleAsyncToolResult_openApprovalBatchDoesNotInsertMidBatch(t *testing.T) {
	hub := stream.NewHub(8, logx.Discard())
	orch := testOrchestrator(t, hub, &llm.MockClient{})

	history := []llm.Message{
		{Role: "user", Content: "run bg and sync"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{
			{
				ID: "call-bg-1", Type: "function",
				Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"command":"sleep 9","run_in_background":true}`},
			},
			{
				ID: "call-sync-1", Type: "function",
				Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`},
			},
		}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1 status=accepted"},
	}

	if n := len(unrespondedToolCallsAfterLastAssistant(history)); n != 1 {
		t.Fatalf("precondition: want 1 unresponded tool call, got %d", n)
	}
	if classifyToolResultTail(history) != tailTool {
		t.Fatalf("precondition: tail = %q, want tailTool", classifyToolResultTail(history))
	}

	outcome := orch.HandleAsyncToolResult(context.Background(), "sess-open-batch", &history, AsyncToolResultInput{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-job-1",
		Status: "succeeded", ResultText: "done",
	}, nil, 0)
	if outcome.Err != nil {
		t.Fatal(outcome.Err)
	}

	if historyHasOpenToolBatchViolation(history) {
		t.Fatalf("async_tool_result produced illegal history while call-sync-1 still pending:\n%s",
			formatHistoryForTest(history))
	}
}

func formatHistoryForTest(messages []llm.Message) string {
	var b strings.Builder
	for i, m := range messages {
		b.WriteString(formatMessageLine(i, m))
		b.WriteByte('\n')
	}
	return b.String()
}

func formatMessageLine(i int, m llm.Message) string {
	switch m.Role {
	case "assistant":
		names := make([]string, 0, len(m.ToolCalls))
		for _, tc := range m.ToolCalls {
			names = append(names, tc.Function.Name+"("+tc.ID+")")
		}
		if len(names) == 0 {
			return strings.TrimSpace(m.Content)
		}
		return "assistant tool_calls=[" + strings.Join(names, ", ") + "]"
	case "tool":
		return "tool " + m.ToolCallID + ": " + truncateForTest(m.Content, 60)
	default:
		return m.Role + ": " + truncateForTest(m.Content, 60)
	}
}

func truncateForTest(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
