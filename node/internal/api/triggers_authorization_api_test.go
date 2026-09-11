package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

func triggerAPIRequest(t *testing.T, method, url string, body any) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return resp, data
}

func createAuthorizedTestTrigger(t *testing.T, srvURL, sessionID string) triggers.Definition {
	t.Helper()
	resp, data := triggerAPIRequest(t, http.MethodPost, srvURL+"/v1/triggers", map[string]any{
		"name":                "authorized-trigger",
		"task_template":       "initial task",
		"condition":           map[string]any{"interval_seconds": 60},
		"target_agent_id":     "ops-linux-01",
		"target_session_id":   sessionID,
		"session_target_mode": "fixed",
		"enabled":             true,
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", resp.StatusCode, data)
	}
	var def triggers.Definition
	if err := json.Unmarshal(data, &def); err != nil {
		t.Fatal(err)
	}
	if def.TriggerID == "" || def.CreatedBy != "local" {
		t.Fatalf("unexpected created definition: %+v", def)
	}
	return def
}

func TestTriggersAuthorizationAPIMissingTargetRejected(t *testing.T) {
	_, ts := newTriggersTestServer(t)
	defer ts.Close()
	resp, data := triggerAPIRequest(t, http.MethodPost, ts.URL+"/v1/triggers", map[string]any{
		"name":              "missing-target",
		"task_template":     "task",
		"condition":         map[string]any{"interval_seconds": 60},
		"target_session_id": "session-fixed",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing target status=%d body=%s", resp.StatusCode, data)
	}
}

func TestTriggersAuthorizationAPIRevisionAndPatch(t *testing.T) {
	srv, ts := newTriggersTestServer(t)
	defer ts.Close()
	def := createAuthorizedTestTrigger(t, ts.URL, createTestRuntime(t, srv))

	name := "patched"
	task := "patched task"
	target := "ops-linux-01"
	session := "session-updated"
	client := "client-updated"
	mode := "fixed"
	patch := map[string]any{
		"revision":            def.Revision,
		"name":                name,
		"task_template":       task,
		"condition":           map[string]any{"interval_seconds": 120},
		"target_agent_id":     target,
		"target_session_id":   session,
		"client_id":           client,
		"enabled":             false,
		"session_target_mode": mode,
	}
	resp, data := triggerAPIRequest(t, http.MethodPatch, ts.URL+"/v1/triggers/"+def.TriggerID, patch)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", resp.StatusCode, data)
	}
	var updated triggers.Definition
	if err := json.Unmarshal(data, &updated); err != nil {
		t.Fatal(err)
	}
	if updated.Name != name || updated.TaskTemplate != task || updated.TargetAgentID != target || updated.OwnerAgentID != def.OwnerAgentID || updated.Enabled {
		t.Fatalf("patch did not apply safely: before=%+v after=%+v", def, updated)
	}
	if updated.TargetSessionID == nil || *updated.TargetSessionID != session || updated.ClientID == nil || *updated.ClientID != client || updated.Revision == def.Revision {
		t.Fatalf("patch fields/revision not applied: %+v", updated)
	}

	resp, data = triggerAPIRequest(t, http.MethodPatch, ts.URL+"/v1/triggers/"+def.TriggerID, map[string]any{"revision": def.Revision, "name": "stale"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale revision status=%d body=%s", resp.StatusCode, data)
	}
}

func TestTriggersAuthorizationAPIMaintenanceAndDeleteRevision(t *testing.T) {
	srv, ts := newTriggersTestServer(t)
	defer ts.Close()
	def := createAuthorizedTestTrigger(t, ts.URL, createTestRuntime(t, srv))
	resp, data := triggerAPIRequest(t, http.MethodDelete, ts.URL+"/v1/triggers/"+def.TriggerID+"?revision=1junk", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid delete revision status=%d body=%s", resp.StatusCode, data)
	}
	def.Controller = "maintenance"
	if err := srv.triggerStore.ReplaceTrigger(def); err != nil {
		t.Fatal(err)
	}
	resp, data = triggerAPIRequest(t, http.MethodPatch, ts.URL+"/v1/triggers/"+def.TriggerID, map[string]any{"name": "blocked"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("maintenance patch status=%d body=%s", resp.StatusCode, data)
	}
	resp, data = triggerAPIRequest(t, http.MethodDelete, ts.URL+"/v1/triggers/"+def.TriggerID, nil)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("maintenance delete status=%d body=%s", resp.StatusCode, data)
	}
}
