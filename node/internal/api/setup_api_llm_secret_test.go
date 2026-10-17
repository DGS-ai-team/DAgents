package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/setup"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func openTestLLMConfigStore(t *testing.T) *store.LLMConfigStore {
	t.Helper()
	dir := t.TempDir()
	st, err := store.OpenLLMConfigs(filepath.Join(dir, "llm_configs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func seedTestLLMProfiles(t *testing.T, st *store.LLMConfigStore, profiles []store.LLMConfigRecord, keys map[string]string) {
	t.Helper()
	if err := st.ReplaceAll(context.Background(), profiles, keys, nil); err != nil {
		t.Fatal(err)
	}
}

func resolveTestLLMKey(t *testing.T, st *store.LLMConfigStore, id string) string {
	t.Helper()
	key, err := st.ResolveAPIKey(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestPersistLLMConfigsPreservesSecretsOnEditRenameAndClear(t *testing.T) {
	t.Run("same id and changed model retains key", func(t *testing.T) {
		st := openTestLLMConfigStore(t)
		seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "custom", Provider: "openai", Model: "old-model"}}, map[string]string{"custom": "synthetic-keep-key"})
		srv := &Server{llmConfigs: st}
		if err := srv.persistLLMConfigs(context.Background(), []setup.LLMProfileSettings{{ID: "custom", Provider: "openai", Model: "new-model"}}); err != nil {
			t.Fatal(err)
		}
		if got := resolveTestLLMKey(t, st, "custom"); got != "synthetic-keep-key" {
			t.Fatal("saved profile did not retain its existing key")
		}
	})

	t.Run("renaming active profile transfers key", func(t *testing.T) {
		st := openTestLLMConfigStore(t)
		seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "custom", Provider: "openai", Model: "gpt-test"}}, map[string]string{"custom": "synthetic-rename-key"})
		srv := &Server{llmConfigs: st}
		if err := srv.persistLLMConfigs(context.Background(), []setup.LLMProfileSettings{{ID: "renamed", OriginalID: "custom", Provider: "openai", Model: "gpt-test"}}); err != nil {
			t.Fatal(err)
		}
		if got := resolveTestLLMKey(t, st, "renamed"); got != "synthetic-rename-key" {
			t.Fatal("renamed profile did not retain its original key")
		}
		if _, err := st.Get(context.Background(), "custom"); err == nil {
			t.Fatal("old profile ID remained after full-list replacement")
		}
	})

	t.Run("explicit clear wins over source key", func(t *testing.T) {
		st := openTestLLMConfigStore(t)
		seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "custom", Provider: "openai"}}, map[string]string{"custom": "synthetic-clear-key"})
		srv := &Server{llmConfigs: st}
		if err := srv.persistLLMConfigs(context.Background(), []setup.LLMProfileSettings{{ID: "renamed", OriginalID: "custom", Provider: "openai", ClearAPIKey: true}}); err != nil {
			t.Fatal(err)
		}
		if got := resolveTestLLMKey(t, st, "renamed"); got != "" {
			t.Fatal("explicit clear did not remove the key")
		}
	})

	t.Run("explicit replacement wins over source key", func(t *testing.T) {
		st := openTestLLMConfigStore(t)
		seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "custom", Provider: "openai"}}, map[string]string{"custom": "synthetic-old-key"})
		srv := &Server{llmConfigs: st}
		if err := srv.persistLLMConfigs(context.Background(), []setup.LLMProfileSettings{{ID: "renamed", OriginalID: "custom", Provider: "openai", APIKey: "synthetic-new-key"}}); err != nil {
			t.Fatal(err)
		}
		if got := resolveTestLLMKey(t, st, "renamed"); got != "synthetic-new-key" {
			t.Fatal("explicit replacement was not saved")
		}
	})

	t.Run("batch rename keeps source keys associated", func(t *testing.T) {
		st := openTestLLMConfigStore(t)
		seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "first", Provider: "openai"}, {ID: "second", Provider: "qwen"}}, map[string]string{"first": "synthetic-first-key", "second": "synthetic-second-key"})
		srv := &Server{llmConfigs: st}
		profiles := []setup.LLMProfileSettings{{ID: "renamed-first", OriginalID: "first", Provider: "openai"}, {ID: "second", Provider: "qwen"}}
		if err := srv.persistLLMConfigs(context.Background(), profiles); err != nil {
			t.Fatal(err)
		}
		if resolveTestLLMKey(t, st, "renamed-first") != "synthetic-first-key" || resolveTestLLMKey(t, st, "second") != "synthetic-second-key" {
			t.Fatal("batch save changed key association")
		}
	})
}

