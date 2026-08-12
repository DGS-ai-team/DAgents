package desktopapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLocalhostCORS(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/desktop/update", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
	h := withLocalhostCORS(mux)

	req := httptest.NewRequest(http.MethodOptions, "/v1/desktop/update", nil)
	req.Header.Set("Origin", "http://127.0.0.1:18765")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("options status=%d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://127.0.0.1:18765" {
		t.Fatalf("cors header=%q", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}
