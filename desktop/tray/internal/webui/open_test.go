//go:build windows

package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNodeProfileIncomplete(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ui/bootstrap", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"onboarding": map[string]any{"node_profile_completed": false},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if !nodeProfileIncomplete(context.Background(), srv.URL) {
		t.Fatal("expected incomplete")
	}
}

func TestNodeProfileIncomplete_legacyCompleted(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ui/bootstrap", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"onboarding": map[string]any{"node_profile_completed": true},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	if nodeProfileIncomplete(context.Background(), srv.URL) {
		t.Fatal("expected completed")
	}
}
