package session

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/queue"
)

func TestCancelInvalidatesQueuedToolResult(t *testing.T) {
	mgr := testManager(t)
	defer mgr.Stop()

	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	rt.mu.Lock()
	rt.turnID = "turn-old"
	rt.generation = 7
	rt.continuationPending = true
	rt.mu.Unlock()

	old := queue.Envelope{
		RequestType:  queue.RequestTypeToolResult,
		SessionEpoch: 0,
		TurnID:       "turn-old",
		Generation:   7,
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
	rt.turnID = "turn-current"
	rt.generation = 2
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
	rt.mu.Lock()
	rt.turnID = "turn-old"
	rt.generation = 4
	rt.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, continuationContextKey{}, continuationRef{turnID: "turn-old", generation: 4})
	cancel()
	if err := rt.enqueueToolResult(ctx, sess.ID); err != context.Canceled {
		t.Fatalf("late callback error = %v, want context.Canceled", err)
	}
	if got := rt.queue.CountByRequestType(queue.RequestTypeToolResult); got != 0 {
		t.Fatalf("late callback enqueued %d tool results", got)
	}

	// A callback from a completed turn with no context binding remains supported
	// for the public compatibility API.
	if err := rt.enqueueToolResult(nil, sess.ID); err != nil {
		t.Fatal(err)
	}
	if got := rt.queue.CountByRequestType(queue.RequestTypeToolResult); got != 1 {
		t.Fatalf("unbound callback enqueued %d tool results, want 1", got)
	}
}
