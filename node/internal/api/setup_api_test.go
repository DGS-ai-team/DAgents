package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestHandleSetupConfigGetPatch(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-chat"
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/setup/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got setup.SettingsView
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.ConfigPath != configPath || !got.ConfigWritable {
		t.Fatalf("config meta = path %q writable %v", got.ConfigPath, got.ConfigWritable)
	}
	if len(got.LLM.Profiles) == 0 || got.LLM.Profiles[0].Model != "deepseek-chat" {
		t.Fatalf("llm = %+v", got.LLM)
	}

	patchBody := []byte(`{"llm":{"profiles":[{"id":"default","provider":"mock","model":"mock","mock":true}]}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/setup/config", bytes.NewReader(patchBody))
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
	var patched setup.SettingsView
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatal(err)
	}
	if !patched.RestartRequired || len(patched.LLM.Profiles) == 0 || patched.LLM.Profiles[0].Provider != "mock" {
		t.Fatalf("patched = %+v", patched)
	}

	reloaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.LLM.Provider != "mock" || !reloaded.LLM.Mock {
		t.Fatalf("file llm = %+v", reloaded.LLM)
	}

	compBody := []byte(`{"compression":{"silent_trigger_tokens":60000,"blocking_trigger_tokens":90000}}`)
	compReq, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/setup/config", bytes.NewReader(compBody))
	if err != nil {
		t.Fatal(err)
	}
	compReq.Header.Set("Content-Type", "application/json")
	compResp, err := http.DefaultClient.Do(compReq)
	if err != nil {
		t.Fatal(err)
	}
	defer compResp.Body.Close()
	if compResp.StatusCode != http.StatusOK {
		t.Fatalf("compression patch status = %d", compResp.StatusCode)
	}
	reloaded2, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded2.Compression.SilentTriggerTokens != 60000 || reloaded2.Compression.BlockingTriggerTokens != 90000 {
		t.Fatalf("file compression = %+v", reloaded2.Compression)
	}
}

func TestHandlePatchSetupConfigBrowserRuntimeLifecycle(t *testing.T) {
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/browser/call" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer service.Close()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	cfg.Browser.ServiceURL = service.URL
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)
	patch := []byte(`{"features":{"browser_enabled":true}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/setup/config", bytes.NewReader(patch))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("enable status=%d body=%s", resp.StatusCode, raw)
	}
	if srv.browserManager() == nil || !srv.browserManager().Enabled() {
		t.Fatal("browser manager was not installed")
	}
	if !hasToolDefinition(srv.tools.Definitions(), "browser_run_task") {
		t.Fatal("default registry missing browser_run_task after enable")
	}
	if !hasToolDefinition(srv.sessions.SessionTools(sessionID).Definitions(), "browser_run_task") {
		t.Fatal("loaded session registry missing browser_run_task after enable")
	}

	disablePatch := []byte(`{"features":{"browser_enabled":false}}`)
	disableReq, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/setup/config", bytes.NewReader(disablePatch))
	if err != nil {
		t.Fatal(err)
	}
	disableReq.Header.Set("Content-Type", "application/json")
	disableResp, err := http.DefaultClient.Do(disableReq)
	if err != nil {
		t.Fatal(err)
	}
	defer disableResp.Body.Close()
	if disableResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(disableResp.Body)
		t.Fatalf("disable status=%d body=%s", disableResp.StatusCode, raw)
	}
	if srv.browserManager() != nil {
		t.Fatal("browser manager remained installed after disable")
	}
	if hasToolDefinition(srv.tools.Definitions(), "browser_run_task") {
		t.Fatal("default registry retained browser_run_task after disable")
	}
	if hasToolDefinition(srv.sessions.SessionTools(sessionID).Definitions(), "browser_run_task") {
		t.Fatal("loaded session registry retained browser_run_task after disable")
	}
}

func TestHandlePatchSetupConfigBrowserFailsBeforePersisting(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	cfg.Browser.ServiceURL = "http://127.0.0.1:1"
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	patch := []byte(`{"features":{"browser_enabled":true}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/setup/config", bytes.NewReader(patch))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	if cfg.BrowserEnabled() {
		t.Fatal("browser config was enabled after failed service preflight")
	}
	reloaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.BrowserEnabled() {
		t.Fatal("persisted browser config after failed service preflight")
	}
}

func hasToolDefinition(defs []tools.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Function.Name == name {
			return true
		}
	}
	return false
}

func TestHandlePatchSetupConfig_noConfigPath(t *testing.T) {
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := []byte(`{"llm":{"profiles":[{"id":"default","provider":"mock","model":"mock","mock":true}]}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/setup/config", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestConfigPathWritable(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "new-config.yaml")
	if !configPathWritable(missing) {
		t.Fatal("expected writable for creatable path")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("probe should not leave file")
	}

	existing := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(existing, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !configPathWritable(existing) {
		t.Fatal("expected writable existing file")
	}
}

func TestProbeLLMModels(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"m1"},{"id":"m2"}]}`))
	}))
	defer upstream.Close()

	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body, _ := json.Marshal(map[string]any{
		"base_url": upstream.URL + "/v1",
		"api_key":  "sk-x",
		"provider": "openai",
	})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/setup/llm/probe-models", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	var got struct {
		Models []struct {
			ID string `json:"id"`
		} `json:"models"`
		SuggestedProvider string `json:"suggested_provider"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Models) != 2 || got.Models[0].ID != "m1" {
		t.Fatalf("got=%+v", got)
	}
}
