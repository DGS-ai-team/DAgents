package stream

import (
	"strings"
	"testing"
	"time"
)

func TestHubPublishSubscribe(t *testing.T) {
	h := NewHub(16, nil)
	ch := h.Subscribe(0)
	defer h.Unsubscribe(ch)

	h.Publish("sess-1", "assistant", map[string]any{"content": "hi"})
	h.Publish("sess-1", "turn_finished", map[string]any{"finish_reason": "stop"})

	var types []string
	for i := 0; i < 2; i++ {
		ev := <-ch
		types = append(types, ev.Type)
		if ev.Seq != i+1 {
			t.Fatalf("seq = %d, want %d", ev.Seq, i+1)
		}
	}
	if types[0] != "assistant" || types[1] != "turn_finished" {
		t.Fatalf("unexpected types: %v", types)
	}
}

func TestHubReplayAfterSeq(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("s", "assistant", map[string]any{"content": "1"})
	h.Publish("s", "assistant", map[string]any{"content": "2"})

	ch := h.Subscribe(1)
	defer h.Unsubscribe(ch)

	ev := <-ch
	if ev.Seq != 2 {
		t.Fatalf("seq = %d", ev.Seq)
	}
}

func TestHubEphemeralIsNotReplayed(t *testing.T) {
	h := NewHub(16, nil)
	h.PublishEphemeral("s", "typing", map[string]any{"content": "old"})
	ch := h.Subscribe(0)
	defer h.Unsubscribe(ch)

	select {
	case ev := <-ch:
		t.Fatalf("unexpected ephemeral replay: %+v", ev)
	default:
	}
	h.PublishEphemeral("s", "typing", map[string]any{"content": "new"})
	select {
	case ev := <-ch:
		if ev.Type != "typing" || ev.Data["content"] != "new" {
			t.Fatalf("unexpected live event: %+v", ev)
		}
	default:
		t.Fatal("expected live ephemeral event")
	}
}

func TestHubCurrentSeq(t *testing.T) {
	h := NewHub(16, nil)
	if got := h.CurrentSeq(); got != 0 {
		t.Fatalf("initial seq = %d", got)
	}
	h.Publish("s", "assistant", map[string]any{"content": "1"})
	if got := h.CurrentSeq(); got != 1 {
		t.Fatalf("after publish seq = %d", got)
	}
}

func TestHubAgentCursorSkipsForeignAndEphemeralEvents(t *testing.T) {
	h := NewHub(16, nil)
	first := h.Publish("agt-a", "assistant", map[string]any{"content": "one"})
	h.PublishEphemeral("agt-a", "execution", map[string]any{"event": "process_output"})
	h.Publish("agt-b", "assistant", map[string]any{"content": "foreign"})
	finished := h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})

	if first.AgentSeq != 1 || finished.AgentSeq != 2 {
		t.Fatalf("agent seq = %d, %d; want 1, 2", first.AgentSeq, finished.AgentSeq)
	}
	if first.StreamEpoch == "" || first.StreamEpoch != finished.StreamEpoch {
		t.Fatalf("stream epoch not stable: %q / %q", first.StreamEpoch, finished.StreamEpoch)
	}
	if first.Delivery != "replayable" {
		t.Fatalf("replayable delivery = %q", first.Delivery)
	}

	sub := h.SubscribeAgentCursor(1, 1, "agt-a")
	defer h.Unsubscribe(sub.Events)
	select {
	case ev := <-sub.Events:
		if ev.Type != "turn_finished" || ev.AgentSeq != 2 {
			t.Fatalf("unexpected agent replay: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for agent replay")
	}
	select {
	case ev := <-sub.Events:
		t.Fatalf("unexpected extra agent replay: %+v", ev)
	default:
	}
}

func TestHubAgentCursorReportsHistoryTruncation(t *testing.T) {
	h := NewHub(2, nil)
	h.Publish("agt-a", "assistant", map[string]any{"content": "one"})
	h.Publish("agt-a", "assistant", map[string]any{"content": "two"})
	h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop"})

	sub := h.SubscribeAgentCursor(0, 0, "agt-a")
	defer h.Unsubscribe(sub.Events)
	if !sub.ResyncRequired {
		t.Fatal("expected resync when requested agent history is truncated")
	}
	if sub.StreamEpoch == "" || sub.CurrentAgentSeq != 3 {
		t.Fatalf("bad subscription cursor: %+v", sub)
	}
}

