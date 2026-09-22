package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestRemoteDriverV2Routes(t *testing.T) {
	var mu sync.Mutex
	seen := make([]string, 0, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/browser/ping":
			_, _ = w.Write([]byte(`{"ok":true,"detail":{"protocol_version":2}}`))
		case "/v2/browser/call", "/v2/browser/evaluate":
			var payload Request
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"ok":true,"detail":{"status":"succeeded"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	on := true
	cfg := &config.Config{Browser: config.BrowserConfig{Enabled: &on, ServiceURL: server.URL}}
	driver, err := NewRemoteDriver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Call(context.Background(), Request{Op: "call", SessionKey: "s", Actions: []Action{{Op: "observe"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := driver.Call(context.Background(), Request{Op: "evaluate", SessionKey: "s", Script: "document.title"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"GET /v2/browser/ping", "POST /v2/browser/call", "POST /v2/browser/evaluate"}
	if len(seen) != len(want) {
		t.Fatalf("routes = %#v", seen)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("route[%d] = %q, want %q", i, seen[i], want[i])
		}
	}
}
