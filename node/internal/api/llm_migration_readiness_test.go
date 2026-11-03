package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
	_ "modernc.org/sqlite"
)

func TestSettingsWritesStayClosedUntilLegacyLLMProfilesMigrate(t *testing.T) {
	const keyEnv = "DAGENTS_MIGRATION_READINESS_TEST_KEY"
	t.Setenv(keyEnv, "")
	runtimeRoot := t.TempDir()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	legacy := testConfig(t)
	legacy.RuntimeRoot = runtimeRoot
	legacy.LLM = config.LLMConfig{
		Active:       "primary",
		ProfileOrder: []string{"primary", "secondary"},
		Profiles: map[string]config.LLMProfileConfig{
			"primary":   {Provider: "qwen", Model: "qwen3-32b", APIKeyEnv: keyEnv},
			"secondary": {Provider: "openai", Model: "gpt-test"},
		},
	}
	legacy.ApplyDefaults()

	// Write the legacy snapshot shape directly: NodeSettingsStore.Save intentionally
	// writes the new compact schema and therefore cannot represent a pre-migration DB.
	settings, err := store.OpenNodeSettings(legacy.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := settings.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", legacy.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO node_settings (id, settings_json, schema_version, updated_at) VALUES (1, ?, 1, '2025-01-01T00:00:00Z')`, string(raw)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(testConfigWithRuntimeRoot(t, runtimeRoot), nil, WithConfigPath(configPath))
	if srv.llmConfigMigrationErr == nil {
		t.Fatal("expected missing credential environment variable to block LLM migration")
	}
	if srv.setupWritable() {
		t.Fatal("settings should not be writable while legacy LLM profiles are unimported")
	}
	getResp := httptest.NewRecorder()
	srv.Handler().ServeHTTP(getResp, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
	var setupView struct {
		ConfigWritable bool `json:"config_writable"`
	}
	if err := json.Unmarshal(getResp.Body.Bytes(), &setupView); err != nil {
		t.Fatal(err)
	}
	if setupView.ConfigWritable {
		t.Fatal("settings GET reported writable before legacy LLM migration completed")
	}
	activeResp := httptest.NewRecorder()
	activeReq := httptest.NewRequest(http.MethodPatch, "/v1/llm/settings", strings.NewReader(`{"active_profile":"secondary"}`))
	activeReq.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(activeResp, activeReq)
	if activeResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("active profile PATCH status = %d, want %d; body=%s", activeResp.Code, http.StatusServiceUnavailable, activeResp.Body.String())
	}
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(`{"compression":{"silent_trigger_tokens":60000}}`))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("settings PATCH status = %d, want %d; body=%s", resp.Code, http.StatusServiceUnavailable, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), keyEnv) {
		t.Fatalf("migration diagnostic should identify missing env var, body=%s", resp.Body.String())
	}
	if count, err := srv.llmConfigs.Count(context.Background()); err != nil || count != 0 {
		t.Fatalf("failed migration changed llm DB: count=%d err=%v", count, err)
	}
	srv.Close()

	settings, err = store.OpenNodeSettings(legacy.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := settings.Load(context.Background())
	if err != nil {
		settings.Close()
		t.Fatal(err)
	}
	if snapshot == nil || len(snapshot.LLM.Profiles) != 2 || snapshot.LLM.Profiles["primary"].APIKeyEnv != keyEnv {
		settings.Close()
		t.Fatalf("failed migration discarded legacy source: %+v", snapshot)
	}
	if err := settings.Close(); err != nil {
		t.Fatal(err)
	}

	t.Setenv(keyEnv, "migration-readiness-key")
	srv = NewServer(testConfigWithRuntimeRoot(t, runtimeRoot), nil, WithConfigPath(configPath))
	defer srv.Close()
	if srv.llmConfigMigrationErr != nil {
		t.Fatalf("migration should recover after env is provided: %v", srv.llmConfigMigrationErr)
	}
	profiles, err := srv.llmConfigs.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 2 || profiles[0].ID != "primary" || profiles[1].ID != "secondary" {
		t.Fatalf("migrated profiles = %+v", profiles)
	}
	key, err := srv.llmConfigs.ResolveAPIKey(context.Background(), "primary")
	if err != nil || key != "migration-readiness-key" {
		t.Fatalf("migrated API key = %q, err=%v", key, err)
	}
	settings, err = store.OpenNodeSettings(legacy.NodeSettingsDBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	snapshot, err = settings.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == nil || snapshot.LLM.Active != "primary" || len(snapshot.LLM.Profiles) != 0 || snapshot.LLM.Model != "" {
		t.Fatalf("legacy snapshot was not compacted after successful import: %+v", snapshot)
	}
}

func testConfigWithRuntimeRoot(t *testing.T, runtimeRoot string) *config.Config {
	t.Helper()
	cfg := testConfig(t)
	cfg.RuntimeRoot = runtimeRoot
	return cfg
}
