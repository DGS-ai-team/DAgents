package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func triggersTestConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.Triggers.Enabled = true
	return cfg
}

func newTriggersTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := triggersTestConfig(t)
	reg, err := tools.NewRegistry(cfg.FSRoot, 30)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithTools(reg), WithSkipStore()).Handler())
}

func TestTriggersAPICreateFireHistory(t *testing.T) {
	ts := newTriggersTestServer(t)
	defer ts.Close()

	createSess, err := http.Post(ts.URL+"/v1/sessions", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	var sess createSessionResponse
	if err := json.NewDecoder(createSess.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	createSess.Body.Close()

	createBody := map[string]any{
		"name":              "smoke",
		"task_template":     "hello {reason}",
		"condition":         map[string]any{"interval_seconds": 3600},
		"target_session_id": sess.SessionID,
	}
	raw, _ := json.Marshal(createBody)
	createResp, err := http.Post(ts.URL+"/v1/triggers", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	createBytes, _ := io.ReadAll(createResp.Body)
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createResp.StatusCode, createBytes)
	}
	var created struct {
		TriggerID string `json:"trigger_id"`
	}
	if err := json.Unmarshal(createBytes, &created); err != nil {
		t.Fatal(err)
	}
	if created.TriggerID == "" {
		t.Fatal("empty trigger_id")
	}

	fireResp, err := http.Post(ts.URL+"/v1/triggers/"+created.TriggerID+"/fire", "application/json", bytes.NewReader([]byte(`{"reason":"smoke"}`)))
	if err != nil {
		t.Fatal(err)
	}
	fireBytes, _ := io.ReadAll(fireResp.Body)
	fireResp.Body.Close()
	if fireResp.StatusCode != http.StatusOK {
		t.Fatalf("fire status=%d body=%s", fireResp.StatusCode, fireBytes)
	}
	var record struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(fireBytes, &record); err != nil {
		t.Fatal(err)
	}
	if record.Status != "queued" {
		t.Fatalf("fire status=%q body=%s", record.Status, fireBytes)
	}

	time.Sleep(100 * time.Millisecond)
	histResp, err := http.Get(ts.URL + "/v1/triggers/" + created.TriggerID + "/history")
	if err != nil {
		t.Fatal(err)
	}
	defer histResp.Body.Close()
	if histResp.StatusCode != http.StatusOK {
		t.Fatalf("history status=%d", histResp.StatusCode)
	}
	var hist struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.NewDecoder(histResp.Body).Decode(&hist); err != nil {
		t.Fatal(err)
	}
	if len(hist.Records) == 0 {
		t.Fatal("expected fire history records")
	}
}
