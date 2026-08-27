package session

import (
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
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: approvalCall}}})

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

func TestInputBoxTriggerStartsTurnOnEmptyHistory(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("sess-empty-bridge")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)

	if err := mgr.EnqueueTriggerMessage(sess.ID, "trig-bridge", "trigger hello"); err != nil {
		t.Fatal(err)
	}
	messages := waitForRuntimeHistory(t, rt, 5*time.Second, func(messages []llm.Message) bool {
		for _, message := range messages {
			if message.Role == "user" && strings.Contains(message.Content, "trigger hello") {
				return true
			}
		}
		return false
	})

	if len(messages) < 1 {
		t.Fatalf("history len = %d, want bridge user after continue", len(messages))
	}
	found := false
	for _, m := range messages {
		if m.Role == "user" && strings.Contains(m.Content, "trigger hello") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("messages = %+v", rt.messages)
	}
	foundTrigger := false
	for _, message := range messages {
		if llm.IsMessageSource(message, llm.MessageSourceTrigger, llm.MessageFormRequest, llm.UserNameTrigger) {
			foundTrigger = true
			break
		}
	}
	if !foundTrigger {
		t.Fatalf("trigger should be recorded as a normal trigger input: %+v", messages)
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
	waitForSideEffectLen(t, rt, 2, 3*time.Second)
	_ = mgr.CancelTurn(sess.ID)
	waitForRuntimeHistory(t, rt, 8*time.Second, func(messages []llm.Message) bool {
		return !rt.sideEffects.HasReady() && historyContainsJobID(messages, "job-1") && historyContainsJobID(messages, "job-2")
	})

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

func TestDuplicateAsyncResultIsAppliedOnce(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "run in background"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID: "call-background-duplicate",
			Function: llm.ToolCallFunction{
				Name:      "bash_run",
				Arguments: `{"run_in_background":true}`,
			},
		}}},
		{Role: "tool", ToolCallID: "call-background-duplicate", Content: "[TOOL_BACKGROUND] job_id=job-duplicate"},
	}
	rt.mu.Unlock()

	payload := queue.AsyncToolResultPayload{
		JobID:      "job-duplicate",
		ToolName:   "bash_run",
		ToolCallID: "async-job-duplicate",
		Status:     "succeeded",
		ResultText: "done",
	}
	for i := 0; i < 2; i++ {
		if err := mgr.EnqueueAsyncToolResult(sess.ID, payload); err != nil {
			t.Fatalf("async result %d enqueue: %v", i+1, err)
		}
	}
	waitForSideEffectLen(t, rt, 1, 3*time.Second)

	_ = mgr.CancelTurn(sess.ID)
	waitForRuntimeHistory(t, rt, 8*time.Second, func(messages []llm.Message) bool {
		return !rt.sideEffects.HasReady() && historyContainsJobID(messages, "job-duplicate")
	})

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.sideEffects.HasReady() {
		t.Fatal("duplicate async result remained buffered")
	}
	callbackResults := 0
	for _, msg := range rt.messages {
		if strings.Contains(msg.Content, "[ASYNC_TOOL_RESULT]") && strings.Contains(msg.Content, "job_id=job-duplicate") {
			callbackResults++
		}
	}
	if callbackResults != 1 {
		t.Fatalf("duplicate async result produced %d callback results, want 1", callbackResults)
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

func waitForSideEffectLen(t *testing.T, rt *runtime, want int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rt.sideEffects.Len() >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for side-effect buffer length >= %d; got %d", want, rt.sideEffects.Len())
}

func waitForRuntimeHistory(t *testing.T, rt *runtime, timeout time.Duration, predicate func([]llm.Message) bool) []llm.Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rt.mu.Lock()
		messages := append([]llm.Message(nil), rt.messages...)
		rt.mu.Unlock()
		if predicate == nil || predicate(messages) {
			return messages
		}
		time.Sleep(5 * time.Millisecond)
	}
	rt.mu.Lock()
	messages := append([]llm.Message(nil), rt.messages...)
	rt.mu.Unlock()
	t.Fatalf("timeout waiting for runtime history condition; messages=%+v", messages)
	return nil
}
