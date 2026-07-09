package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/setup"
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
	if got.LLM.Model != "deepseek-chat" {
		t.Fatalf("llm = %+v", got.LLM)
	}

	patchBody := []byte(`{"llm":{"provider":"mock","model":"mock","mock":true}}`)
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
	if !patched.RestartRequired || patched.LLM.Provider != "mock" {
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

func TestHandlePatchSetupConfig_noConfigPath(t *testing.T) {
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := []byte(`{"llm":{"provider":"mock","model":"mock","mock":true}}`)
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
