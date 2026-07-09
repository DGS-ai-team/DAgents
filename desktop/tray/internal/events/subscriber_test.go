package events

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/nodeclient"
	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/pending"
)

func TestPollLoopSyncsWithoutSSE(t *testing.T) {
	var listCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/sessions":
			listCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sessions": []any{
					map[string]any{
						"session_id":        "sess-1",
						"has_pending_hitl":  true,
						"pending_hitl_items": 1,
					},
				},
			})
		case "/v1/streams":
			http.Error(w, "sse unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	store := pending.NewStore()
	sub := NewSubscriber(nodeclient.New(srv.URL), store, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub.Start(ctx)
	time.Sleep(150 * time.Millisecond)
	sub.Stop()

	if listCalls.Load() < 1 {
		t.Fatalf("list calls = %d, want >= 1", listCalls.Load())
	}
	if store.Summary().SessionCount != 1 {
		t.Fatalf("summary = %+v", store.Summary())
	}
}
