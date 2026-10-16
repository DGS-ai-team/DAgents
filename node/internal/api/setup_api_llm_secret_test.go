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
