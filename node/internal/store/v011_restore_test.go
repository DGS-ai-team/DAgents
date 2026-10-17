package store

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/shared/config"
	_ "modernc.org/sqlite"
)

// TestV011StoresRestoreRepresentativeReleaseData opens synthetic files built
// with the v0.11.0 SQLite schemas and serialized data shapes. It exercises the
// current persistent stores in-process; it is not an installer-upgrade test.
func TestV011StoresRestoreRepresentativeReleaseData(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	runtimeRoot := filepath.Join(root, ".runtime")
	workspace := filepath.Join(root, "workspaces", "primary-agent")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatal(err)
	}
	workspaceSentinel := filepath.Join(workspace, "keep.txt")
	if err := os.WriteFile(workspaceSentinel, []byte("user workspace fixture"), 0o600); err != nil {
		t.Fatal(err)
	}

	// v0.11.0 node_settings.db uses a singleton JSON settings row.
	nodeSettingsPath := filepath.Join(runtimeRoot, "node_settings.db")
	nodeDB := openV011FixtureDB(t, nodeSettingsPath)
	_, err := nodeDB.Exec(`CREATE TABLE node_settings (
		id INTEGER PRIMARY KEY CHECK (id = 1), settings_json TEXT NOT NULL DEFAULT '{}',
		schema_version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	// This literal mirrors v0.11.0's Config JSON field names. In particular it
	// contains old idle compression fields that current code must not reinterpret
	// as the different new-context setting.
	settingsJSON := `{"NodeID":"node-v011-fixture","Agent":{"Name":"fixture-node","Description":"synthetic release data"},"User":{"PreferredName":"Fixture User"},"Onboarding":{"NodeProfileCompleted":true},"LLM":{"Active":"fixture-provider","Provider":"openai","BaseURL":"https://example.invalid/v1","Model":"fixture-model"},"Skills":{"Enabled":true,"MaxInPrompt":4},"Compression":{"SilentTriggerTokens":51000,"BlockingTriggerTokens":72000,"IdleAutoCompressSeconds":3600,"IdleAutoCompressPollSeconds":60,"IdleAutoCompressMinTokens":12000},"Triggers":{"Enabled":true,"PollSeconds":11},"ChildAgents":{"Enabled":true,"DefaultMaxTurns":13}}`
	if _, err := nodeDB.Exec(`INSERT INTO node_settings(id, settings_json, schema_version, updated_at) VALUES(1, ?, 1, ?)`, string(settingsJSON), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := nodeDB.Close(); err != nil {
		t.Fatal(err)
	}
	bootstrapPath := filepath.Join(root, "config.yaml")
	if err := config.SaveBootstrapFile(bootstrapPath, &config.Config{
		NodeID: "node-v011-fixture",
		Listen: config.ListenConfig{Host: "127.0.0.1", Port: 18765},
		Local:  config.LocalConfig{Endpoint: "http://127.0.0.1:18765"},
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFile(bootstrapPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.RuntimeRoot = runtimeRoot
	nodeStore, err := BootstrapNodeSettings(ctx, cfg, bootstrapPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	if cfg.User.PreferredName != "Fixture User" || !cfg.Onboarding.NodeProfileCompleted ||
		!cfg.Skills.Enabled || cfg.Compression.SilentTriggerTokens != 51000 ||
		!cfg.Triggers.Enabled || cfg.Triggers.PollSeconds != 11 || !cfg.ChildAgents.Enabled || cfg.Compression.IdleNewConversationSeconds != 0 {
		t.Fatalf("v0.11 node settings were not restored: %+v", cfg)
	}

	// v0.11.0 llm_configs.db stores AES-GCM(base64(nonce|ciphertext)); the
	// separate .llm_secret_key is part of the backup unit with that database.
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtimeRoot, secretKeyFileName), key, 0o600); err != nil {
		t.Fatal(err)
	}
	const syntheticAPIKey = "fixture-only-not-a-real-api-key"
	ciphertext := v011FixtureCiphertext(t, key, syntheticAPIKey)
	llmDB := openV011FixtureDB(t, filepath.Join(runtimeRoot, "llm_configs.db"))
	_, err = llmDB.Exec(`CREATE TABLE llm_configs (
		id TEXT PRIMARY KEY, sort_order INTEGER NOT NULL, provider TEXT NOT NULL,
		base_url TEXT NOT NULL DEFAULT '', model TEXT NOT NULL DEFAULT '',
		api_key_ciphertext TEXT NOT NULL DEFAULT '', mock INTEGER NOT NULL DEFAULT 0,
		thinking TEXT NOT NULL DEFAULT '', reasoning_effort TEXT NOT NULL DEFAULT '',
		multimodal_enabled INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := llmDB.Exec(`INSERT INTO llm_configs(id, sort_order, provider, base_url, model, api_key_ciphertext, mock, thinking, reasoning_effort, multimodal_enabled, created_at, updated_at)
		VALUES('fixture-provider', 0, 'openai', 'https://example.invalid/v1', 'fixture-model', ?, 0, 'enabled', 'high', 1, ?, ?)`, ciphertext, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := llmDB.Close(); err != nil {
		t.Fatal(err)
	}
	llmStore, err := OpenLLMConfigs(filepath.Join(runtimeRoot, "llm_configs.db"), runtimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer llmStore.Close()
	if err := EnsureDefaultLLMConfig(ctx, llmStore, cfg); err != nil {
		t.Fatal(err)
	}
	llmRecords, err := llmStore.List(ctx)
	if err != nil || len(llmRecords) != 1 {
		t.Fatalf("LLM records=%d err=%v", len(llmRecords), err)
	}
	ApplyLLMConfigsToConfig(cfg, llmRecords, cfg.LLM.Active)
	profile, ok := cfg.LLM.GetProfile("fixture-provider")
	if !ok || profile.Model != "fixture-model" || !config.ProfileMultimodalEnabled(profile) {
		t.Fatalf("LLM profile was not restored: profile=%+v ok=%v", profile, ok)
	}
	resolvedKey, err := llmStore.ResolveAPIKey(ctx, "fixture-provider")
	if err != nil || resolvedKey != syntheticAPIKey {
		t.Fatalf("synthetic key restore failed: match=%v err=%v", resolvedKey == syntheticAPIKey, err)
	}
	// A database-only copy must not be reported as a successful credential
	// restore. The ciphertext remains present, while the missing paired key
	// causes authenticated decryption to fail explicitly.
	dbOnlyRoot := filepath.Join(root, "db-only-copy")
	if err := os.MkdirAll(dbOnlyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	llmDBBytes, err := os.ReadFile(filepath.Join(runtimeRoot, "llm_configs.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dbOnlyRoot, "llm_configs.db"), llmDBBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	dbOnlyStore, err := OpenLLMConfigs(filepath.Join(dbOnlyRoot, "llm_configs.db"), dbOnlyRoot)
	if err != nil {
		t.Fatal(err)
	}
	dbOnlyRecord, err := dbOnlyStore.Get(ctx, "fixture-provider")
	if err != nil || !dbOnlyRecord.HasAPIKey() {
		t.Fatalf("database-only restore unexpectedly lost credential marker: record=%+v err=%v", dbOnlyRecord, err)
	}
	if _, err := dbOnlyStore.ResolveAPIKey(ctx, "fixture-provider"); err == nil {
		t.Fatal("database-only restore unexpectedly decrypted without the paired key file")
	}
	if err := dbOnlyStore.Close(); err != nil {
		t.Fatal(err)
	}

	// agents.db in v0.11.0 already owns the Agent/Auto config snapshot and its
	// custom workspace path. Current open performs additive sidecar setup.
	agentsPath := filepath.Join(runtimeRoot, "agents.db")
	agentDB := openV011FixtureDB(t, agentsPath)
	_, err = agentDB.Exec(`CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY, display_name TEXT NOT NULL, template_id TEXT NOT NULL,
		config_snapshot_json TEXT NOT NULL DEFAULT '{}', host_json TEXT NOT NULL DEFAULT '{}',
		archived INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		runtime_revision INTEGER NOT NULL DEFAULT 1
	)`)
	if err != nil {
		t.Fatal(err)
	}
	agentSnapshot := map[string]any{
		"agent_type": "auto", "template_id": "auto-worker",
		"workspace": map[string]any{"mode": "custom", "path": workspace},
		"defaults": map[string]any{
			"llm":   map[string]any{"active": "fixture-provider", "max_steps": 14},
			"auto":  map[string]any{"enabled": true, "wake_interval_seconds": 900, "daily_budget": 2},
			"tools": map[string]any{"enabled_groups": []string{"filesystem", "shell"}},
		},
	}
	agentJSON, err := json.Marshal(agentSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentDB.Exec(`INSERT INTO agents(agent_id, display_name, template_id, config_snapshot_json, host_json, archived, created_at, updated_at, runtime_revision)
		VALUES('agent-v011', 'Fixture Auto', 'auto-worker', ?, '{}', 0, ?, ?, 1)`, string(agentJSON), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := agentDB.Close(); err != nil {
		t.Fatal(err)
	}
	agentStore, err := OpenAgents(agentsPath)
	if err != nil {
		t.Fatal(err)
	}
	defer agentStore.Close()
	agent, err := agentStore.Get(ctx, "agent-v011")
	if err != nil || agent == nil {
		t.Fatalf("agent=%+v err=%v", agent, err)
	}
	var restoredAgent map[string]any
	if err := json.Unmarshal(agent.ConfigSnapshot, &restoredAgent); err != nil {
		t.Fatal(err)
	}
	workspaceCfg := restoredAgent["workspace"].(map[string]any)
	defaults := restoredAgent["defaults"].(map[string]any)
	autoCfg := defaults["auto"].(map[string]any)
	if workspaceCfg["path"] != workspace || autoCfg["enabled"] != true || autoCfg["wake_interval_seconds"] != float64(900) {
		t.Fatalf("Agent/Auto/workspace snapshot changed: %+v", restoredAgent)
	}
	if data, err := os.ReadFile(workspaceSentinel); err != nil || string(data) != "user workspace fixture" {
		t.Fatalf("external workspace fixture changed: data=%q err=%v", data, err)
	}

	// The v0.11.0 session DB schema is also directly readable. New RuntimeState
	// fields are optional JSON additions; existing transcript/input state stays.
	sessionDB := openV011FixtureDB(t, filepath.Join(runtimeRoot, "memory", "sessions.db"))
	_, err = sessionDB.Exec(`CREATE TABLE agent_runtimes (
		agent_id TEXT PRIMARY KEY, node_id TEXT NOT NULL DEFAULT '', messages_json TEXT NOT NULL DEFAULT '[]',
		first_user_message TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
		loaded_skills_json TEXT NOT NULL DEFAULT '[]', runtime_state_json TEXT NOT NULL DEFAULT '{}'
	)`)
	if err != nil {
		t.Fatal(err)
	}
	oldMessages := `[{"role":"user","content":"fixture request"},{"role":"assistant","content":"fixture answer"}]`
	oldRuntimeState := `{"input_box_state":{"seq":4,"items":[]},"history_revision":8,"active_context_start":0,"last_context_reset_id":"fixture-reset","dreaming_attempt":{"status":"idle"},"idle_auto_compress_applied":true}`
	if _, err := sessionDB.Exec(`INSERT INTO agent_runtimes(agent_id, node_id, messages_json, first_user_message, created_at, updated_at, loaded_skills_json, runtime_state_json)
		VALUES('session-v011', 'node-v011-fixture', ?, 'fixture request', ?, ?, '[]', ?)`, oldMessages, stamp, stamp, oldRuntimeState); err != nil {
		t.Fatal(err)
	}
	if err := sessionDB.Close(); err != nil {
		t.Fatal(err)
	}
	sessionStore, err := Open(filepath.Join(runtimeRoot, "memory", "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sessionStore.Close()
	session, err := sessionStore.Load(ctx, "session-v011")
	if err != nil || session == nil || len(session.Messages) != 2 || session.Messages[0].Content != "fixture request" ||
		session.RuntimeState.HistoryRevision != 8 || session.RuntimeState.ActiveContextStart != 0 || session.RuntimeState.LastContextResetID != "fixture-reset" {
		t.Fatalf("v0.11 session restore failed: record=%+v err=%v", session, err)
	}
}

func openV011FixtureDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func v011FixtureCiphertext(t *testing.T, key []byte, plaintext string) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, gcm.NonceSize())
	for i := range nonce {
		nonce[i] = byte(0xa0 + i)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed)
}
