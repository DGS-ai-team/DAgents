package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestHandleSetupConfigGetPatch(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "synthetic-setup-test-key")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	cfg.LLM.Provider = "deepseek"
	cfg.LLM.Model = "deepseek-chat"
	cfg.LLM.Profiles = nil
	cfg.ApplyDefaults()
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	llmConfigs := openTestLLMConfigStore(t)
	if err := store.EnsureDefaultLLMConfig(t.Context(), llmConfigs, cfg); err != nil {
		t.Fatal(err)
	}
	nodeSettings, err := store.OpenNodeSettings(filepath.Join(dir, "node_settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeSettings.Close()
	if err := nodeSettings.Save(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath), WithNodeSettings(nodeSettings))
	srv.llmConfigs = llmConfigs
	t.Cleanup(func() { srv.Close(); _ = llmConfigs.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/settings")
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
	if got.Features.RawMessageHistorySource != "settings" {
		t.Fatalf("raw history source = %q", got.Features.RawMessageHistorySource)
	}

	patchBody := []byte(`{"llm":{"profiles":[{"id":"default","provider":"mock","model":"mock","mock":true}]}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", bytes.NewReader(patchBody))
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
	if patched.RestartRequired || len(patched.LLM.Profiles) == 0 || patched.LLM.Profiles[0].Provider != "mock" {
		t.Fatalf("patched = %+v", patched)
	}

	reloaded, err := llmConfigs.Get(t.Context(), "default")
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Provider != "mock" || !reloaded.Mock {
		t.Fatalf("stored llm = %+v", reloaded)
	}

	compBody := []byte(`{"compression":{"silent_trigger_tokens":60000,"blocking_trigger_tokens":90000}}`)
	compReq, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", bytes.NewReader(compBody))
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
	reloaded2, err := nodeSettings.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded2.Compression.SilentTriggerTokens != 60000 || reloaded2.Compression.BlockingTriggerTokens != 90000 {
		t.Fatalf("stored compression = %+v", reloaded2.Compression)
	}

	zeroToolsReq := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(`{"tools":{"bash_compress_max_output_chars":0}}`))
	zeroToolsRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(zeroToolsRec, zeroToolsReq)
	if zeroToolsRec.Code != http.StatusBadRequest {
		t.Fatalf("zero output limit status = %d body=%s", zeroToolsRec.Code, zeroToolsRec.Body.String())
	}

	resetReq := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(`{"tools":{"reset_bash_compress_limits":true}}`))
	resetRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("reset limits status = %d body=%s", resetRec.Code, resetRec.Body.String())
	}
	var resetView setup.SettingsView
	if err := json.NewDecoder(resetRec.Body).Decode(&resetView); err != nil {
		t.Fatal(err)
	}
	if resetView.Tools.BashCompressMaxOutputChars != config.DefaultBashCompressMaxOutputChars || resetView.Tools.BashCompressMaxStderrChars != config.DefaultBashCompressMaxOutputCharsStderr {
		t.Fatalf("reset limits = %+v", resetView.Tools)
	}
}

func TestHandleSetupFeaturePatchPreservesOmittedGatesAndAppliesExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	cfg.Skills.Enabled = true
	cfg.Triggers.Enabled = true
	cfg.ChildAgents.Enabled = true
	multimodal := true
	cfg.Multimodal.Enabled = &multimodal
	cfg.SyncActiveProfileMultimodalFromFlat()
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	nodeSettings, llmConfigs := openSetupStores(t, dir, cfg)
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath), WithNodeSettings(nodeSettings))
	srv.llmConfigs = llmConfigs
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	patch := func(body string) setup.SettingsView {
		t.Helper()
		req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", strings.NewReader(body))
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
			t.Fatalf("patch status=%d body=%s", resp.StatusCode, raw)
		}
		var view setup.SettingsView
		if err := json.NewDecoder(resp.Body).Decode(&view); err != nil {
			t.Fatal(err)
		}
		return view
	}

	omitted := patch(`{"features":{"skills_max_in_prompt":12}}`)
	if !omitted.Features.SkillsEnabled || !omitted.Features.TriggersEnabled || !omitted.Features.ChildAgentsEnabled || !omitted.Features.MultimodalEnabled {
		t.Fatalf("omitted gates changed: %+v", omitted.Features)
	}

	explicitFalse := patch(`{"features":{"skills_enabled":false,"triggers_enabled":false,"child_agents_enabled":false,"multimodal_enabled":false}}`)
	if explicitFalse.Features.SkillsEnabled || explicitFalse.Features.TriggersEnabled || explicitFalse.Features.ChildAgentsEnabled || explicitFalse.Features.MultimodalEnabled {
		t.Fatalf("explicit false gates not returned: %+v", explicitFalse.Features)
	}
	stored, err := nodeSettings.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if stored == nil || stored.Skills.Enabled || stored.Triggers.Enabled || stored.ChildAgents.Enabled || stored.MultimodalEnabled() {
		t.Fatalf("explicit false gates not persisted: %+v", stored)
	}
	profile, err := llmConfigs.Get(t.Context(), cfg.LLM.ActiveProfileID())
	if err != nil {
		t.Fatal(err)
	}
	if profile.MultimodalEnabled {
		t.Fatalf("explicit false multimodal setting not persisted to active profile: %+v", profile)
	}
}

func TestHandleSetupRawHistoryEnvironmentOverrideRejectsPatchAtomically(t *testing.T) {
	tests := []struct {
		name          string
		envValue      string
		expectedValue bool
		patchValue    bool
	}{
		{name: "environment true", envValue: "true", expectedValue: true, patchValue: false},
		{name: "environment false", envValue: "false", expectedValue: false, patchValue: true},
		{name: "invalid value defaults true but remains controlled", envValue: "not-a-bool", expectedValue: true, patchValue: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.EnvRawMessageHistoryEnabled, tt.envValue)
			dir := t.TempDir()
			cfg := testConfig(t)
			configuredRawHistory := false
			cfg.RawMessageHistory.Enabled = &configuredRawHistory
			if err := config.SaveFile(filepath.Join(dir, "config.yaml"), cfg); err != nil {
				t.Fatal(err)
			}
			nodeSettings, llmConfigs := openSetupStores(t, dir, cfg)
			srv := NewServer(cfg, nil, WithSkipStore(), WithNodeSettings(nodeSettings))
			srv.llmConfigs = llmConfigs
			t.Cleanup(func() { srv.Close() })
			recorder := httptest.NewRecorder()
			body := fmt.Sprintf(`{"features":{"raw_message_history_enabled":%t,"skills_max_in_prompt":77},"tools":{"bash_output_encoding":"should-not-save"}}`, tt.patchValue)
			req := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(body))
			srv.Handler().ServeHTTP(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("controlled raw-history PATCH status = %d, want %d: %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			var apiErr struct {
				Error struct {
					Code    string         `json:"code"`
					Message string         `json:"message"`
					Details map[string]any `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &apiErr); err != nil {
				t.Fatal(err)
			}
			if apiErr.Error.Code != "setting_controlled_by_environment" || !strings.Contains(apiErr.Error.Message, config.EnvRawMessageHistoryEnabled) || apiErr.Error.Details["source"] != "environment" {
				t.Fatalf("controlled setting error = %+v", apiErr)
			}

			getRec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(getRec, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
			var view setup.SettingsView
			if err := json.Unmarshal(getRec.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
			if view.Features.RawMessageHistoryEnabled != tt.expectedValue || view.Features.RawMessageHistorySource != "environment" {
				t.Fatalf("effective raw-history view = %+v, want enabled=%v source=environment", view.Features, tt.expectedValue)
			}
			if view.Features.SkillsMaxInPrompt != cfg.Skills.MaxInPrompt || view.Tools.BashOutputEncoding != cfg.Tools.BashOutputEncoding {
				t.Fatalf("failed PATCH partially changed settings: skills max=%d, output encoding=%q", view.Features.SkillsMaxInPrompt, view.Tools.BashOutputEncoding)
			}
			stored, err := nodeSettings.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if stored == nil || stored.Skills.MaxInPrompt != cfg.Skills.MaxInPrompt || stored.Tools.BashOutputEncoding != cfg.Tools.BashOutputEncoding || stored.RawMessageHistory.Enabled == nil || *stored.RawMessageHistory.Enabled {
				t.Fatalf("failed PATCH changed persisted settings: %+v", stored)
			}
		})
	}
}

func TestHandleSetupConfigCompletesNodeProfileWithoutPreferredNameOrLLMKey(t *testing.T) {
	const missingKeyEnv = "DAGENTS_TEST_SETUP_MISSING_LLM_KEY"
	t.Setenv(missingKeyEnv, "")
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	cfg.Agent.Name = ""
	cfg.User.PreferredName = ""
	cfg.Onboarding.NodeProfileCompleted = false
	cfg.LLM.Mock = false
	cfg.LLM.APIKeyEnv = missingKeyEnv
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	nodeSettings, llmConfigs := openSetupStores(t, dir, cfg)
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath), WithNodeSettings(nodeSettings))
	srv.llmConfigs = llmConfigs
	t.Cleanup(func() { srv.Close() })
	req := httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader([]byte(
		`{"agent":{"name":"local-node"},"onboarding":{"node_profile_completed":true}}`,
	)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !cfg.NodeProfileCompleted() || cfg.Agent.Name != "local-node" || cfg.PreferredName() != "" {
		t.Fatalf("config after Node-only setup: completed=%v agent=%+v user=%+v", cfg.NodeProfileCompleted(), cfg.Agent, cfg.User)
	}
	if cfg.LLM.Mock {
		t.Fatal("fixture unexpectedly configured a mock model")
	}
}

