package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMCPStatusReturnsNodeHealthProjection(t *testing.T) {
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	t.Cleanup(srv.Close)
	req := httptest.NewRequest(http.MethodGet, "/v1/mcp/status", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		Servers []any `json:"servers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Health.Status != "unconfigured" || payload.Servers == nil {
		t.Fatalf("payload=%s", rec.Body.String())
	}
}