func TestPersistLLMConfigsRejectsAmbiguousSecretMoveWithoutChangingStore(t *testing.T) {
	tests := []struct {
		name     string
		profiles []setup.LLMProfileSettings
	}{
		{name: "missing source", profiles: []setup.LLMProfileSettings{{ID: "renamed", OriginalID: "missing", Provider: "openai"}}},
		{name: "duplicate source", profiles: []setup.LLMProfileSettings{{ID: "one", OriginalID: "source", Provider: "openai"}, {ID: "two", OriginalID: "source", Provider: "openai"}}},
		{name: "occupied destination", profiles: []setup.LLMProfileSettings{{ID: "destination", OriginalID: "source", Provider: "openai"}}},
		{name: "source still present", profiles: []setup.LLMProfileSettings{{ID: "renamed", OriginalID: "source", Provider: "openai"}, {ID: "source", Provider: "qwen"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := openTestLLMConfigStore(t)
			seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "source", Provider: "openai"}, {ID: "destination", Provider: "qwen"}}, map[string]string{"source": "synthetic-source-key", "destination": "synthetic-destination-key"})
			before, err := st.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			srv := &Server{llmConfigs: st}
			if err := srv.persistLLMConfigs(context.Background(), tt.profiles); err == nil {
				t.Fatal("ambiguous key move unexpectedly succeeded")
			}
			after, err := st.List(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("store changed after rejected key move")
			}
		})
	}
}

