package desktopapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/desktop/tray/internal/uifocus"
	sharedupdate "github.com/DGS-ai-team/DAgents/shared/update"
)

type stubUpdateProvider struct {
	status sharedupdate.Status
}

func (s stubUpdateProvider) Snapshot() sharedupdate.Status {
	return s.status
}

func TestDesktopUpdateEndpoint(t *testing.T) {
	provider := stubUpdateProvider{status: sharedupdate.Status{
		CurrentVersion:   "0.6.1",
		LatestVersion:    "0.6.2",
		UpgradeAvailable: true,
		ManageReachable:  true,
		Channel:          "stable",
		Platform:         "windows-amd64",
		ApplyCommand:     "dagents update",
		Message:          "新版本 0.6.2 可用",
	}}
	srv := New(provider, nil, uifocus.NewStore())
	mux := srv.mux

	req := httptest.NewRequest(http.MethodGet, "/v1/desktop/update", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got sharedupdate.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.UpgradeAvailable || got.LatestVersion != "0.6.2" {
		t.Fatalf("got=%+v", got)
	}
}

func TestHealthEndpoint(t *testing.T) {
	srv := New(nil, nil, uifocus.NewStore())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestClipboardFilesEndpoint(t *testing.T) {
	srv := New(nil, nil, uifocus.NewStore())
	req := httptest.NewRequest(http.MethodGet, "/v1/desktop/clipboard/files", nil)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Paths == nil {
		t.Fatal("paths must be array")
	}
}

func TestUIFocusEndpoint(t *testing.T) {
	focus := uifocus.NewStore()
	srv := New(nil, nil, focus)
	body := strings.NewReader(`{"agent_id":"sess-1","ttl_seconds":60}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/desktop/ui/focus", body)
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !focus.IsFocused("sess-1") {
		t.Fatal("expected session focused")
	}
}

func TestUIFocusEndpointInvalidJSON(t *testing.T) {
	srv := New(nil, nil, uifocus.NewStore())
	req := httptest.NewRequest(http.MethodPost, "/v1/desktop/ui/focus", strings.NewReader(`{bad`))
	rec := httptest.NewRecorder()
	srv.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
