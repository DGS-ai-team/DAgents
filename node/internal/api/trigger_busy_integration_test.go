package api

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type busyTriggerASKClient struct {
	calls         atomic.Int32
	secondStarted chan struct{}
}

func (c *busyTriggerASKClient) StreamChat(_ context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	if c.calls.Add(1) == 1 {
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "busy-ask", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user_information", Arguments: `{"question":"确认"}`}}}, FinishReason: "tool_calls"}, nil
	}
	if c.secondStarted != nil {
		select {
		case c.secondStarted <- struct{}{}:
		default:
		}
	}
	return llm.ChatResult{Content: "已完成", FinishReason: "stop"}, nil
}

func (*busyTriggerASKClient) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*busyTriggerASKClient) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestSchedulerBusyAutoWakeupsCoalesceAndDisableDoesNotRunQueued(t *testing.T) {
	root := t.TempDir()
	reg, err := tools.NewRegistry(filepath.Join(root, "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(filepath.Join(root, "handbook")); err != nil {
		t.Fatal(err)
	}
	triggerStore, err := triggers.OpenStore(filepath.Join(root, "triggers.json"), 20)
	if err != nil {
		t.Fatal(err)
	}
	client := &busyTriggerASKClient{secondStarted: make(chan struct{}, 1)}
	mgr := session.NewManager("auto-busy", stream.NewHub(32, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, session.TurnOptions{AutoAgent: true, WorkspaceRoot: filepath.Join(root, "workspace")}, logx.Discard())
	defer mgr.Stop()
	// Match production wiring before the canonical runtime is constructed.
	mgr.SetTriggerDeliveryTracker(triggerStore)
	if _, _, err := mgr.Create("auto-busy"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	def, err := triggerStore.EnsureAutoDefault("auto-busy", 60, now)
	if err != nil {
		t.Fatal(err)
	}
	sched := triggers.NewScheduler(triggerStore, &session.TriggerSubmitter{Mgr: mgr}, 60)
	defer sched.Stop()

	if _, err := mgr.EnqueueMessage(context.Background(), "auto-busy", "message", "先处理这个问题", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && client.calls.Load() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("initial human activation calls=%d", client.calls.Load())
	}
	var view *session.HydrateView
	for time.Now().Before(deadline) {
		view, err = mgr.GetHydrateView("auto-busy")
		if err != nil {
			t.Fatal(err)
		}
		if view.PendingHITL != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if view == nil || view.PendingHITL == nil {
		t.Fatalf("human ASK did not persist: view=%+v err=%v", view, err)
	}

	first, err := sched.FireTrigger(def.TriggerID, "schedule", nil, false, nil)
	if err != nil || first.Status != triggers.FireStatusQueued {
		t.Fatalf("first fire=%+v err=%v", first, err)
	}

	// Repeated scheduler ticks while the user turn is pending leave one queued
	// wakeup, but must never create an unbounded FIFO or another model turn.
	for i := 0; i < 5; i++ {
		if _, err := sched.FireTrigger(def.TriggerID, "schedule", nil, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view, err = mgr.GetHydrateView("auto-busy")
		if err != nil {
			t.Fatal(err)
		}
		if view.QueuePending == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if view.QueuePending != 1 {
		t.Fatalf("busy wakeups did not exercise one queued delivery: queue=%d active=%v", view.QueuePending, view.HasActiveTurn)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("busy scheduler started another model turn: %d", client.calls.Load())
	}

	// Turning the default frequency off removes the queued delivery identity;
	// the stale queued envelope may be consumed, but must not execute a turn.
	if _, err := triggerStore.EnsureAutoDefault("auto-busy", 0, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.EnqueueMessage(context.Background(), "auto-busy", "resume", "", nil, map[string]any{"type": "user_information", "tool_call_id": "busy-ask", "answer": "确认"}, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.secondStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("approval did not start the original turn's final model request")
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view, err = mgr.GetHydrateView("auto-busy")
		if err != nil {
			t.Fatal(err)
		}
		if view.QueuePending == 0 && !view.HasActiveTurn {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if client.calls.Load() != 2 {
		t.Fatalf("approval/disabled queued wakeup caused unexpected model calls=%d", client.calls.Load())
	}
	if view.QueuePending != 0 || view.HasActiveTurn {
		t.Fatalf("disabled stale wakeup was executed or retained: queue=%d active=%v", view.QueuePending, view.HasActiveTurn)
	}
}
