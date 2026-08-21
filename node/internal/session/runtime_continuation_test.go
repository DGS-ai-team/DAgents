package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestCancelInvalidatesQueuedToolResult(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if err := rt.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	state := rt.turnCoordinator.Snapshot()

	old := queue.Envelope{
		RequestType:  queue.RequestTypeToolResult,
		SessionEpoch: 0,
		TurnID:       state.TurnID,
		Generation:   state.Generation,
	}
	if !mgr.CancelTurn(sess.ID) {
		t.Fatal("queued continuation should be considered cancellable")
	}
	if rt.acceptEnvelope(old) {
		t.Fatal("cancelled tool_result must be rejected")
	}
}

func TestExternalEventsSurviveTurnGenerationChange(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.sessionEpoch = 3
	rt.mu.Unlock()

	for _, env := range []queue.Envelope{
		{RequestType: queue.RequestTypeAsyncToolResult, SessionEpoch: 3},
		{RequestType: queue.RequestTypeTriggerMessage, SessionEpoch: 3},
	} {
		if !rt.acceptEnvelope(env) {
			t.Fatalf("external event was rejected: %#v", env)
		}
	}
}

func TestCancelDoesNotLetLateToolCallbackCreateContinuation(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if err := rt.lifecycleBeginHumanTurn(); err != nil {
		t.Fatal(err)
	}
	execution := rt.turnCoordinator.ExecutionContext()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = turn.WithExecutionContext(ctx, execution)
	cancel()
	if err := rt.enqueueToolResult(ctx, sess.ID); err != context.Canceled {
		t.Fatalf("late callback error = %v, want context.Canceled", err)
	}
	if got := rt.queue.CountByRequestType(queue.RequestTypeToolResult); got != 0 {
		t.Fatalf("late callback enqueued %d tool results", got)
	}

	// An unbound callback cannot create a new lifecycle turn after cancellation.
	if err := rt.lifecycleCancel(); err != nil {
		t.Fatal(err)
	}
	if err := rt.enqueueToolResult(nil, sess.ID); err == nil {
		t.Fatal("unbound callback should be rejected without an active step")
	}
	if got := rt.queue.CountByRequestType(queue.RequestTypeToolResult); got != 0 {
		t.Fatalf("unbound callback enqueued %d tool results", got)
	}
}

func TestCancelPendingHITLRepairsToolResultOnce(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	call := llm.ToolCall{
		ID:   "call-pending-cancel",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "bash_run",
			Arguments: `{"command":"sleep 20"}`,
		},
	}
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "run a command"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
	}
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: call}}})

	if !mgr.CancelTurn(sess.ID) {
		t.Fatal("expected pending HITL cancellation to report changed=true")
	}
	if mgr.CancelTurn(sess.ID) {
		t.Fatal("second cancellation should be a no-op")
	}

	rt.mu.Lock()
	msgs := append([]llm.Message(nil), rt.messages...)
	rt.mu.Unlock()
	pending := rt.pendingSnapshot()
	if pending != nil {
		t.Fatalf("pending HITL was not cleared: %#v", pending)
	}
	if got := countToolResponsesForCall(msgs, call.ID); got != 1 {
		t.Fatalf("tool result count=%d, want 1; messages=%+v", got, msgs)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeResume, "", nil, map[string]any{
		"type":        "selection",
		"approved":    []string{call.ID},
		"rejected":    []string{},
		"approval_id": "stale-after-cancel",
	}, ""); err == nil {
		t.Fatal("resume after cancellation should be rejected as no_pending_hitl")
	} else if err.Error() != "no_pending_hitl" {
		t.Fatalf("resume error=%q, want no_pending_hitl", err)
	}
}

func TestDuplicateResumeQueueProducesOneToolResult(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	call := llm.ToolCall{
		ID:   "call-duplicate-resume",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "read_file",
			Arguments: `{"path":"missing-file-for-regression"}`,
		},
	}
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "read a file"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
	}
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: call}}})

	resume := map[string]any{
		"type":     "selection",
		"approved": []string{},
		"rejected": []string{call.ID},
	}
	for i := 0; i < 2; i++ {
		if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeResume, "", nil, resume, ""); err != nil {
			t.Fatalf("resume %d enqueue: %v", i+1, err)
		}
	}
	waitQueueDrain(t, rt, 5*time.Second)

	rt.mu.Lock()
	msgs := append([]llm.Message(nil), rt.messages...)
	rt.mu.Unlock()
	pending := rt.pendingSnapshot()
	if pending != nil {
		t.Fatalf("pending HITL remained after resume: %#v", pending)
	}
	if got := countToolResponsesForCall(msgs, call.ID); got != 1 {
		t.Fatalf("duplicate resume produced %d tool results, want 1; messages=%+v", got, msgs)
	}
}

func countToolResponsesForCall(messages []llm.Message, callID string) int {
	n := 0
	for _, msg := range messages {
		if msg.Role == "tool" && msg.ToolCallID == callID {
			n++
		}
	}
	return n
}
