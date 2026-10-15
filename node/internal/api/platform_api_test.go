package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/platform"
)

type fakeDirectoryPicker struct {
	available bool
	result    platform.DirectoryPickResult
	err       error
}

func (p fakeDirectoryPicker) Available(context.Context) bool { return p.available }

func (p fakeDirectoryPicker) Pick(context.Context) (platform.DirectoryPickResult, error) {
	return p.result, p.err
}

func TestPlatformCapabilitiesReportsOnlyNodeHostCapabilities(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithDirectoryPicker(fakeDirectoryPicker{available: true}), WithSkipStore())
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
	if !got.NativeDirectoryPicker {
		t.Fatalf("Node-native directory picker should be available: %+v", got)
	}
}

func TestPlatformDirectoryPickerReturnsCancelledResult(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithDirectoryPicker(fakeDirectoryPicker{available: true, result: platform.DirectoryPickResult{OK: true, Cancelled: true}}), WithSkipStore())
	defer srv.Close()

	req := httptest.NewRequest(http.MethodPost, "/v1/platform/directory-picker", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"cancelled":true`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
