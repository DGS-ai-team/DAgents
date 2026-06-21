package session

import (
	"context"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func TestEnqueueTriggerMessageCarriesTriggerID(t *testing.T) {
	mgr := NewManager("agent-1", nil, nil, nil, nil, nil, TurnOptions{}, nil)
	sid := "sess-trigger-env"
	mgr.mu.Lock()
	rt := newRuntime(sid, mgr.agentID, mgr.hub, mgr.llm, mgr.tools, mgr.policy, mgr.store, mgr.logger, nil, nil, nil, 0, mgr.turn, mgr.triggerDelivery)
	mgr.sessions[sid] = rt
	mgr.mu.Unlock()

	if err := mgr.EnqueueTriggerMessage(sid, "trig-abc", "hello trigger"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	env, err := rt.queue.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if env.RequestType != queue.RequestTypeTriggerMessage {
		t.Fatalf("request_type = %q, want %q", env.RequestType, queue.RequestTypeTriggerMessage)
	}
	if env.TriggerID != "trig-abc" || env.Content != "hello trigger" {
		t.Fatalf("env = %+v", env)
	}
}

func TestConsumeLoopClearsTriggerPendingAfterApply(t *testing.T) {
	store, err := triggers.OpenStore(t.TempDir()+"/t.json", 10)
	if err != nil {
		t.Fatal(err)
	}
	mgr := testManager(t)
	defer mgr.Stop()
	mgr.SetTriggerDeliveryTracker(store)

	sess, _, err := mgr.Create("sess-pending")
	if err != nil {
		t.Fatal(err)
	}
	rt := mgr.getRuntime(sess.ID)
	if rt == nil {
		t.Fatal("runtime missing")
	}

	rt.mu.Lock()
	rt.messages = []llm.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "ok"},
	}
	rt.mu.Unlock()

	store.MarkPendingDelivery("trig-1")
	if err := mgr.EnqueueTriggerMessage(sess.ID, "trig-1", "trigger fired"); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(5 * time.Second)
	for store.HasPendingDelivery("trig-1") {
		select {
		case <-deadline:
			t.Fatal("pending not cleared after side effect apply")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}
