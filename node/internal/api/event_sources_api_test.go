package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/events"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestEventSourceHTTPIsAgentScopedAndCAS(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	cfg.Onboarding.NodeProfileCompleted = true
	srv := NewServer(cfg, nil)
	defer srv.Close()
	srv.cfg.Onboarding.NodeProfileCompleted = true
	now := time.Now()
	for _, id := range []string{"auto-a", "auto-b"} {
		if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: []byte(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	r := events.SourceRegistration{SourceID: "files", OwnerAgentID: "auto-a", Revision: 1, Root: t.TempDir(), Enabled: true}
	body, _ := json.Marshal(r)
	req := httptest.NewRequest("POST", "/v1/agents/auto-a/event-sources", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create=%d body=%s", rec.Code, rec.Body.String())
	}
	claim := r
	claim.OwnerAgentID = "auto-b"
	body, _ = json.Marshal(claim)
	req = httptest.NewRequest("POST", "/v1/agents/auto-b/event-sources", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code == 201 {
		t.Fatal("cross owner source takeover accepted")
	}
	req = httptest.NewRequest("GET", "/v1/agents/auto-b/event-sources", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	var list []events.SourceRegistration
	json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 0 {
		t.Fatalf("cross owner list=%v", list)
	}
	req = httptest.NewRequest("DELETE", "/v1/agents/auto-b/event-sources/files?revision=1", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code == 204 {
		t.Fatal("cross owner delete accepted")
	}
	req = httptest.NewRequest("DELETE", "/v1/agents/auto-a/event-sources/files?revision=2", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code == 204 {
		t.Fatal("stale CAS accepted")
	}
}
