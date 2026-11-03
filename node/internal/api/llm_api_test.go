package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func testConfigDeepSeek(t *testing.T) *config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-v4-pro"
	return cfg
}

func TestLLMSettingsExposesCurrentProfileCapability(t *testing.T) {
	srv := NewServer(testConfigDeepSeek(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/llm/settings")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var view llm.LLMSettingsView
	if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if view.Model != "deepseek-v4-pro" || !view.ThinkingSupported || view.ThinkingControl != llm.ThinkingControlEffort {
		t.Fatalf("settings = %+v", view)
	}

	capResp, err := http.Get(ts.URL + "/v1/llm/capabilities?provider=openai&model=gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	defer capResp.Body.Close()
	var capability map[string]any
	if err := json.NewDecoder(capResp.Body).Decode(&capability); err != nil {
		t.Fatal(err)
	}
	if capability["supported"] != false {
		t.Fatalf("unknown capability = %#v", capability)
	}
}

func TestLLMSettingsPatchCannotSetThinkingGlobally(t *testing.T) {
	srv := NewServer(testConfigDeepSeek(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	for _, body := range []string{`{"thinking":"disabled"}`, `{"active_profile":"default","thinking":"disabled"}`} {
		req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/llm/settings", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("body %s status = %d", body, resp.StatusCode)
		}
	}
}

func TestLLMSettingsActiveProfileRequiresStores(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfigDeepSeek(t)
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := `{"active_profile":"another-profile"}`
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/llm/settings", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want fail-closed 503", resp.StatusCode)
	}
	if cfg.LLM.ActiveProfileID() != "default" {
		t.Fatalf("active profile changed without stores: %q", cfg.LLM.ActiveProfileID())
	}
	reloaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LLM.ActiveProfileID() != "default" {
		t.Fatalf("active profile persisted to YAML without stores: %q", reloaded.LLM.ActiveProfileID())
	}
}
