package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func TestAutonomyV2ConfigSyncsStableDefaultTriggerAndReconcilesFailure(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	body := `{"expected_revision":0,"responsibility":"keep","wake_interval_seconds":60,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`
	w := autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", body)
	if w.Code != http.StatusOK {
		t.Fatalf("first put=%d %s", w.Code, w.Body)
	}
	def, ok := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID("auto-v2"))
	if !ok || !def.Enabled || def.TargetSessionID == nil || *def.TargetSessionID != "auto-v2" || def.Controller != "auto" {
		t.Fatalf("default trigger=%+v ok=%v", def, ok)
	}
	firstRevision, firstNext := def.Revision, *def.NextFireAt
	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":1,"responsibility":"keep","wake_interval_seconds":60,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("repeat put=%d %s", w.Code, w.Body)
	}
	def, _ = s.triggerStore.GetTrigger(def.TriggerID)
	if def.Revision != firstRevision || *def.NextFireAt != firstNext {
		t.Fatalf("repeat changed trigger=%+v", def)
	}

	originalPath := s.cfg.TriggersStorePath()
	backupPath := originalPath + ".bak"
	if err := os.Rename(originalPath, backupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(originalPath, 0o700); err != nil {
		t.Fatal(err)
	}
	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":2,"responsibility":"changed","wake_interval_seconds":120,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`)
	if w.Code != http.StatusServiceUnavailable || !bytes.Contains(w.Body.Bytes(), []byte("saved_profile")) {
		t.Fatalf("sync failure=%d %s", w.Code, w.Body)
	}
	if err := os.Remove(originalPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backupPath, originalPath); err != nil {
		t.Fatal(err)
	}
	w = autonomyV2Request(s, http.MethodPost, "/v1/agents/auto-v2/auto-config/reconcile", "{}")
	if w.Code != http.StatusOK {
		t.Fatalf("reconcile=%d %s", w.Code, w.Body)
	}
	def, _ = s.triggerStore.GetTrigger(def.TriggerID)
	if int(def.Condition["interval_seconds"].(int64)) != 120 {
		t.Fatalf("reconciled trigger=%+v", def)
	}
	var profile struct {
		Revision            int64 `json:"revision"`
		WakeIntervalSeconds int64 `json:"wake_interval_seconds"`
	}
	if err := json.Unmarshal(autonomyV2Request(s, http.MethodGet, "/v1/agents/auto-v2/auto-config", "").Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	if profile.Revision != 3 || profile.WakeIntervalSeconds != 120 {
		t.Fatalf("reconcile changed profile=%+v", profile)
	}

	w = autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":3,"responsibility":"changed","wake_interval_seconds":0,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("disable=%d %s", w.Code, w.Body)
	}
	def, _ = s.triggerStore.GetTrigger(def.TriggerID)
	if def.Enabled || def.NextFireAt != nil {
		t.Fatalf("disabled trigger=%+v", def)
	}
}

func TestAutonomyV2DefaultTriggerSyncIsSerialized(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	requests := []string{
		`{"expected_revision":0,"responsibility":"a","wake_interval_seconds":60,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`,
		`{"expected_revision":0,"responsibility":"b","wake_interval_seconds":120,"max_tool_rounds":4,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`,
	}
	var wg sync.WaitGroup
	results := make(chan int, len(requests))
	for _, body := range requests {
		wg.Add(1)
		go func(body string) {
			defer wg.Done()
			results <- autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", body).Code
		}(body)
	}
	wg.Wait()
	close(results)
	var okCount, conflictCount int
	for code := range results {
		switch code {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("concurrent results ok=%d conflict=%d", okCount, conflictCount)
	}
	defs := s.triggerStore.ListTriggers()
	var autoCount int
	for _, d := range defs {
		if d.Controller == "auto" && d.ControllerID == "auto-v2" {
			autoCount++
		}
	}
	if autoCount != 1 {
		t.Fatalf("auto defaults=%d defs=%+v", autoCount, defs)
	}
	var profile struct {
		WakeIntervalSeconds int64 `json:"wake_interval_seconds"`
	}
	if err := json.Unmarshal(autonomyV2Request(s, http.MethodGet, "/v1/agents/auto-v2/auto-config", "").Body.Bytes(), &profile); err != nil {
		t.Fatal(err)
	}
	def, _ := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID("auto-v2"))
	if int64(def.Condition["interval_seconds"].(int64)) != profile.WakeIntervalSeconds {
		t.Fatalf("trigger interval=%v profile interval=%d", def.Condition["interval_seconds"], profile.WakeIntervalSeconds)
	}
}

