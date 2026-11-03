package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func TestSetupSettingsNormalizesLegacyOpenAIEffortAndPreservesKey(t *testing.T) {
	const profileID = "legacy-o3"
	const apiKey = "synthetic-legacy-o3-key"
	dir := t.TempDir()
	llmConfigs, err := store.OpenLLMConfigs(filepath.Join(dir, "llm_configs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := llmConfigs.ReplaceAll(context.Background(), []store.LLMConfigRecord{{
		ID: profileID, Provider: "openai", Model: "o3", Thinking: "enabled", ReasoningEffort: "max",
	}}, map[string]string{profileID: apiKey}, nil); err != nil {
		_ = llmConfigs.Close()
		t.Fatal(err)
	}
	legacyRecords, err := llmConfigs.List(context.Background())
	if err != nil {
		_ = llmConfigs.Close()
		t.Fatal(err)
	}
	if len(legacyRecords) != 1 || legacyRecords[0].ReasoningEffort != "" || !legacyRecords[0].HasAPIKey() {
		_ = llmConfigs.Close()
		t.Fatalf("legacy OpenAI record projection = %+v", legacyRecords)
	}

	cfg := testConfig(t)
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		profileID: {Provider: "openai", Model: "o3", Thinking: "enabled", ReasoningEffort: "max"},
	}
	cfg.LLM.ProfileOrder = []string{profileID}
	if err := cfg.SetActiveLLMProfile(profileID); err != nil {
		_ = llmConfigs.Close()
		t.Fatal(err)
	}
	store.ApplyLLMConfigsToConfig(cfg, legacyRecords, profileID)
	nodeSettings, err := store.OpenNodeSettings(filepath.Join(dir, "node_settings.db"))
	if err != nil {
		_ = llmConfigs.Close()
		t.Fatal(err)
	}
	if err := nodeSettings.Save(context.Background(), cfg); err != nil {
		_ = nodeSettings.Close()
		_ = llmConfigs.Close()
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore(), WithNodeSettings(nodeSettings))
	srv.llmConfigs = llmConfigs
	t.Cleanup(func() {
		srv.Close()
		_ = nodeSettings.Close()
		_ = llmConfigs.Close()
	})

	get := httptest.NewRecorder()
	srv.Handler().ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
	if get.Code != http.StatusOK {
		t.Fatalf("GET settings status=%d body=%s", get.Code, get.Body.String())
	}
	var view setup.SettingsView
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.LLM.Profiles) != 1 || view.LLM.Profiles[0].ReasoningEffort != "" || !view.LLM.Profiles[0].HasAPIKey {
		t.Fatalf("legacy OpenAI GET view = %+v", view.LLM.Profiles)
	}

	// Existing invalid legacy state must not block an unrelated settings change.
	unrelated := httptest.NewRecorder()
	srv.Handler().ServeHTTP(unrelated, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader([]byte(`{"compression":{"silent_trigger_tokens":60000}}`))))
	if unrelated.Code != http.StatusOK {
		t.Fatalf("unrelated settings PATCH status=%d body=%s", unrelated.Code, unrelated.Body.String())
	}
	key, err := llmConfigs.ResolveAPIKey(context.Background(), profileID)
	if err != nil || key != apiKey {
		t.Fatalf("API key after unrelated PATCH = %q, err=%v", key, err)
	}

	invalid := []byte(`{"llm":{"profiles":[{"id":"legacy-o3","provider":"openai","model":"o3","thinking":"enabled","reasoning_effort":"max"}]}}`)
	badPatch := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badPatch, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader(invalid)))
	if badPatch.Code != http.StatusBadRequest {
		t.Fatalf("new unsupported OpenAI o3 effort status=%d body=%s", badPatch.Code, badPatch.Body.String())
	}

	valid := []byte(`{"llm":{"profiles":[{"id":"legacy-o3","provider":"openai","model":"o3","thinking":"enabled","reasoning_effort":"high"}]}}`)
	patch := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patch, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader(valid)))
	if patch.Code != http.StatusOK {
		t.Fatalf("valid OpenAI o3 effort PATCH status=%d body=%s", patch.Code, patch.Body.String())
	}
	stored, err := llmConfigs.Get(context.Background(), profileID)
	if err != nil {
		t.Fatal(err)
	}
	key, err = llmConfigs.ResolveAPIKey(context.Background(), profileID)
	if err != nil || key != apiKey {
		t.Fatalf("API key after valid effort PATCH = %q, err=%v", key, err)
	}
	if stored.ReasoningEffort != "high" || !stored.HasAPIKey() {
		t.Fatalf("stored OpenAI record after valid effort PATCH = %+v", stored)
	}
}