func TestHubSubscribeLiveSkipsHistory(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("s", "assistant", map[string]any{"content": "old"})
	ch := h.Subscribe(h.CurrentSeq())
	defer h.Unsubscribe(ch)
	h.Publish("s", "assistant", map[string]any{"content": "new"})
	select {
	case ev := <-ch:
		if ev.Data["content"] != "new" {
			t.Fatalf("unexpected replay/live: %+v", ev.Data)
		}
	default:
		t.Fatal("expected live event")
	}
}

func TestEventFormatSSE(t *testing.T) {
	ev := Event{
		AgentID: "agt-x",
		Type:    "assistant",
		Seq:     3,
		TS:      "2026-05-27T00:00:00Z",
		Data:    map[string]any{"content": "ok"},
	}
	s := ev.FormatSSE()
	if !strings.Contains(s, "event: assistant") || !strings.Contains(s, "id: 3") {
		t.Fatalf("bad sse: %q", s)
	}
	if strings.Contains(s, "session_id") {
		t.Fatalf("wire SSE must not include session_id: %q", s)
	}
	if !strings.Contains(s, `"agent_id":"agt-x"`) {
		t.Fatalf("expected agent_id in sse: %q", s)
	}
}

func TestHubCriticalTurnFinishedDeliveredWhenBufferFull(t *testing.T) {
	h := NewHub(16, nil)
	ch := h.Subscribe(0)
	defer h.Unsubscribe(ch)

	gotDone := make(chan struct{})
	go func() {
		for ev := range ch {
			if ev.Type == "turn_finished" {
				close(gotDone)
				return
			}
			// 模拟慢消费者：慢慢排空缓冲，给锁外补送终态事件让路
			time.Sleep(time.Millisecond)
		}
	}()

	for i := 0; i < 400; i++ {
		h.Publish("agt-1", "assistant", map[string]any{"content": "x"})
	}
	h.Publish("agt-1", "turn_finished", map[string]any{"finish_reason": "stop", "turn_complete": true})

	select {
	case <-gotDone:
	case <-time.After(5 * time.Second):
		t.Fatal("critical turn_finished was dropped while subscriber buffer was full")
	}
}

func TestHubSubscribeAgentFiltersOtherAgents(t *testing.T) {
	h := NewHub(64, nil)
	chA := h.SubscribeAgent(0, "agt-a")
	defer h.Unsubscribe(chA)

	h.Publish("agt-b", "assistant", map[string]any{"content": "noise"})
	h.Publish("agt-b", "turn_finished", map[string]any{"finish_reason": "stop"})
	h.Publish("agt-a", "assistant", map[string]any{"content": "mine"})
	h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop", "turn_complete": true})

	var types []string
	for i := 0; i < 2; i++ {
		select {
		case ev := <-chA:
			if ev.AgentID != "agt-a" {
				t.Fatalf("got other agent event: %+v", ev)
			}
			types = append(types, ev.Type)
		case <-time.After(time.Second):
			t.Fatalf("timeout waiting for agt-a event %d", i)
		}
	}
	if types[0] != "assistant" || types[1] != "turn_finished" {
		t.Fatalf("types = %v", types)
	}
	select {
	case ev := <-chA:
		t.Fatalf("unexpected extra event: %+v", ev)
	default:
	}
}

func TestHubSubscribeAgentIgnoresForeignFlood(t *testing.T) {
	h := NewHub(64, nil)
	chA := h.SubscribeAgent(0, "agt-a")
	defer h.Unsubscribe(chA)

	// 他 Agent 洪峰不应占满 A 的缓冲
	for i := 0; i < 500; i++ {
		h.Publish("agt-b", "assistant", map[string]any{"content": "x"})
	}
	h.Publish("agt-a", "turn_finished", map[string]any{"finish_reason": "stop", "turn_complete": true})

	select {
	case ev := <-chA:
		if ev.Type != "turn_finished" || ev.AgentID != "agt-a" {
			t.Fatalf("expected agt-a turn_finished, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agt-a turn_finished blocked/dropped by foreign agent flood")
	}
}
