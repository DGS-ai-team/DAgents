package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestRemoteDriverCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/browser/call" {
			http.NotFound(w, r)
			return
		}
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		switch req.Op {
		case "ping":
			_ = json.NewEncoder(w).Encode(Response{OK: true, Detail: map[string]any{"driver": "browser-use-cdp-v1"}})
		case "navigate":
			_ = json.NewEncoder(w).Encode(Response{OK: true, URL: req.URL, Title: "Example"})
		case "snapshot":
			_ = json.NewEncoder(w).Encode(Response{
				OK:    true,
				URL:   "https://example.com",
				Title: "Example",
				Detail: map[string]any{
					"elements": []any{
						map[string]any{"index": 1, "selector": "#login", "engine": "browser-use"},
					},
				},
			})
		default:
			_ = json.NewEncoder(w).Encode(Response{OK: true})
		}
	}))
	defer srv.Close()

	on := true
	cfg := &config.Config{
		Browser: config.BrowserConfig{
			Enabled:    &on,
			ServiceURL: srv.URL,
		},
	}
	d, err := NewRemoteDriver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := d.Call(context.Background(), Request{Op: "navigate", SessionKey: "s1", URL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.OK || resp.URL != "https://example.com" {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestNewDriverUsesRemote(t *testing.T) {
	on := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(Response{OK: true, Detail: map[string]any{"driver": "browser-use-cdp-v1"}})
	}))
	defer srv.Close()
	cfg := &config.Config{
		Browser: config.BrowserConfig{
			Enabled:    &on,
			ServiceURL: srv.URL,
		},
	}
	d, err := NewDriver(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := d.(*RemoteDriver); !ok {
		t.Fatalf("expected RemoteDriver, got %T", d)
	}
}
