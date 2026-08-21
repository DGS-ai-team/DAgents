package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func sideEffectContinueDepth(rt *runtime) int {
	if rt == nil {
		return 0
	}
	return rt.queue.CountByRequestType(queue.RequestTypeSideEffectContinue)
}

func TestCancelRecoverySchedulesContinueWhenBufferReady(t *testing.T) {
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
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1 status=accepted"},
	}
	rt.mu.Unlock()

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "succeeded", ResultText: "done",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)

	if !rt.sideEffects.HasReady() {
		t.Fatal("expected buffered async")
	}
	if sideEffectContinueDepth(rt) != 0 {
		t.Fatalf("produce on tailTool should not schedule continue, depth=%d", sideEffectContinueDepth(rt))
	}

	if mgr.CancelTurn(sess.ID) {
		t.Fatal("idle cancel should return false")
	}
	waitCancelRecoveryContinue(t, rt, "job-1", 5*time.Second)

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.sideEffects.HasReady() {
		t.Fatal("buffer should be drained after continue")
	}
	if !historyContainsJobID(rt.messages, "job-1") {
		t.Fatal("async side effect should be applied to history after cancel recovery")
	}
}

func TestCancelWithPendingAndBufferDoesNotScheduleContinue(t *testing.T) {
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
		{Role: "user", Content: "bg + sync"},
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

	if !rt.sideEffects.HasReady() {
		t.Fatal("expected buffer")
	}
	_ = mgr.CancelTurn(sess.ID)
	time.Sleep(100 * time.Millisecond)
	if sideEffectContinueDepth(rt) != 0 {
		t.Fatalf("pending should block cancel recovery continue, depth=%d", sideEffectContinueDepth(rt))
	}
}

func TestCancelWithEmptyBufferDoesNotScheduleContinue(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	_ = mgr.CancelTurn(sess.ID)
	time.Sleep(50 * time.Millisecond)
	if sideEffectContinueDepth(rt) != 0 {
		t.Fatalf("empty buffer should not schedule continue, depth=%d", sideEffectContinueDepth(rt))
	}
}

func TestClearContextDropsSideEffectBuffer(t *testing.T) {
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
		{Role: "user", Content: "x"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{approvalCall}},
	}
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: approvalCall}}})

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "failed", ErrorText: "exit 1",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)
	if !rt.sideEffects.HasReady() {
		t.Fatal("expected buffer before clear")
	}

	if _, err := mgr.ClearContext(sess.ID); err != nil {
		t.Fatal(err)
	}
	if rt.sideEffects.HasReady() {
		t.Fatal("ClearContext should drop side effect buffer")
	}
	if rt.messageCount() != 0 {
		t.Fatalf("messages = %d, want 0", rt.messageCount())
	}
}

func TestHumanMessagePreemptAppliesBufferedSideEffects(t *testing.T) {
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
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "failed", ErrorText: "exit 1",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)
	if sideEffectContinueDepth(rt) != 0 {
		t.Fatal("human preempt path must not rely on cancel recovery continue")
	}

	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, "message", "human preempt", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	waitForRuntimeHistory(t, rt, 8*time.Second, func(messages []llm.Message) bool {
		return !rt.sideEffects.HasReady() && historyContainsJobID(messages, "job-1")
	})

	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.sideEffects.HasReady() {
		t.Fatal("buffer should be applied during human turn step")
	}
	if !historyContainsJobID(rt.messages, "job-1") {
		t.Fatal("human turn should apply buffered async side effect at step start")
	}
}

// waitCancelRecoveryContinue 等待 cancel 恢复路径：side_effect_continue 入队或旁路已 apply 完成。
// continue 可能在两次轮询之间被 consumer 立即处理，不能仅靠队列深度观测。
func waitCancelRecoveryContinue(t *testing.T, rt *runtime, jobID string, timeout time.Duration) {
	t.Helper()
	waitForRuntimeHistory(t, rt, timeout, func(messages []llm.Message) bool {
		return !rt.sideEffects.HasReady() && historyContainsJobID(messages, jobID)
	})
}