func TestAutonomyV2TriggerRoundProviderRequiresExactDefaultDelivery(t *testing.T) {
	s, _ := autonomyV2TestServer(t)
	if w := autonomyV2Request(s, http.MethodPut, "/v1/agents/auto-v2/auto-config", `{"expected_revision":0,"responsibility":"x","wake_interval_seconds":60,"max_tool_rounds":7,"dreaming_enabled":false,"dreaming_time":"03:00","timezone":"UTC"}`); w.Code != http.StatusOK {
		t.Fatalf("put=%d %s", w.Code, w.Body)
	}
	id := triggers.AutoDefaultTriggerID("auto-v2")
	d, ok := s.triggerStore.GetTrigger(id)
	if !ok {
		t.Fatal("default trigger missing")
	}
	delivery := "delivery-rounds"
	d.PendingDeliveryID = &delivery
	sessionID := "auto-v2"
	d.PendingSessionID = &sessionID
	if err := s.triggerStore.ReplaceTrigger(*d); err != nil {
		t.Fatal(err)
	}
	s.triggerStore.MarkPendingDelivery(id)
	limit, trusted, err := s.triggerToolRoundProvider(context.Background(), "auto-v2", id, delivery)
	if err != nil || !trusted || limit != 7 {
		t.Fatalf("provider=(%d,%v,%v)", limit, trusted, err)
	}
	defaultOptions := s.sessions.DefaultTurnOptions()
	if defaultOptions.TriggerToolRoundProvider == nil {
		t.Fatal("NewServer did not wire trigger round provider into default session options")
	}
	if limit, trusted, err := defaultOptions.TriggerToolRoundProvider(context.Background(), "auto-v2", id, delivery); err != nil || !trusted || limit != 7 {
		t.Fatalf("wired provider=(%d,%v,%v)", limit, trusted, err)
	}
	if _, trusted, err := s.triggerToolRoundProvider(context.Background(), "auto-v2", id, "wrong-delivery"); err == nil || trusted {
		t.Fatalf("wrong delivery accepted: trusted=%v err=%v", trusted, err)
	}
	ordinary, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "ordinary", Condition: map[string]any{"interval_seconds": 60}, TargetAgentID: "auto-v2"}, "auto-v2", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.triggerStore.CreateTrigger(ordinary); err != nil {
		t.Fatal(err)
	}
	if _, trusted, err := s.triggerToolRoundProvider(context.Background(), "auto-v2", ordinary.TriggerID, "delivery"); err != nil || trusted {
		t.Fatalf("ordinary trigger accepted: trusted=%v err=%v", trusted, err)
	}
	if _, trusted, err := s.triggerToolRoundProvider(context.Background(), "auto-v2", "auto-default:other-agent", delivery); err == nil || trusted {
		t.Fatalf("other Auto trigger should be rejected: trusted=%v err=%v", trusted, err)
	}
	if err := s.agents.Save(context.Background(), store.AgentRecord{AgentID: "auto-v2", ConfigSnapshot: []byte(`{"agent_type":"normal","defaults":{}}`), RuntimeRevision: 2}); err != nil {
		t.Fatal(err)
	}
	if _, trusted, err := s.triggerToolRoundProvider(context.Background(), "auto-v2", id, delivery); err == nil || trusted {
		t.Fatalf("changed Agent type should reject old default: trusted=%v err=%v", trusted, err)
	}
}

