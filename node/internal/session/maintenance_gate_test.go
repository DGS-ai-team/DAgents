package session

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

type gateCountingLLM struct {
	llm.Client
	calls   *atomic.Int32
	started chan struct{}
}

func TestMaintenanceLeaseCannotBeReusedAcrossAcquisitions(t *testing.T) {
	g := newAgentExecutionGate()
	ctx1, release1, ok, err := g.acquireMaintenanceContext(context.Background())
	if err != nil || !ok {
		t.Fatalf("first acquire: %v %v", ok, err)
	}
	release1()
	ctx2, release2, ok, err := g.acquireMaintenanceContext(context.Background())
	if err != nil || !ok {
		t.Fatalf("second acquire: %v %v", ok, err)
	}
	defer release2()
	if g.owns(ctx1) {
		t.Fatal("old lease owns new acquisition")
	}
	if !g.owns(ctx2) {
		t.Fatal("current lease not recognized")
	}
}

func TestMaintenanceLeasesAreAgentScoped(t *testing.T) {
	a := newAgentExecutionGate()
	b := newAgentExecutionGate()
	ctxA, releaseA, ok, err := a.acquireMaintenanceContext(context.Background())
	if err != nil || !ok {
		t.Fatalf("agent A acquire: %v %v", ok, err)
	}
	defer releaseA()
	ctxB, releaseB, ok, err := b.acquireMaintenanceContext(context.Background())
	if err != nil || !ok {
		t.Fatalf("agent B acquire: %v %v", ok, err)
	}
	defer releaseB()
	if b.owns(ctxA) || a.owns(ctxB) {
		t.Fatal("lease crossed agent gate boundary")
	}
}

type blockingGateLLM struct {
	llm.Client
	started chan struct{}
	unblock chan struct{}
}

func (c *blockingGateLLM) StreamChat(ctx context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	close(c.started)
	select {
	case <-c.unblock:
	case <-ctx.Done():
		return llm.ChatResult{}, ctx.Err()
	}
	return c.Client.StreamChat(ctx, req, h)
}