func TestPersistLLMConfigsProtectsAgentProfileReferences(t *testing.T) {
	ctx := context.Background()
	st := openTestLLMConfigStore(t)
	seedTestLLMProfiles(t, st, []store.LLMConfigRecord{
		{ID: "bound", Provider: "openai", Model: "original-model"},
		{ID: "spare", Provider: "qwen", Model: "spare-model"},
	}, map[string]string{"bound": "synthetic-bound-key", "spare": "synthetic-spare-key"})
	agents, err := store.OpenAgents(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer agents.Close()
	for _, rec := range []store.AgentRecord{
		{AgentID: "normal-agent", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal","defaults":{"llm":{"active":"bound"}}}`)},
		{AgentID: "auto-agent", ConfigSnapshot: json.RawMessage(`{"agent_type":"auto","defaults":{"llm":{"active":"bound"}}}`)},
		{AgentID: "archived-agent", Archived: true, ConfigSnapshot: json.RawMessage(`{"agent_type":"normal","defaults":{"llm":{"active":"bound"}}}`)},
	} {
		if err := agents.Save(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	srv := &Server{llmConfigs: st, agents: agents}

	for _, profiles := range [][]setup.LLMProfileSettings{
		{{ID: "renamed", OriginalID: "bound", Provider: "openai", Model: "new-model"}, {ID: "spare", Provider: "qwen", Model: "spare-model"}},
		{{ID: "spare", Provider: "qwen", Model: "spare-model"}},
	} {
		if err := srv.persistLLMConfigs(ctx, profiles); err == nil || !strings.Contains(err.Error(), "无法删除或改名模型配置「bound」") || !strings.Contains(err.Error(), "auto-agent") || !strings.Contains(err.Error(), "normal-agent") || strings.Contains(err.Error(), "archived-agent") {
			t.Fatalf("referenced profile mutation error = %v", err)
		}
		if got := resolveTestLLMKey(t, st, "bound"); got != "synthetic-bound-key" {
			t.Fatal("referenced profile or secret changed after rejection")
		}
		if rec, err := st.Get(ctx, "bound"); err != nil || rec.Model != "original-model" {
			t.Fatalf("referenced profile = %+v, err=%v", rec, err)
		}
	}

	// Editing the same referenced ID and renaming an unreferenced ID stay valid.
	if err := srv.persistLLMConfigs(ctx, []setup.LLMProfileSettings{
		{ID: "bound", Provider: "openai", Model: "edited-model"},
		{ID: "spare-renamed", OriginalID: "spare", Provider: "qwen", Model: "spare-model"},
	}); err != nil {
		t.Fatalf("same-ID edit plus unreferenced rename failed: %v", err)
	}
	if got := resolveTestLLMKey(t, st, "bound"); got != "synthetic-bound-key" {
		t.Fatal("same-ID edit did not retain the referenced key")
	}
	for _, id := range []string{"normal-agent", "auto-agent"} {
		if err := agents.SoftDelete(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if err := srv.persistLLMConfigs(ctx, []setup.LLMProfileSettings{
		{ID: "renamed", OriginalID: "bound", Provider: "openai", Model: "renamed-model"},
		{ID: "spare-renamed", Provider: "qwen", Model: "spare-model"},
	}); err != nil {
		t.Fatalf("rename after archiving referencing Agents failed: %v", err)
	}
}

func TestPatchSetupRejectsReferencedProfileBeforePersistingSettingsOrSecrets(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	profiles := map[string]config.LLMProfileConfig{
		"bound": {Provider: "openai", Model: "original-model"},
		"spare": {Provider: "qwen", Model: "spare-model"},
	}
	cfg := testConfig(t)
	cfg.LLM.Profiles = profiles
	cfg.LLM.ProfileOrder = []string{"bound", "spare"}
	if err := cfg.SetActiveLLMProfile("bound"); err != nil {
		t.Fatal(err)
	}
	llmStore := openTestLLMConfigStore(t)
	seedTestLLMProfiles(t, llmStore, []store.LLMConfigRecord{
		{ID: "bound", Provider: "openai", Model: "original-model"},
		{ID: "spare", Provider: "qwen", Model: "spare-model"},
	}, map[string]string{"bound": "synthetic-bound-key", "spare": "synthetic-spare-key"})
	settings, err := store.OpenNodeSettings(filepath.Join(root, "node_settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Save(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	agents, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := agents.Save(ctx, store.AgentRecord{AgentID: "agent-bound", ConfigSnapshot: json.RawMessage(`{"agent_type":"normal","defaults":{"llm":{"active":"bound"}}}`)}); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore())
	srv.llmConfigs = llmStore
	srv.nodeSettings = settings
	srv.agents = agents
	t.Cleanup(func() {
		_ = agents.Close()
		_ = settings.Close()
	})
	patch := []byte(`{"llm":{"active":"spare","profiles":[{"id":"spare","provider":"qwen","model":"spare-model"}]}}`)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader(patch)))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "llm_profile_in_use") || !strings.Contains(rec.Body.String(), "agent-bound") || !strings.Contains(rec.Body.String(), "无法删除或改名") {
		t.Fatalf("PATCH status=%d body=%s", rec.Code, rec.Body.String())
	}
	loaded, err := settings.Load(ctx)
	if err != nil || loaded == nil || loaded.LLM.Active != "bound" {
		t.Fatalf("node settings changed after rejection: settings=%+v err=%v", loaded, err)
	}
	if got := resolveTestLLMKey(t, llmStore, "bound"); got != "synthetic-bound-key" {
		t.Fatal("LLM secret store changed after rejected setup PATCH")
	}
}

func TestAgentLLMProfileAssignmentsRequireExistingProfiles(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(t)
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		"bound": {Provider: "openai", Model: "model-a"},
	}
	cfg.LLM.ProfileOrder = []string{"bound"}
	if err := cfg.SetActiveLLMProfile("bound"); err != nil {
		t.Fatal(err)
	}
	llmStore := openTestLLMConfigStore(t)
	seedTestLLMProfiles(t, llmStore, []store.LLMConfigRecord{{ID: "bound", Provider: "openai", Model: "model-a"}}, nil)
	agents, err := store.OpenAgents(filepath.Join(root, "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore())
	srv.agents = agents
	srv.llmConfigs = llmStore
	t.Cleanup(srv.Close)

	create := func(active string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{
			"display_name": "profile test",
			"defaults":     map[string]any{"llm": map[string]any{"active": active}},
		})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewReader(body)))
		return rec
	}
	invalidCreate := create("missing")
	if invalidCreate.Code != http.StatusBadRequest || !strings.Contains(invalidCreate.Body.String(), "invalid_agent_llm_profile") || !strings.Contains(invalidCreate.Body.String(), "模型配置「missing」不存在") {
		t.Fatalf("invalid create status=%d body=%s", invalidCreate.Code, invalidCreate.Body.String())
	}
	if records, err := agents.List(context.Background()); err != nil || len(records) != 0 {
		t.Fatalf("invalid create persisted Agent: records=%+v err=%v", records, err)
	}

	validCreate := create("bound")
	if validCreate.Code != http.StatusOK {
		t.Fatalf("valid create status=%d body=%s", validCreate.Code, validCreate.Body.String())
	}
	var created agentView
	if err := json.Unmarshal(validCreate.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	before, err := agents.Get(context.Background(), created.AgentID)
	if err != nil || before == nil {
		t.Fatalf("created Agent missing: rec=%+v err=%v", before, err)
	}
	patchBody := []byte(`{"defaults":{"llm":{"active":"missing"}}}`)
	patchRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(patchRec, httptest.NewRequest(http.MethodPatch, "/v1/agents/"+created.AgentID, bytes.NewReader(patchBody)))
	if patchRec.Code != http.StatusBadRequest || !strings.Contains(patchRec.Body.String(), "invalid_agent_llm_profile") {
		t.Fatalf("invalid patch status=%d body=%s", patchRec.Code, patchRec.Body.String())
	}
	after, err := agents.Get(context.Background(), created.AgentID)
	if err != nil || after == nil || !bytes.Equal(after.ConfigSnapshot, before.ConfigSnapshot) {
		t.Fatalf("invalid patch changed Agent snapshot: before=%s after=%+v err=%v", before.ConfigSnapshot, after, err)
	}
}

func TestSetupSettingsReadDoesNotExposeLLMKeyOrRenameMetadata(t *testing.T) {
	st := openTestLLMConfigStore(t)
	seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "custom", Provider: "openai", Model: "gpt-test"}}, map[string]string{"custom": "synthetic-private-key"})
	cfg := testConfig(t)
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{"custom": {Provider: "openai", Model: "gpt-test", APIKeyEnv: "TEST_LLM_SECRET"}}
	cfg.LLM.ProfileOrder = []string{"custom"}
	if err := cfg.SetActiveLLMProfile("custom"); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore())
	srv.llmConfigs = st
	t.Cleanup(srv.Close)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("settings status = %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "synthetic-private-key") || strings.Contains(body, "original_id") {
		t.Fatal("settings response exposed a secret or write-only rename field")
	}
	var view struct {
		LLM struct {
			Profiles []struct {
				HasAPIKey bool `json:"has_api_key"`
			} `json:"profiles"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if len(view.LLM.Profiles) != 1 || !view.LLM.Profiles[0].HasAPIKey {
		t.Fatal("settings response did not report only the configured-key flag")
	}
}

func TestSetupSettingsPatchRenamesProfileAndKeepsSecret(t *testing.T) {
	const syntheticKey = "synthetic-http-patch-key"
	st := openTestLLMConfigStore(t)
	seedTestLLMProfiles(t, st, []store.LLMConfigRecord{{ID: "source-profile", Provider: "openai", BaseURL: "https://api.example.test/v1", Model: "old-model"}}, map[string]string{"source-profile": syntheticKey})

	cfg := testConfig(t)
	cfg.LLM.Profiles = map[string]config.LLMProfileConfig{
		"source-profile": {Provider: "openai", BaseURL: "https://api.example.test/v1", Model: "old-model"},
	}
	cfg.LLM.ProfileOrder = []string{"source-profile"}
	if err := cfg.SetActiveLLMProfile("source-profile"); err != nil {
		t.Fatal(err)
	}
	srv := NewServer(cfg, nil, WithSkipStore())
	srv.llmConfigs = st
	t.Cleanup(srv.Close)

	patch := []byte(`{"llm":{"active":"renamed-profile","profiles":[{"id":"renamed-profile","original_id":"source-profile","provider":"openai","base_url":"https://api.example.test/v1","model":"new-model"}]}}`)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader(patch)))
	if rec.Code != http.StatusOK {
		t.Fatalf("rename patch status=%d body=%s", rec.Code, rec.Body.String())
	}
	var view setup.SettingsView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.LLM.Active != "renamed-profile" || len(view.LLM.Profiles) != 1 || view.LLM.Profiles[0].ID != "renamed-profile" || !view.LLM.Profiles[0].HasAPIKey {
		t.Fatal("rename response did not preserve the active profile and configured-key flag")
	}
	if strings.Contains(rec.Body.String(), syntheticKey) || strings.Contains(rec.Body.String(), "original_id") {
		t.Fatal("rename response exposed the key or write-only source ID")
	}
	if got := resolveTestLLMKey(t, st, "renamed-profile"); got != syntheticKey {
		t.Fatal("renamed profile did not retain its configured key")
	}

	clearPatch := []byte(`{"llm":{"active":"renamed-profile","profiles":[{"id":"renamed-profile","provider":"openai","base_url":"https://api.example.test/v1","model":"new-model","clear_api_key":true}]}}`)
	clearRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(clearRec, httptest.NewRequest(http.MethodPatch, "/v1/settings", bytes.NewReader(clearPatch)))
	if clearRec.Code != http.StatusOK {
		t.Fatalf("clear patch status=%d body=%s", clearRec.Code, clearRec.Body.String())
	}
	var cleared setup.SettingsView
	if err := json.Unmarshal(clearRec.Body.Bytes(), &cleared); err != nil {
		t.Fatal(err)
	}
	if len(cleared.LLM.Profiles) != 1 || cleared.LLM.Profiles[0].HasAPIKey || resolveTestLLMKey(t, st, "renamed-profile") != "" {
		t.Fatal("explicit clear was not reflected by the patch response and store")
	}
}
