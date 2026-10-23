package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestHandleAgentUpgradeReadiness_idle(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/node/upgrade-readiness")
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

func TestHandleAgentUpgradeReadiness_beforeOnboardingIsLocalAndLive(t *testing.T) {
	cfg := testConfig(t)
	cfg.Onboarding.NodeProfileCompleted = false
	srv := NewServer(cfg, nil, WithLLM(&llm.MockClient{}), WithSkipStore())

	localReq := httptest.NewRequest(http.MethodGet, "/v1/node/upgrade-readiness", nil)
	localReq.RemoteAddr = "127.0.0.1:43210"
	localRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(localRec, localReq)
	if localRec.Code != http.StatusOK {
		t.Fatalf("loopback status = %d body=%s; want 200", localRec.Code, localRec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(localRec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := srv.sessions.UpgradeReadiness()
	if got["ready"] != want.Ready || got["has_active_turn"] != want.HasActiveTurn ||
		got["active_turn_count"] != float64(want.ActiveTurnCount) {
		t.Fatalf("readiness response = %#v, want live session state %#v", got, want)
	}

	remoteReq := httptest.NewRequest(http.MethodGet, "/v1/node/upgrade-readiness", nil)
	remoteReq.RemoteAddr = "192.0.2.10:43210"
	remoteRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(remoteRec, remoteReq)
	if remoteRec.Code != http.StatusForbidden {
		t.Fatalf("remote status = %d body=%s; want 403", remoteRec.Code, remoteRec.Body.String())
	}
}