func TestSettingsNodeTokenIsRedactedAndWriteOnly(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	workgroupEnabled := false
	cfg.Manage.Enabled = true
	cfg.Manage.URL = "https://manage.example.test"
	cfg.Manage.Registration.Team = "keep-team"
	cfg.Manage.Registration.BaseURL = "https://register.example.test"
	cfg.Manage.Registration.IntervalSeconds = 90
	cfg.Manage.Registration.TTLSeconds = 180
	cfg.Manage.Workgroup.Enabled = &workgroupEnabled
	cfg.Manage.NodeToken = "manage-node-secret"
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	nodeSettings, llmConfigs := openSetupStores(t, dir, cfg)
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath), WithNodeSettings(nodeSettings))
	srv.llmConfigs = llmConfigs
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/v1/settings")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || bytes.Contains(body, []byte("manage-node-secret")) || bytes.Contains(body, []byte(`"local_endpoint"`)) {
		t.Fatalf("GET settings status=%d leaked forbidden field: %s", resp.StatusCode, body)
	}
	var view setup.SettingsView
	if err := json.Unmarshal(body, &view); err != nil {
		t.Fatal(err)
	}
	if !view.Manage.HasNodeToken {
		t.Fatalf("redacted token view = %+v", view.Manage)
	}

	patch := func(body string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", bytes.NewReader([]byte(body)))
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
			raw, _ := io.ReadAll(patchResp.Body)
			t.Fatalf("PATCH status=%d body=%s", patchResp.StatusCode, raw)
		}
	}
	patch(`{"manage":{"node_token":""}}`)
	reloaded, err := nodeSettings.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Manage.NodeToken != "manage-node-secret" {
		t.Fatal("blank write-only token replaced the stored credential")
	}
	assertManageSettingsPreserved(t, reloaded)
	patch(`{"manage":{"clear_node_token":true}}`)
	reloaded, err = nodeSettings.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Manage.NodeToken != "" {
		t.Fatal("explicit clear did not remove the stored credential")
	}
	assertManageSettingsPreserved(t, reloaded)
}

