package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHandleAgentUpgradeReadiness_idle(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/agent/upgrade-readiness")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["ready"] != true || body["has_active_turn"] != false {
		t.Fatalf("body = %#v", body)
	}
}
