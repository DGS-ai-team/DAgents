package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestCancelInvalidatesQueuedTurnContinuation(t *testing.T) {
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
		RequestType:  queue.RequestTypeTurnContinuation,
		SessionEpoch: 0,
		TurnID:       state.TurnID,
		Generation:   state.Generation,
	}
	if !mgr.CancelTurn(sess.ID) {
		t.Fatal("queued continuation should be considered cancellable")
	}
	if rt.acceptEnvelope(old) {
		t.Fatal("cancelled turn continuation must be rejected")
	}
}

func TestAcceptEnvelopeRequiresActiveTurnForResume(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if rt.acceptEnvelope(queue.Envelope{RequestType: queue.RequestTypeResume}) {
		t.Fatal("resume without an active turn must be rejected")
	}
	if !rt.acceptEnvelope(queue.Envelope{RequestType: queue.RequestTypeSideEffectContinue}) {
		t.Fatal("side-effect continuation without an active turn must be accepted")
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

	for _, env := range []queue.Envelope{{RequestType: queue.RequestTypeAsyncToolResult, SessionEpoch: 3}} {
		if !rt.acceptEnvelope(env) {
			t.Fatalf("external event was rejected: %#v", env)
		}
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

func TestHumanInputDuringPendingHITLWaitsForExplicitCancel(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	call := llm.ToolCall{
		ID:   "call-approval-wait",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "bash_run",
			Arguments: `{"command":"echo approval"}`,
		},
	}
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "run command"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
	}
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: call}}})

	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeMessage, "先问一个问题", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for rt.inputBox.Len() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("input was not retained while HITL pending; len=%d", rt.inputBox.Len())
		}
		time.Sleep(10 * time.Millisecond)
	}

	if got := rt.pendingSnapshot(); got == nil || len(got.Items) != 1 {
		t.Fatalf("pending HITL changed before explicit cancel: %#v", got)
	}
	rt.mu.Lock()
	if len(rt.messages) != 2 {
		rt.mu.Unlock()
		t.Fatalf("human input changed history while HITL pending: %+v", rt.messages)
	}
	rt.mu.Unlock()

	if !mgr.CancelTurn(sess.ID) {
		t.Fatal("expected explicit cancel to interrupt pending HITL")
	}
	deadline = time.Now().Add(3 * time.Second)
	for rt.inputBox.Len() != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("queued input was not drained after cancel; len=%d", rt.inputBox.Len())
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline = time.Now().Add(3 * time.Second)
	for {
		rt.mu.Lock()
		processed := historyContainsText(rt.messages, "先问一个问题")
		closed := countToolResponsesForCall(rt.messages, call.ID) == 1
		messages := append([]llm.Message(nil), rt.messages...)
		rt.mu.Unlock()
		if processed && closed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("queued human input was not processed after cancel: %+v", messages)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestHumanInputDoesNotAnswerUserInformationHITL(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	call := llm.ToolCall{
		ID:   "call-user-information-wait",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "ask_user_information",
			Arguments: `{"question":"请选择环境"}`,
		},
	}
	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "开始部署"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{call}},
	}
	rt.mu.Unlock()
	setTestPendingHITL(t, rt, &turn.PendingHITL{Items: []turn.PendingHITLItem{{ToolCall: call}}})

	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeMessage, "这是另一个问题", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for rt.inputBox.Len() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rt.inputBox.Len() != 1 {
		t.Fatal("ordinary human input should not answer ask_user_information")
	}
	if pending := rt.pendingSnapshot(); pending == nil || len(pending.Items) != 1 {
		t.Fatalf("user-information pending changed: %#v", pending)
	}

	resume := map[string]any{
		"type":         "user_information",
		"tool_call_id": call.ID,
		"answer":       "staging",
	}
	if _, err := mgr.EnqueueMessage(context.Background(), sess.ID, queue.RequestTypeResume, "", nil, resume, ""); err != nil {
		t.Fatalf("typed user-information resume should be accepted: %v", err)
	}
	waitForRuntimeHistory(t, rt, 8*time.Second, func(messages []llm.Message) bool {
		return rt.pendingSnapshot() == nil && historyContainsText(messages, "这是另一个问题")
	})
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