func assertManageSettingsPreserved(t *testing.T, cfg *config.Config) {
	t.Helper()
	if !cfg.Manage.Enabled || cfg.Manage.URL != "https://manage.example.test" || cfg.Manage.Registration.Team != "keep-team" || cfg.Manage.Registration.BaseURL != "https://register.example.test" || cfg.Manage.Registration.IntervalSeconds != 90 || cfg.Manage.Registration.TTLSeconds != 180 || cfg.Manage.Workgroup.Enabled == nil || *cfg.Manage.Workgroup.Enabled {
		t.Fatalf("token-only settings patch changed Manage configuration: %+v", cfg.Manage)
	}
}

func TestHandlePatchSetupConfigBrowserRuntimeLifecycle(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	nodeSettings, llmConfigs := openSetupStores(t, dir, cfg)
	var factoryEndpoint string
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath), WithNodeSettings(nodeSettings), func(o *serverOptions) {
		o.browserRuntimeFactory = func(cfg *config.Config) (*browser.Manager, *browser.ServiceProcess, error) {
			factoryEndpoint = cfg.Browser.ServiceURL
			manager, err := browser.NewManager(cfg, &browser.MockDriver{})
			return manager, nil, err
		}
	})
	srv.llmConfigs = llmConfigs
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sessionID := createTestRuntime(t, srv)
	patch := []byte(`{"features":{"browser_enabled":true}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", bytes.NewReader(patch))
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
	var enabledView setup.SettingsView
	if err := json.NewDecoder(resp.Body).Decode(&enabledView); err != nil {
		t.Fatal(err)
	}
	if enabledView.RestartRequired {
		t.Fatal("successfully applied browser change should not require restart")
	}
	if srv.browserManager() == nil || !srv.browserManager().Enabled() {
		t.Fatal("browser manager was not installed")
	}
	if factoryEndpoint != config.DefaultBrowserServiceURL {
		t.Fatalf("browser runtime endpoint = %q, want fixed local %q", factoryEndpoint, config.DefaultBrowserServiceURL)
	}
	if !hasToolDefinition(srv.tools.Definitions(), "browser_call") || !hasToolDefinition(srv.tools.Definitions(), "browser_evaluate") {
		t.Fatal("default registry missing browser tools after enable")
	}
	if !hasToolDefinition(srv.sessions.SessionTools(sessionID).Definitions(), "browser_call") || !hasToolDefinition(srv.sessions.SessionTools(sessionID).Definitions(), "browser_evaluate") {
		t.Fatal("loaded session registry missing browser tools after enable")
	}

	disablePatch := []byte(`{"features":{"browser_enabled":false}}`)
	disableReq, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", bytes.NewReader(disablePatch))
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
	var disabledView setup.SettingsView
	if err := json.NewDecoder(disableResp.Body).Decode(&disabledView); err != nil {
		t.Fatal(err)
	}
	if disabledView.RestartRequired {
		t.Fatal("successfully applied browser disable should not require restart")
	}
	if srv.browserManager() != nil {
		t.Fatal("browser manager remained installed after disable")
	}
	if hasToolDefinition(srv.tools.Definitions(), "browser_call") || hasToolDefinition(srv.tools.Definitions(), "browser_evaluate") {
		t.Fatal("default registry retained browser tools after disable")
	}
	if hasToolDefinition(srv.sessions.SessionTools(sessionID).Definitions(), "browser_call") || hasToolDefinition(srv.sessions.SessionTools(sessionID).Definitions(), "browser_evaluate") {
		t.Fatal("loaded session registry retained browser tools after disable")
	}
}

func TestHandlePatchSetupConfigBrowserFailsBeforePersisting(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	cfg := testConfig(t)
	cfg.Browser.ServiceURL = "http://127.0.0.1:1"
	multimodalEnabled := true
	cfg.Multimodal.Enabled = &multimodalEnabled
	cfg.SyncActiveProfileMultimodalFromFlat()
	if err := config.SaveFile(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	nodeSettings, llmConfigs := openSetupStores(t, dir, cfg)
	srv := NewServer(cfg, nil, WithSkipStore(), WithConfigPath(configPath), WithNodeSettings(nodeSettings))
	srv.llmConfigs = llmConfigs
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	patch := []byte(`{"features":{"browser_enabled":true,"multimodal_enabled":false}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", bytes.NewReader(patch))
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
	profile, ok := cfg.LLM.GetProfile(cfg.LLM.ActiveProfileID())
	if !ok || !config.ProfileMultimodalEnabled(profile) {
		t.Fatalf("failed browser preflight mutated source profile: %+v", profile)
	}
	reloaded, err := config.LoadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.BrowserEnabled() {
		t.Fatal("persisted browser config after failed service preflight")
	}
}

func TestSetupRestartRequiredIncludesNodeIdentity(t *testing.T) {
	before := testConfig(t)
	after := *before
	after.NodeID = before.NodeID + "-changed"
	if !setupRestartRequired(before, &after, true, true) {
		t.Fatal("changing NodeID must require restart because startup-bound clients retain the prior identity")
	}
	if setupRestartRequired(before, before, true, true) {
		t.Fatal("unchanged config should not require restart")
	}
}

func TestSetupRestartRequiredUsesEffectiveRawHistoryValue(t *testing.T) {
	envTrue, envFalse, envInvalid := "true", "false", "invalid"
	tests := []struct {
		name     string
		envValue *string
		want     bool
	}{
		{name: "environment true masks stored difference", envValue: &envTrue, want: false},
		{name: "environment false masks stored difference", envValue: &envFalse, want: false},
		{name: "invalid environment defaults true and masks stored difference", envValue: &envInvalid, want: false},
		{name: "settings source uses stored effective difference", envValue: nil, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue == nil {
				previous, existed := os.LookupEnv(config.EnvRawMessageHistoryEnabled)
				if err := os.Unsetenv(config.EnvRawMessageHistoryEnabled); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if existed {
						_ = os.Setenv(config.EnvRawMessageHistoryEnabled, previous)
					} else {
						_ = os.Unsetenv(config.EnvRawMessageHistoryEnabled)
					}
				})
			} else {
				t.Setenv(config.EnvRawMessageHistoryEnabled, *tt.envValue)
			}
			before := testConfig(t)
			storedOff, storedOn := false, true
			before.RawMessageHistory.Enabled = &storedOff
			after := *before
			after.RawMessageHistory.Enabled = &storedOn
			got := setupRestartRequired(before, &after, true, true)
			if got != tt.want {
				t.Fatalf("restart required = %v, want %v", got, tt.want)
			}
		})
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

