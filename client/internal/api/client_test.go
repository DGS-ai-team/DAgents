package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientCreateMessageStream(t *testing.T) {
	mux := http.NewServeMux()
	var mu sync.Mutex
	seq := 0
	sessionID := "sess-test"
	var gotMessageAgentID string
	var gotStreamAgentID string

	mux.HandleFunc("POST /v1/agents/"+sessionID+"/ensure", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "agent_id": sessionID})
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if v, ok := body["agent_id"].(string); ok {
			gotMessageAgentID = v
		}
		go func() {
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			seq++
			mu.Unlock()
		}()
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
	})
	mux.HandleFunc("GET /v1/streams", func(w http.ResponseWriter, r *http.Request) {
		gotStreamAgentID = r.URL.Query().Get("agent_id")
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// 模拟 echo turn
		time.Sleep(30 * time.Millisecond)
		payload := map[string]any{
			"session_id": sessionID, "agent_id": "a1", "type": "assistant", "seq": 1,
			"ts": "2026-01-01T00:00:00Z", "data": map[string]any{"content": "（echo）hi"},
		}
		b, _ := json.Marshal(payload)
		_, _ = w.Write([]byte("id: 1\nevent: assistant\ndata: " + string(b) + "\n\n"))
		flusher.Flush()

		donePayload := map[string]any{
			"session_id": sessionID, "agent_id": "a1", "type": "done", "seq": 2,
			"ts": "2026-01-01T00:00:00Z", "data": map[string]any{"finish_reason": "stop"},
		}
		b2, _ := json.Marshal(donePayload)
		_, _ = w.Write([]byte("id: 2\nevent: done\ndata: " + string(b2) + "\n\n"))
		flusher.Flush()

		<-r.Context().Done()
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := New(ts.URL, ts.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := c.EnsureAgent(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	sid := sessionID

	doneCh := make(chan struct{})
	var text strings.Builder
	go func() {
		_ = c.StreamEvents(ctx, sid, 0, func(ev StreamEvent) bool {
			if ev.Type == "assistant" {
				if s, ok := ev.Data["content"].(string); ok {
					text.WriteString(s)
				}
			}
			if ev.Type == "done" {
				close(doneCh)
				return false
			}
			return true
		})
	}()

	time.Sleep(50 * time.Millisecond)
	if err := c.SubmitMessage(ctx, sid, "hi"); err != nil {
		t.Fatal(err)
	}

	select {
	case <-doneCh:
	case <-ctx.Done():
		t.Fatal("timeout")
	}
	if !strings.Contains(text.String(), "hi") {
		t.Fatalf("text = %q", text.String())
	}
	if gotMessageAgentID != sessionID {
		t.Fatalf("POST /v1/messages agent_id = %q, want %q", gotMessageAgentID, sessionID)
	}
	if gotStreamAgentID != sessionID {
		t.Fatalf("GET /v1/streams agent_id = %q, want %q", gotStreamAgentID, sessionID)
	}
}

func TestClientCancelTurn(t *testing.T) {
	mux := http.NewServeMux()
	sessionID := "sess-cancel"
	cancelled := false
	mux.HandleFunc("POST /v1/agents/"+sessionID+"/cancel", func(w http.ResponseWriter, _ *http.Request) {
		cancelled = true
		_ = json.NewEncoder(w).Encode(map[string]any{
			"session_id": sessionID,
			"cancelled":  true,
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := New(ts.URL, ts.Client())
	ok, err := c.CancelTurn(context.Background(), sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || !cancelled {
		t.Fatalf("cancelled=%v ok=%v", cancelled, ok)
	}
}