func TestNewServerStartupRebuildsOnlyAutoDefaults(t *testing.T) {
	cfg := testConfig(t)
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, item := range []struct{ id, typ string }{{"startup-auto", "auto"}, {"startup-auto-no-profile", "auto"}, {"startup-normal", "normal"}} {
		if err := agents.Save(context.Background(), store.AgentRecord{AgentID: item.id, ConfigSnapshot: []byte(`{"agent_type":"` + item.typ + `","defaults":{}}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	_ = agents.Close()
	autonomyStore, err := autonomy.Open(filepath.Join(cfg.RuntimeDir(), "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := autonomyStore.PutProfile(autonomy.Profile{AgentID: "startup-auto", WakeIntervalSeconds: 60, MaxToolRounds: 4, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	triggerStore, err := triggers.OpenStore(cfg.TriggersStorePath(), 20)
	if err != nil {
		t.Fatal(err)
	}
	ordinary, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "ordinary-startup", Condition: map[string]any{"interval_seconds": 60}, TargetAgentID: "startup-normal"}, "startup-normal", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := triggerStore.CreateTrigger(ordinary); err != nil {
		t.Fatal(err)
	}
	s := NewServer(cfg, nil, WithLLM(&autonomyV2PromptLLM{}))
	if s.triggerSched != nil {
		s.triggerSched.Stop()
	}
	if s.sessions != nil {
		s.sessions.Stop()
	}
	s.Close()
	if s.startupErr != nil {
		t.Fatalf("startup=%v", s.startupErr)
	}
	if _, ok := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID("startup-auto")); !ok {
		t.Fatal("startup did not rebuild auto default")
	}
	if _, ok := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID("startup-normal")); ok {
		t.Fatal("startup created default for normal Agent")
	}
	off, ok := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID("startup-auto-no-profile"))
	if !ok || off.Enabled || off.NextFireAt != nil {
		t.Fatalf("auto without profile should have a disabled default: %+v ok=%v", off, ok)
	}
	if _, ok := s.triggerStore.GetTrigger(ordinary.TriggerID); !ok {
		t.Fatal("startup removed ordinary trigger")
	}
	if s.agents != nil {
		_ = s.agents.Close()
	}
}

func TestNewServerStartupKeepsPendingAutoDefaultFrozen(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = true
	agents, err := store.OpenAgents(cfg.AgentsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "pending-auto", ConfigSnapshot: []byte(`{"agent_type":"auto","defaults":{}}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := agents.Save(context.Background(), store.AgentRecord{AgentID: "healthy-auto", ConfigSnapshot: []byte(`{"agent_type":"auto","defaults":{}}`), RuntimeRevision: 1, CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	_ = agents.Close()
	autonomyStore, err := autonomy.Open(filepath.Join(cfg.RuntimeDir(), "autonomy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := autonomyStore.PutProfile(autonomy.Profile{AgentID: "pending-auto", WakeIntervalSeconds: 0, MaxToolRounds: 4, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	if err := autonomyStore.PutProfile(autonomy.Profile{AgentID: "healthy-auto", WakeIntervalSeconds: 60, MaxToolRounds: 4, DreamingTime: "03:00", Timezone: "UTC"}, 0); err != nil {
		t.Fatal(err)
	}
	triggerStore, err := triggers.OpenStore(cfg.TriggersStorePath(), 20)
	if err != nil {
		t.Fatal(err)
	}
	d, err := triggerStore.EnsureAutoDefault("pending-auto", 60, now)
	if err != nil {
		t.Fatal(err)
	}
	delivery, sessionID := "pending-delivery", "pending-auto"
	d.PendingDeliveryID, d.PendingSessionID = &delivery, &sessionID
	if err := triggerStore.ReplaceTrigger(d); err != nil {
		t.Fatal(err)
	}
	s := NewServer(cfg, nil, WithLLM(&autonomyV2PromptLLM{}))
	if s.triggerSched != nil {
		s.triggerSched.Stop()
	}
	if s.sessions != nil {
		s.sessions.Stop()
	}
	if s.startupErr != nil || s.triggerSched == nil {
		t.Fatalf("pending startup should keep node running while freezing trigger: err=%v scheduler=%v", s.startupErr, s.triggerSched)
	}
	got, ok := s.triggerStore.GetTrigger(d.TriggerID)
	if !ok || got.PendingDeliveryID == nil || *got.PendingDeliveryID != delivery || !got.RecoveryRequired {
		t.Fatalf("pending trigger was not preserved for recovery: %+v ok=%v", got, ok)
	}
	healthy, ok := s.triggerStore.GetTrigger(triggers.AutoDefaultTriggerID("healthy-auto"))
	if !ok || !healthy.Enabled || healthy.RecoveryRequired || healthy.PendingDeliveryID != nil {
		t.Fatalf("healthy auto trigger was not reconciled: %+v ok=%v", healthy, ok)
	}
	triggerRequest := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/triggers/"+d.TriggerID+"/recover", bytes.NewBufferString(body))
		req.SetPathValue("trigger_id", d.TriggerID)
		rec := httptest.NewRecorder()
		s.handleRecoverTrigger(rec, req)
		return rec
	}
	wrong := triggerRequest(`{"revision":` + fmt.Sprint(d.Revision) + `,"delivery_id":"wrong"}`)
	if wrong.Code != http.StatusConflict {
		t.Fatalf("wrong recovery status=%d body=%s", wrong.Code, wrong.Body)
	}
	recovered := triggerRequest(`{"revision":` + fmt.Sprint(d.Revision) + `,"delivery_id":"` + delivery + `"}`)
	if recovered.Code != http.StatusOK {
		t.Fatalf("recovery status=%d body=%s", recovered.Code, recovered.Body)
	}
	got, _ = s.triggerStore.GetTrigger(d.TriggerID)
	if got.Enabled || got.PendingDeliveryID != nil || got.RecoveryRequired {
		t.Fatalf("recovered auto trigger should remain disabled: %+v", got)
	}
	if err := s.autonomyStore.PutProfile(autonomy.Profile{AgentID: "pending-auto", WakeIntervalSeconds: 60, MaxToolRounds: 4, DreamingTime: "03:00", Timezone: "UTC"}, 1); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/agents/pending-auto/auto-config/reconcile", bytes.NewBufferString(`{}`))
	req.SetPathValue("agent_id", "pending-auto")
	rec := httptest.NewRecorder()
	s.handleAutonomyV2ConfigReconcile(rec, req)
	reconciled := rec
	if reconciled.Code != http.StatusOK {
		t.Fatalf("reconcile after recovery status=%d body=%s", reconciled.Code, reconciled.Body)
	}
	got, _ = s.triggerStore.GetTrigger(d.TriggerID)
	if !got.Enabled || got.RecoveryRequired || got.PendingDeliveryID != nil {
		t.Fatalf("reconciled auto trigger=%+v", got)
	}
	s.Close()
	if s.agents != nil {
		_ = s.agents.Close()
	}
}
