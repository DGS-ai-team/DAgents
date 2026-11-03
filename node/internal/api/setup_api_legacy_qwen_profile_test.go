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

func TestSetupSettingsNormalizesLegacyQwenProfileWithoutLosingBudgetOrKey(t *testing.T) {
	const profileID = "legacy-qwen"
	const apiKey = "synthetic-legacy-qwen-key"
	dir := t.TempDir()
	cfg := testConfig(t)
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		profileID: {
			Provider: "qwen", Model: "qwen3-32b", Thinking: "enabled",
			ReasoningEffort: "high",
		},
	}
	cfg.LLM.ProfileOrder = []string{profileID}
	if err := cfg.SetActiveLLMProfile(profileID); err != nil {
		t.Fatal(err)
	}
	budget := 4096
	llmConfigs, err := store.OpenLLMConfigs(filepath.Join(dir, "llm_configs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := llmConfigs.ReplaceAll(context.Background(), []store.LLMConfigRecord{{
		ID: profileID, Provider: "qwen", Model: "qwen3-32b", Thinking: "enabled",
		ReasoningEffort: "high", ThinkingBudget: &budget,
	}}, map[string]string{profileID: apiKey}, nil); err != nil {
		_ = llmConfigs.Close()
		t.Fatal(err)
	}
	records, err := llmConfigs.List(context.Background())
	if err != nil {
		_ = llmConfigs.Close()
		t.Fatal(err)
	}
	store.ApplyLLMConfigsToConfig(cfg, records, profileID)

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
	if len(view.LLM.Profiles) != 1 || view.LLM.Profiles[0].ReasoningEffort != "" ||
		view.LLM.Profiles[0].Thinking != "enabled" || view.LLM.Profiles[0].ThinkingBudget == nil ||
		*view.LLM.Profiles[0].ThinkingBudget != budget || !view.LLM.Profiles[0].HasAPIKey {
		t.Fatalf("legacy Qwen GET view = %+v", view.LLM.Profiles)
	}

	invalid := []byte(`{"llm":{"profiles":[{"id":"legacy-qwen","provider":"qwen","model":"qwen3-32b","thinking":"enabled","reasoning_effort":"high","thinking_budget":4096}]}}`)
	badPatch := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badPatch, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader(invalid)))
	if badPatch.Code != http.StatusBadRequest {
		t.Fatalf("new unsupported Qwen effort status=%d body=%s", badPatch.Code, badPatch.Body.String())
	}

	valid := []byte(`{"llm":{"profiles":[{"id":"legacy-qwen","provider":"qwen","model":"qwen3-32b","thinking":"enabled","thinking_budget":4096}]}}`)
	patch := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patch, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader(valid)))
	if patch.Code != http.StatusOK {
		t.Fatalf("valid Qwen profile patch status=%d body=%s", patch.Code, patch.Body.String())
	}
	stored, err := llmConfigs.Get(context.Background(), profileID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := llmConfigs.ResolveAPIKey(context.Background(), profileID)
	if err != nil || key != apiKey {
		t.Fatalf("profile key after patch = %q, err=%v", key, err)
	}
	if stored.ReasoningEffort != "" || stored.Thinking != "enabled" || stored.ThinkingBudget == nil || *stored.ThinkingBudget != budget {
		t.Fatalf("stored Qwen profile after patch = %+v", stored)
	}
}
