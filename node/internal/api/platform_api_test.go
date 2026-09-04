package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestPlatformGatewayReportsUnavailableWithoutShell(t *testing.T) {
	t.Setenv("DAGENTS_DESKTOP_API_URL", "")
	t.Setenv("DAGENTS_DESKTOP_BRIDGE_TOKEN", "")
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	defer srv.Close()

	req := httptest.NewRequest(http.MethodGet, "/v1/platform/capabilities", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got platformCapabilities
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.DesktopShell || got.NativeDirectoryPicker || got.UpdateApply {
		t.Fatalf("unavailable Shell reported as capable: %+v", got)
	}
}

func TestPlatformGatewayForwardsNativeRequestsThroughNode(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Authorization") != "Bearer bridge-token" {
			http.Error(w, `{"message":"missing bridge token"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/desktop/dialog/directory":
			_, _ = w.Write([]byte(`{"ok":true,"cancelled":false,"path":"C:\\workspace"}`))
		case "/v1/desktop/clipboard/files":
			_, _ = w.Write([]byte(`{"paths":["C:\\workspace\\a.txt"]}`))
		case "/v1/desktop/ui/focus":
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/v1/desktop/update/apply":
			_, _ = w.Write([]byte(`{"ok":true,"message":"updated"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer bridge.Close()
	t.Setenv("DAGENTS_DESKTOP_API_URL", bridge.URL)
	t.Setenv("DAGENTS_DESKTOP_BRIDGE_TOKEN", "bridge-token")
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())
	defer srv.Close()

	checks := []struct {
		method string
		path   string
		body   string
		want   string
	}{
		{http.MethodPost, "/v1/platform/directory-picker", `{}`, `C:\\workspace`},
		{http.MethodGet, "/v1/platform/clipboard/files", "", `C:\\workspace\\a.txt`},
		{http.MethodPost, "/v1/platform/ui-focus", `{"source_id":"tab-1"}`, `"ok":true`},
		{http.MethodPost, "/v1/platform/update/apply", `{"force":false}`, `"updated"`},
	}
	for _, check := range checks {
		req := httptest.NewRequest(check.method, check.path, strings.NewReader(check.body))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status=%d body=%s", check.method, check.path, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), check.want) {
			t.Fatalf("%s %s body=%s missing %q", check.method, check.path, rec.Body.String(), check.want)
		}
	}
}
