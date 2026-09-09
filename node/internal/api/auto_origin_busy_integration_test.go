package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

type autoOriginBusyLLM struct {
	calls       atomic.Int32
	entered     chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
}

func (c *autoOriginBusyLLM) releaseProvider() { c.releaseOnce.Do(func() { close(c.release) }) }

func (c *autoOriginBusyLLM) StreamChat(ctx context.Context, _ llm.ChatRequest, _ llm.StreamHandler) (llm.ChatResult, error) {
	if c.calls.Add(1) == 1 {
		close(c.entered)
		select {
		case <-c.release:
		case <-ctx.Done():
			return llm.ChatResult{}, ctx.Err()
		}
		return llm.ChatResult{ToolCalls: []llm.ToolCall{{ID: "auto-ask", Type: "function", Function: llm.ToolCallFunction{Name: "ask_user_information", Arguments: `{"question":"确认"}`}}}, FinishReason: "tool_calls"}, nil
	}
	return llm.ChatResult{Content: "已完成", FinishReason: "stop"}, nil
}

func (*autoOriginBusyLLM) CompleteText(context.Context, llm.CompleteRequest) (string, error) {
	return "", nil
}
func (*autoOriginBusyLLM) NormalizeAssistant(existing []llm.Message, msg llm.Message) llm.Message {
	return llm.StubNormalizeAssistant(existing, msg)
}

func TestAutoOriginBusyWakeupsUseProductionProviderAndDoNotReplayAfterDisable(t *testing.T) {
	cfg := testConfig(t)
	reg, err := tools.NewRegistry(filepath.Join(cfg.RuntimeDir(), "workspace"), 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.SetBuiltinEnabled(config.ExpandBuiltinToolGroups([]string{"filesystem"})); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetHandbookRoot(filepath.Join(cfg.RuntimeDir(), "handbook")); err != nil {
		t.Fatal(err)
	}
	client := &autoOriginBusyLLM{entered: make(chan struct{}), release: make(chan struct{})}
	s := NewServer(cfg, nil, WithLLM(client), WithTools(reg), WithPolicy(policy.NewDefaultEngine()), WithSkipStore())
	t.Cleanup(func() {
		client.releaseProvider()
		s.Close()
	})
	if s.triggerSched == nil || s.triggerStore == nil || s.autonomyStore == nil {
		t.Fatal("production trigger/autonomy stores were not initialized")
	}
	s.triggerSched.Stop()
	// Use the same Scheduler implementation and durable store, but submit
	// directly to the already injected test runtime so server ensure/reload
	// cannot replace the captured LLM client.
	sched := triggers.NewScheduler(s.triggerStore, &session.TriggerSubmitter{Mgr: s.sessions}, 60)
	defer sched.Stop()
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	s.agents = agents
	t.Cleanup(func() { _ = agents.Close() })
	now := time.Now().UTC()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-origin", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto"}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := s.autonomyStore.PutProfile(autonomy.Profile{AgentID: "auto-origin", WakeIntervalSeconds: 60, MaxToolRounds: 4, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	turnOpts := s.sessions.DefaultTurnOptions()
	turnOpts.AutoAgent = true
	turnOpts.WorkspaceRoot = filepath.Join(cfg.RuntimeDir(), "agents", "auto-origin", "workspace")
	if _, _, err := s.sessions.CreateWithOptionsAndLLM("auto-origin", turnOpts, reg, policy.NewDefaultEngine(), client, "auto-origin"); err != nil {
		t.Fatal(err)
	}
	def, err := s.triggerStore.EnsureAutoDefault("auto-origin", 60, now)
	if err != nil {
		t.Fatal(err)
	}
	if def.TargetSessionID == nil || *def.TargetSessionID != "auto-origin" {
		t.Fatalf("default target=%+v", def)
	}

	first, err := sched.FireTrigger(def.TriggerID, "schedule", nil, false, nil)
	if err != nil || first.Status != triggers.FireStatusQueued {
		t.Fatalf("first auto fire=%+v err=%v", first, err)
	}
	select {
	case <-client.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("auto activation did not enter provider")
	}
	claimed, ok := s.triggerStore.GetTrigger(def.TriggerID)
	if !ok || claimed.PendingDeliveryID == nil {
		t.Fatalf("auto delivery was not durably claimed while provider was blocked: %+v", claimed)
	}
	if limit, trusted, providerErr := s.triggerToolRoundProvider(context.Background(), "auto-origin", def.TriggerID, *claimed.PendingDeliveryID); providerErr != nil || !trusted || limit != 4 {
		t.Fatalf("production provider valid delivery=(%d,%v,%v)", limit, trusted, providerErr)
	}
	if _, trusted, err := s.triggerToolRoundProvider(context.Background(), "auto-origin", def.TriggerID, "wrong"); err == nil && trusted {
		t.Fatal("production provider accepted wrong delivery")
	}
	client.releaseProvider()
	deadline := time.Now().Add(3 * time.Second)
	var view struct {
		PendingHITL  map[string]any `json:"pending_hitl"`
		QueuePending int            `json:"queue_pending"`
	}
	for time.Now().Before(deadline) {
		h, e := s.sessions.GetHydrateView("auto-origin")
		if e != nil {
			t.Fatal(e)
		}
		view.PendingHITL, view.QueuePending = h.PendingHITL, h.QueuePending
		if view.PendingHITL != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if view.PendingHITL == nil {
		t.Fatalf("auto ASK did not persist: %+v", view)
	}
	for i := 0; i < 5; i++ {
		if _, err := sched.FireTrigger(def.TriggerID, "schedule", nil, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	h, err := s.sessions.GetHydrateView("auto-origin")
	if err != nil {
		t.Fatal(err)
	}
	if h == nil || h.QueuePending > 1 {
		queuePending := -1
		if h != nil {
			queuePending = h.QueuePending
		}
		t.Fatalf("auto busy queue=%d", queuePending)
	}
	if client.calls.Load() != 1 {
		t.Fatalf("repeated busy fires started another model turn: calls=%d", client.calls.Load())
	}

	if _, err := s.triggerStore.EnsureAutoDefault("auto-origin", 0, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.sessions.EnqueueMessage(context.Background(), "auto-origin", "resume", "", nil, map[string]any{"type": "user_information", "tool_call_id": "auto-ask", "answer": "确认"}, ""); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		h, e := s.sessions.GetHydrateView("auto-origin")
		if e != nil {
			t.Fatal(e)
		}
		if client.calls.Load() >= 2 && h.QueuePending == 0 && !h.HasActiveTurn {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if client.calls.Load() != 2 {
		t.Fatalf("disabled queued auto wakeup started extra model turn: calls=%d", client.calls.Load())
	}
	h, err = s.sessions.GetHydrateView("auto-origin")
	if err != nil || h == nil || h.QueuePending != 0 || h.HasActiveTurn {
		t.Fatalf("auto origin did not settle after approval: view=%+v err=%v", h, err)
	}
}