func TestCancelRecoveryPublishesSideEffectTurnStartSSE(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", hub, &llm.MockClient{}, reg, pol, nil, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	sub := hub.Subscribe(0)
	defer hub.Unsubscribe(sub)

	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "bg"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"run_in_background":true}`},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1 status=accepted"},
	}
	rt.mu.Unlock()

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "succeeded", ResultText: "done",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)
	_ = mgr.CancelTurn(sess.ID)

	deadline := time.After(5 * time.Second)
	gotStart := false
	for !gotStart {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for side_effect_turn_start SSE")
		case ev := <-sub:
			if ev.SessionID != sess.ID {
				continue
			}
			if ev.Type == "side_effect_turn_start" {
				src, _ := ev.Data["source"].(string)
				if src != "cancel_recovery" {
					t.Fatalf("source = %q, want cancel_recovery", src)
				}
				implicit, _ := ev.Data["implicit_turn"].(bool)
				if !implicit {
					t.Fatal("expected implicit_turn=true")
				}
				gotStart = true
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	waitQueueDrain(t, rt, 5*time.Second)
}

func TestSideEffectApplyPublishesAppliedSSE(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", hub, &llm.MockClient{}, reg, pol, nil, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	sub := hub.Subscribe(0)
	defer hub.Unsubscribe(sub)

	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "bg"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{{
			ID: "call-bg-1", Type: "function",
			Function: llm.ToolCallFunction{Name: "bash_run", Arguments: `{"run_in_background":true}`},
		}}},
		{Role: "tool", ToolCallID: "call-bg-1", Content: "[TOOL_BACKGROUND] job_id=job-1 status=accepted"},
	}
	rt.mu.Unlock()

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "succeeded", ResultText: "done",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)
	_ = mgr.CancelTurn(sess.ID)
	waitQueueDrain(t, rt, 8*time.Second)

	deadline := time.After(8 * time.Second)
	gotApplied := false
	for !gotApplied {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for side_effect_applied SSE")
		case ev := <-sub:
			if ev.SessionID != sess.ID || ev.Type != "side_effect_applied" {
				continue
			}
			raw, _ := ev.Data["seqs"].([]any)
			if len(raw) == 0 {
				t.Fatal("side_effect_applied missing seqs")
			}
			gotApplied = true
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestClearContextPublishesSideEffectsClearedSSE(t *testing.T) {
	hub := stream.NewHub(32, logx.Discard())
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", hub, &llm.MockClient{}, reg, pol, nil, TurnOptions{SkillsEnabled: false}, logx.Discard())
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	sub := hub.Subscribe(0)
	defer hub.Unsubscribe(sub)

	approvalCall := llm.ToolCall{
		ID: "call-sync-1", Type: "function",
		Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"a.txt"}`},
	}
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "x"},
		{Role: "assistant", Content: "", ToolCalls: []llm.ToolCall{approvalCall}},
	}
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: approvalCall}}})

	if err := mgr.EnqueueAsyncToolResult(sess.ID, queue.AsyncToolResultPayload{
		JobID: "job-1", ToolName: "bash_run", ToolCallID: "async-1", Status: "failed", ErrorText: "exit 1",
	}); err != nil {
		t.Fatal(err)
	}
	waitQueueDrain(t, rt, 3*time.Second)

	if _, err := mgr.ClearContext(sess.ID); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	gotCleared := false
	for !gotCleared {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for side_effects_cleared SSE")
		case ev := <-sub:
			if ev.SessionID != sess.ID || ev.Type != "side_effects_cleared" {
				continue
			}
			dropped, _ := ev.Data["dropped"].(int)
			if dropped != 1 {
				if f, ok := ev.Data["dropped"].(float64); !ok || int(f) != 1 {
					t.Fatalf("dropped = %v, want 1", ev.Data["dropped"])
				}
			}
			raw, _ := ev.Data["seqs"].([]any)
			if len(raw) != 1 {
				t.Fatalf("seqs len = %d, want 1", len(raw))
			}
			gotCleared = true
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func historyContainsJobID(messages []llm.Message, jobID string) bool {
	needle := "job_id：" + jobID
	for _, m := range messages {
		if strings.Contains(m.Content, needle) {
			return true
		}
	}
	return false
}