func TestMaintenanceRejectedWhileChatLLMIsActive(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	client := &blockingGateLLM{Client: &llm.MockClient{}, started: make(chan struct{}), unblock: make(chan struct{})}
	m := NewManager("owner", stream.NewHub(32, logx.Discard()), client, reg, policy.NewDefaultEngine(), nil, TurnOptions{}, logx.Discard())
	defer m.Stop()
	s, _, err := m.CreateWithOptionsAndLLM("active-chat", TurnOptions{}, reg, nil, client, "agent-active")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueMessage(context.Background(), s.ID, "message", "busy", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("chat LLM did not start")
	}
	if _, acquired, err := m.TryAcquireMaintenance(context.Background(), "agent-active"); err != nil || acquired {
		t.Fatalf("maintenance acquired during active LLM: acquired=%v err=%v", acquired, err)
	}
	close(client.unblock)
	deadline := time.After(2 * time.Second)
	for {
		_, active, _, _ := m.RuntimeInfo(s.ID)
		if !active {
			break
		}
		select {
		case <-deadline:
			t.Fatal("chat did not finish")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	deadline = time.After(2 * time.Second)
	for {
		release, acquired, err := m.TryAcquireMaintenance(context.Background(), "agent-active")
		if err != nil {
			t.Fatal(err)
		}
		if acquired {
			release()
			break
		}
		select {
		case <-deadline:
			t.Fatal("maintenance not available after chat")
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func (c *gateCountingLLM) StreamChat(ctx context.Context, req llm.ChatRequest, h llm.StreamHandler) (llm.ChatResult, error) {
	c.calls.Add(1)
	if c.started != nil {
		select {
		case c.started <- struct{}{}:
		default:
		}
	}
	return c.Client.StreamChat(ctx, req, h)
}
func (c *gateCountingLLM) CompleteText(ctx context.Context, req llm.CompleteRequest) (string, error) {
	c.calls.Add(1)
	return c.Client.CompleteText(ctx, req)
}

func gateRuntime() *runtime { return &runtime{queue: queue.NewMessageQueue(), inputBox: NewInputBox()} }

func TestAgentExecutionGateIsolationAndMultiRuntimeWake(t *testing.T) {
	g1, g2 := newAgentExecutionGate(), newAgentExecutionGate()
	r1, r2 := gateRuntime(), gateRuntime()
	g1.register(r1)
	g1.register(r2)
	release, ok, err := g1.acquireMaintenance(context.Background())
	if err != nil || !ok {
		t.Fatalf("acquire: %v %v", ok, err)
	}
	if _, ok, _ := g1.acquireMaintenance(context.Background()); ok {
		t.Fatal("same agent gate re-acquired")
	}
	if _, ok, _ := g2.acquireMaintenance(context.Background()); !ok {
		t.Fatal("different agent blocked")
	}
	blocked, ch := g1.waitSnapshot()
	if !blocked {
		t.Fatal("gate not blocked")
	}
	release()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("multi-runtime wake was lost")
	}
}

func TestManagerMaintenanceGateTwoRuntimesAndOtherAgent(t *testing.T) {
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	total := &atomic.Int32{}
	counter := &gateCountingLLM{Client: &llm.MockClient{}, calls: total}
	m := NewManager("owner", stream.NewHub(64, logx.Discard()), counter, reg, policy.NewDefaultEngine(), nil, TurnOptions{}, logx.Discard())
	defer m.Stop()
	aLLM := &gateCountingLLM{Client: &llm.MockClient{}, calls: total}
	bLLM := &gateCountingLLM{Client: &llm.MockClient{}, calls: total}
	otherLLM := &gateCountingLLM{Client: &llm.MockClient{}, calls: total}
	a, _, err := m.CreateWithOptionsAndLLM("a", TurnOptions{}, reg, nil, aLLM, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := m.CreateWithOptionsAndLLM("b", TurnOptions{}, reg, nil, bLLM, "agent-a")
	if err != nil {
		t.Fatal(err)
	}
	leaseCtx, release, ok, err := m.TryAcquireMaintenanceContext(context.Background(), "agent-a")
	if err != nil || !ok {
		t.Fatalf("maintenance acquire: %v %v", ok, err)
	}
	c, _, err := m.CreateWithOptionsAndLLM("c", TurnOptions{}, reg, nil, otherLLM, "agent-b")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := m.TryAcquireMaintenance(context.Background(), "agent-a"); ok {
		t.Fatal("second maintenance acquired")
	}
	otherRelease, otherOK, _ := m.TryAcquireMaintenance(context.Background(), "agent-b")
	if !otherOK {
		t.Fatal("different agent blocked")
	}
	otherRelease()
	if _, err := m.EnqueueMessage(context.Background(), c.ID, "message", "other", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueMessage(context.Background(), a.ID, "message", "one", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := m.EnqueueMessage(context.Background(), b.ID, "message", "two", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-leaseCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("lease context not cancelled")
	}
	callDeadline := time.After(time.Second)
	for total.Load() < 1 {
		select {
		case <-callDeadline:
			t.Fatal("independent agent did not reach LLM")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if got := total.Load(); got != 1 {
		t.Fatalf("maintenance leaked into LLM, calls=%d", got)
	}
	select {
	case <-leaseCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("lease context not cancelled by chat")
	}
	release()
	deadline := time.After(2 * time.Second)
	for total.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("expected independent + 2 released calls, got %d", total.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestMaintenanceLeaseContextCancelledByInputUntilRelease(t *testing.T) {
	g := newAgentExecutionGate()
	r := gateRuntime()
	g.register(r)
	leaseCtx, release, ok, err := g.acquireMaintenanceContext(context.Background())
	if err != nil || !ok {
		t.Fatalf("acquire: %v %v", ok, err)
	}
	if _, err := r.inputBox.Append(InputKindUser, queue.Envelope{Content: "chat"}); err != nil {
		t.Fatal(err)
	}
	g.notifyInput()
	select {
	case <-leaseCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("maintenance context not cancelled")
	}
	if _, ok, _ := g.acquireMaintenance(context.Background()); ok {
		t.Fatal("cancelled maintenance still held incorrectly")
	}
	release()
	if rec, ok := r.inputBox.Pop(); ok {
		r.inputBox.Ack(rec.Seq)
	}
	if _, ok, _ := g.acquireMaintenance(context.Background()); ok == false {
		t.Fatal("release did not free gate")
	}
}
