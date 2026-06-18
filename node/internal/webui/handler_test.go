package webui_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/webui"
)

func TestHandlerServesIndex(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /ui/", webui.Handler())
	mux.HandleFunc("GET /ui", webui.RedirectHandler())

	req := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body, _ := io.ReadAll(rec.Body)
	html := string(body)
	if !strings.Contains(html, "html") {
		t.Fatalf("expected html, got %q", body)
	}
	if !strings.Contains(html, `id="app"`) {
		t.Fatalf("expected vue mount root, got %q", body)
	}
}

func TestHandlerServesAssets(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /ui/", webui.Handler())

	idxReq := httptest.NewRequest(http.MethodGet, "/ui/", nil)
	idxRec := httptest.NewRecorder()
	mux.ServeHTTP(idxRec, idxReq)
	if idxRec.Code != http.StatusOK {
		t.Fatalf("index status=%d", idxRec.Code)
	}
	body := idxRec.Body.String()
	start := strings.Index(body, `src="/ui/assets/`)
	if start < 0 {
		t.Fatalf("no asset src in index: %q", body)
	}
	start += len(`src="`)
	end := strings.Index(body[start:], `"`)
	if end < 0 {
		t.Fatal("malformed asset src")
	}
	assetPath := body[start : start+end]

	req := httptest.NewRequest(http.MethodGet, assetPath, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", assetPath, rec.Code, rec.Body.String())
	}
}

func TestRedirectUI(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ui", webui.RedirectHandler())

	req := httptest.NewRequest(http.MethodGet, "/ui", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/ui/" {
		t.Fatalf("location=%q", loc)
	}
}