func openSetupStores(t *testing.T, dir string, cfg *config.Config) (*store.NodeSettingsStore, *store.LLMConfigStore) {
	t.Helper()
	nodeSettings, err := store.OpenNodeSettings(filepath.Join(dir, "node_settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := nodeSettings.Save(t.Context(), cfg); err != nil {
		_ = nodeSettings.Close()
		t.Fatal(err)
	}
	llmConfigs := openTestLLMConfigStore(t)
	t.Cleanup(func() { _ = nodeSettings.Close(); _ = llmConfigs.Close() })
	return nodeSettings, llmConfigs
}

func TestHandlePatchSetupConfig_noConfigPath(t *testing.T) {
	srv := NewServer(testConfig(t), nil, WithSkipStore())
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := []byte(`{"llm":{"profiles":[{"id":"default","provider":"mock","model":"mock","mock":true}]}}`)
	req, err := http.NewRequest(http.MethodPatch, ts.URL+"/v1/settings", bytes.NewReader(body))
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

func TestHandlePatchSetupConfigRequiresBothStores(t *testing.T) {
	for _, tc := range []struct {
		name          string
		withNodeStore bool
		withLLMStore  bool
		body          string
	}{
		{name: "missing LLM store", withNodeStore: true, body: `{"compression":{"silent_trigger_tokens":60000}}`},
		{name: "missing Node settings store", withLLMStore: true, body: `{"llm":{"profiles":[{"id":"default","provider":"mock","model":"mock","mock":true}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			configPath := filepath.Join(dir, "config.yaml")
			cfg := testConfig(t)
			if err := config.SaveFile(configPath, cfg); err != nil {
				t.Fatal(err)
			}
			var options []Option
			if tc.withNodeStore {
				nodeSettings, err := store.OpenNodeSettings(filepath.Join(dir, "node_settings.db"))
				if err != nil {
					t.Fatal(err)
				}
				if err := nodeSettings.Save(t.Context(), cfg); err != nil {
					_ = nodeSettings.Close()
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = nodeSettings.Close() })
				options = append(options, WithNodeSettings(nodeSettings))
			}
			srv := NewServer(cfg, nil, append([]Option{WithSkipStore(), WithConfigPath(configPath)}, options...)...)
			if tc.withLLMStore {
				srv.llmConfigs = openTestLLMConfigStore(t)
			}
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewBufferString(tc.body))
			req.Header.Set("Content-Type", "application/json")
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "settings_store_unavailable") {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			reloaded, err := config.LoadFile(configPath)
			if err != nil {
				t.Fatal(err)
			}
			if reloaded.Compression.SilentTriggerTokens != cfg.Compression.SilentTriggerTokens || reloaded.LLM.Mock != cfg.LLM.Mock {
				t.Fatal("settings patch was persisted to YAML without both stores")
			}
		})
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
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/llm/probe-models", bytes.NewReader(body))
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
