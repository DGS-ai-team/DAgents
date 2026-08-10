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
	h.Publish("sess-1", "done", map[string]any{"finish_reason": "stop"})

	var types []string
	for i := 0; i < 2; i++ {
		ev := <-ch
		types = append(types, ev.Type)
		if ev.Seq != i+1 {
			t.Fatalf("seq = %d, want %d", ev.Seq, i+1)
		}
	}
	if types[0] != "assistant" || types[1] != "done" {
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
		SessionID: "sess-x",
		AgentID:   "agt-x",
		Type:      "assistant",
		Seq:       3,
		TS:        "2026-05-27T00:00:00Z",
		Data:      map[string]any{"content": "ok"},
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

func TestHubCriticalDoneDeliveredWhenBufferFull(t *testing.T) {
	h := NewHub(16, nil)
	ch := h.Subscribe(0)
	defer h.Unsubscribe(ch)

	gotDone := make(chan struct{})
	go func() {
		for ev := range ch {
			if ev.Type == "done" {
				close(gotDone)
				return
			}
			// 模拟慢消费者：慢慢排空缓冲，给锁外补送 done 让路
			time.Sleep(time.Millisecond)
		}
	}()

	for i := 0; i < 400; i++ {
		h.Publish("agt-1", "assistant", map[string]any{"content": "x"})
	}
	h.Publish("agt-1", "done", map[string]any{"finish_reason": "stop", "turn_complete": true})

	select {
	case <-gotDone:
	case <-time.After(5 * time.Second):
		t.Fatal("critical done was dropped while subscriber buffer was full")
	}
}

func TestHubSubscribeAgentFiltersOtherAgents(t *testing.T) {
	h := NewHub(64, nil)
	chA := h.SubscribeAgent(0, "agt-a")
	defer h.Unsubscribe(chA)

	h.Publish("agt-b", "assistant", map[string]any{"content": "noise"})
	h.Publish("agt-b", "done", map[string]any{"finish_reason": "stop"})
	h.Publish("agt-a", "assistant", map[string]any{"content": "mine"})
	h.Publish("agt-a", "done", map[string]any{"finish_reason": "stop", "turn_complete": true})

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
	if types[0] != "assistant" || types[1] != "done" {
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
	h.Publish("agt-a", "done", map[string]any{"finish_reason": "stop", "turn_complete": true})

	select {
	case ev := <-chA:
		if ev.Type != "done" || ev.AgentID != "agt-a" {
			t.Fatalf("expected agt-a done, got %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agt-a done blocked/dropped by foreign agent flood")
	}
}

