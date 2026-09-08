package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

type p0TriggerSubmitter struct{ submits int }

func (s *p0TriggerSubmitter) EnsureSession(id string) (string, error) { return id, nil }
func (s *p0TriggerSubmitter) SubmitTriggerMessage(id, trigger, content string) error {
	s.submits++
	return nil
}

func TestP0TriggerCreateDisabledIsAtomic(t *testing.T) {
	srv, ts := newTriggersTestServer(t)
	defer ts.Close()
	sessionID := createTestRuntime(t, srv)
	body := []byte(`{"name":"disabled","task_template":"hello","condition":{"interval_seconds":60},"target_agent_id":"ops-linux-01","target_session_id":"` + sessionID + `","session_target_mode":"fixed","enabled":false}`)
	resp, err := http.Post(ts.URL+"/v1/triggers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var def triggers.Definition
	if err := json.NewDecoder(resp.Body).Decode(&def); err != nil {
		t.Fatal(err)
	}
	if def.Enabled {
		t.Fatal("disabled create became enabled")
	}
}

func TestP0RecoveryRequiresExactDeliveryAndLeavesDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "triggers.json")
	store, err := triggers.OpenStore(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	def, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{Name: "recover", Condition: map[string]any{"interval_seconds": 60}, TargetAgentID: "node", TargetSessionID: strPtr("session"), TaskTemplate: "hello", Enabled: boolPtr(false)}, "node", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	created, err := store.CreateTrigger(def)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimDelivery(created.TriggerID, "delivery-1", "session"); err != nil {
		t.Fatal(err)
	}
	reloaded, err := triggers.OpenStore(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := reloaded.RecoverPendingDelivery(created.TriggerID, "wrong"); err == nil {
		t.Fatal("wrong delivery recovered")
	}
	if err := reloaded.RecoverPendingDelivery(created.TriggerID, "delivery-1"); err != nil {
		t.Fatal(err)
	}
	got, ok := reloaded.GetTrigger(created.TriggerID)
	if !ok || got.Enabled || got.PendingDeliveryID != nil {
		t.Fatalf("unexpected recovered trigger: %+v", got)
	}
}

func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }

func TestP0TriggerAPIRouteExists(t *testing.T) {
	cfg := triggersTestConfig(t)
	reg, err := tools.NewRegistry(cfg.RuntimeDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithTools(reg), WithSkipStore())
	req := httptest.NewRequest(http.MethodPost, "/v1/triggers/missing/recover", bytes.NewBufferString(`{"delivery_id":"x"}`))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("recover route missing")
	}
}

func TestP0RecoveryHTTPFencesDeliveryAndEnable(t *testing.T) {
	srv, ts := newTriggersTestServer(t)
	defer ts.Close()
	sessionID := createTestRuntime(t, srv)
	body := []byte(`{"name":"recover-api","task_template":"hello","condition":{"interval_seconds":60},"target_agent_id":"ops-linux-01","target_session_id":"` + sessionID + `","session_target_mode":"fixed","enabled":false}`)
	resp, err := http.Post(ts.URL+"/v1/triggers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var def triggers.Definition
	if err := json.NewDecoder(resp.Body).Decode(&def); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := srv.triggerStore.ClaimDelivery(def.TriggerID, "delivery-api", sessionID); err != nil {
		t.Fatal(err)
	}
	reloaded, err := triggers.OpenStore(srv.cfg.TriggersStorePath(), 200)
	if err != nil {
		t.Fatal(err)
	}
	srv.triggerSched.Stop()
	srv.triggerStore = reloaded
	submitter := &p0TriggerSubmitter{}
	srv.triggerSched = triggers.NewScheduler(reloaded, submitter, 1)
	wrong, err := http.Post(ts.URL+"/v1/triggers/"+def.TriggerID+"/recover", "application/json", bytes.NewBufferString(`{"delivery_id":"wrong"}`))
	if err != nil {
		t.Fatal(err)
	}
	if wrong.StatusCode != http.StatusConflict {
		t.Fatalf("wrong recovery status=%d", wrong.StatusCode)
	}
	wrong.Body.Close()
	get, err := http.Get(ts.URL + "/v1/triggers/" + def.TriggerID)
	if err != nil || get.StatusCode != http.StatusOK {
		t.Fatalf("get status=%v", get.StatusCode)
	}
	var pending triggers.Definition
	_ = json.NewDecoder(get.Body).Decode(&pending)
	get.Body.Close()
	if pending.PendingDeliveryID == nil || *pending.PendingDeliveryID != "delivery-api" {
		t.Fatal("wrong recovery cleared pending delivery")
	}
	patchReq, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/triggers/"+def.TriggerID, bytes.NewBufferString(`{"enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	patchReq.Header.Set("Content-Type", "application/json")
	patched, err := http.DefaultClient.Do(patchReq)
	if err != nil {
		t.Fatal(err)
	}
	if patched.StatusCode != http.StatusBadRequest {
		t.Fatal("recovery pending trigger was enabled")
	}
	patched.Body.Close()
	forced, err := http.Post(ts.URL+"/v1/triggers/"+def.TriggerID+"/fire", "application/json", bytes.NewBufferString(`{"force":true}`))
	if err != nil || forced.StatusCode != http.StatusOK {
		t.Fatalf("force status=%v", forced.StatusCode)
	}
	var fired triggers.FireRecord
	if err := json.NewDecoder(forced.Body).Decode(&fired); err != nil {
		t.Fatal(err)
	}
	forced.Body.Close()
	if fired.Status != triggers.FireStatusSkipped || submitter.submits != 0 {
		t.Fatal("force fire queued during recovery")
	}
	ok, err := http.Post(ts.URL+"/v1/triggers/"+def.TriggerID+"/recover", "application/json", bytes.NewBufferString(`{"delivery_id":"delivery-api"}`))
	if err != nil {
		t.Fatal(err)
	}
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("recovery status=%d", ok.StatusCode)
	}
	ok.Body.Close()
	get, err = http.Get(ts.URL + "/v1/triggers/" + def.TriggerID)
	if err != nil || get.StatusCode != http.StatusOK {
		t.Fatalf("recovered get status=%v", get.StatusCode)
	}
	var recovered triggers.Definition
	if err := json.NewDecoder(get.Body).Decode(&recovered); err != nil {
		t.Fatal(err)
	}
	get.Body.Close()
	if recovered.Enabled || recovered.PendingDeliveryID != nil {
		t.Fatalf("recovered trigger not disabled: %+v", recovered)
	}
}
