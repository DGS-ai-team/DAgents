package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testConfigDeepSeek(t *testing.T) *config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-chat"
	return cfg
}

func TestHandleLLMSettingsGetPatch(t *testing.T) {
	srv := NewServer(testConfigDeepSeek(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/llm/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got llm.LLMSettingsView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Model != "deepseek-chat" || !got.ThinkingSupported || got.Thinking != "enabled" {
		t.Fatalf("unexpected settings: %+v", got)
	}
	if got.ThinkingControl != llm.ThinkingControlEffort || got.ThinkingSecondaryLabel != "推理强度" {
		t.Fatalf("unexpected thinking controls: %+v", got)
	}

	patchBody := []byte(`{"thinking":"disabled"}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/llm/settings", bytes.NewReader(patchBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	patchResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer patchResp.Body.Close()
	if patchResp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d", patchResp.StatusCode)
	}
	var patched llm.LLMSettingsView
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatal(err)
	}
	if patched.Thinking != "disabled" {
		t.Fatalf("patched = %+v", patched)
	}

	infoResp, err := http.Get(ts.URL + "/v1/agent/info")
	if err != nil {
		t.Fatal(err)
	}
	defer infoResp.Body.Close()
	var info agentInfoResponse
	if err := json.NewDecoder(infoResp.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.LLM.Thinking != "disabled" || info.LLM.Model != "deepseek-chat" {
		t.Fatalf("agent info llm = %+v", info.LLM)
	}
}
