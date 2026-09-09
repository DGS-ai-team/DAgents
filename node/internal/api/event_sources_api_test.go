package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
	body, _ := json.Marshal(map[string]any{"source_id": r.SourceID, "owner_agent_id": r.OwnerAgentID, "revision": r.Revision, "root": r.Root, "enabled": r.Enabled})
	req := httptest.NewRequest("POST", "/v1/agents/auto-a/event-sources", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create=%d body=%s", rec.Code, rec.Body.String())
	}
	legacyBody := []byte(`{"SourceID":"legacy","OwnerAgentID":"auto-a","Revision":1,"Root":"C:\\legacy","Enabled":false}`)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/agents/auto-a/event-sources", bytes.NewReader(legacyBody)))
	if rec.Code != 201 || strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Fatalf("legacy disabled source status=%d body=%s", rec.Code, rec.Body.String())
	}
	claim := r
	claim.OwnerAgentID = "auto-b"
	body, _ = json.Marshal(map[string]any{"source_id": claim.SourceID, "owner_agent_id": claim.OwnerAgentID, "revision": claim.Revision, "root": claim.Root, "enabled": claim.Enabled})
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
	if rec.Code != http.StatusOK {
		t.Fatalf("cross owner list status=%d body=%s", rec.Code, rec.Body.String())
	}
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("cross owner list decode: %v", err)
	}
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

func TestEventSourceHTTPHealthReportsProbeSuccessAndFailure(t *testing.T) {
	cfg := testConfig(t)
	cfg.LLM.Mock = true
	cfg.Onboarding.NodeProfileCompleted = true
	srv := NewServer(cfg, nil)
	defer srv.Close()
	srv.cfg.Onboarding.NodeProfileCompleted = true
	now := time.Now().UTC()
	for _, id := range []string{"health-ok", "health-bad", "health-other"} {
		if err := srv.agents.Save(context.Background(), store.AgentRecord{AgentID: id, ConfigSnapshot: []byte(`{"agent_type":"auto"}`), CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	goodRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(goodRoot, "signal.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := events.SourceRegistration{SourceID: "good", OwnerAgentID: "health-ok", Revision: 1, Root: goodRoot, Enabled: true}
	bad := events.SourceRegistration{SourceID: "bad", OwnerAgentID: "health-bad", Revision: 1, Root: filepath.Join(t.TempDir(), "missing"), Enabled: true}
	for _, registration := range []events.SourceRegistration{good, bad} {
		body, _ := json.Marshal(map[string]any{"source_id": registration.SourceID, "owner_agent_id": registration.OwnerAgentID, "revision": registration.Revision, "root": registration.Root, "enabled": registration.Enabled})
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/v1/agents/"+registration.OwnerAgentID+"/event-sources", bytes.NewReader(body)))
		if rr.Code != http.StatusCreated {
			t.Fatalf("register %s=%d %s", registration.SourceID, rr.Code, rr.Body.String())
		}
	}
	probe := events.New(srv.eventStore, events.DeliveryFunc(func(context.Context, events.Event) error { return nil }))
	if state, err := probe.Poll(context.Background(), events.Config{SourceID: "good", OwnerAgentID: "health-ok", Revision: 1, Root: goodRoot, HashContent: true}, now); err != nil || !state.Baseline || state.LastError != "" {
		t.Fatalf("healthy probe state=%+v err=%v", state, err)
	}
	if state, err := probe.Poll(context.Background(), events.Config{SourceID: "bad", OwnerAgentID: "health-bad", Revision: 1, Root: bad.Root, HashContent: true}, now); err == nil || state.LastError == "" || state.FailureCount == 0 {
		t.Fatalf("failed probe state=%+v err=%v", state, err)
	}
	var views []struct {
		events.SourceRegistration
		State struct {
			Baseline  bool   `json:"baseline"`
			LastError string `json:"last_error"`
		} `json:"state"`
	}
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/v1/agents/health-ok/event-sources", nil))
	if rr.Code != http.StatusOK || json.NewDecoder(rr.Body).Decode(&views) != nil || len(views) != 1 || !views[0].State.Baseline || views[0].State.LastError != "" {
		t.Fatalf("healthy API status=%d views=%+v body=%s", rr.Code, views, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/v1/agents/health-bad/event-sources", nil))
	views = nil
	if rr.Code != http.StatusOK || json.NewDecoder(rr.Body).Decode(&views) != nil || len(views) != 1 || views[0].State.LastError == "" {
		t.Fatalf("failed API status=%d views=%+v", rr.Code, views)
	}
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/v1/agents/health-other/event-sources", nil))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "good") || strings.Contains(rr.Body.String(), "bad") {
		t.Fatalf("owner isolation status=%d body=%s", rr.Code, rr.Body.String())
	}
}
