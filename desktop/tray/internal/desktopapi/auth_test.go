package desktopapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/uifocus"
)

func TestHandlerRequiresBridgeTokenWhenConfigured(t *testing.T) {
	srv := New(nil, nil, uifocus.NewStore(), "secret")
	req := httptest.NewRequest(http.MethodPost, "/v1/desktop/ui/focus", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/health", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status=%d", rec.Code)
	}
}

func TestHandlerRejectsForeignOrigin(t *testing.T) {
	srv := New(nil, nil, uifocus.NewStore(), "secret")
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d", rec.Code)
	}
}
