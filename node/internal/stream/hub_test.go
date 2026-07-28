package stream

import (
	"strings"
	"testing"
)

func TestHubPublishSubscribe(t *testing.T) {
	h := NewHub(16, nil)
	ch := h.Subscribe(0)
	defer h.Unsubscribe(ch)

	h.Publish("sess-1", "agent-a", "assistant", map[string]any{"content": "hi"})
	h.Publish("sess-1", "agent-a", "done", map[string]any{"finish_reason": "stop"})

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
	h.Publish("s", "a", "assistant", map[string]any{"content": "1"})
	h.Publish("s", "a", "assistant", map[string]any{"content": "2"})

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
	h.Publish("s", "a", "assistant", map[string]any{"content": "1"})
	if got := h.CurrentSeq(); got != 1 {
		t.Fatalf("after publish seq = %d", got)
	}
}

func TestHubSubscribeLiveSkipsHistory(t *testing.T) {
	h := NewHub(16, nil)
	h.Publish("s", "a", "assistant", map[string]any{"content": "old"})
	ch := h.Subscribe(h.CurrentSeq())
	defer h.Unsubscribe(ch)
	h.Publish("s", "a", "assistant", map[string]any{"content": "new"})
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
