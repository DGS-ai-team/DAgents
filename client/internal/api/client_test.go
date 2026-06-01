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

	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"session_id": sessionID})
	})
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, _ *http.Request) {
		go func() {
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			seq++
			mu.Unlock()
		}()
		_ = json.NewEncoder(w).Encode(map[string]any{"accepted": true})
	})
	mux.HandleFunc("GET /v1/streams", func(w http.ResponseWriter, r *http.Request) {
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

	sid, err := c.CreateSession(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if sid != sessionID {
		t.Fatalf("session_id = %q", sid)
	}

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
}

func TestClientCancelTurn(t *testing.T) {
	mux := http.NewServeMux()
	sessionID := "sess-cancel"
	cancelled := false
	mux.HandleFunc("POST /v1/sessions/"+sessionID+"/cancel", func(w http.ResponseWriter, _ *http.Request) {
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
