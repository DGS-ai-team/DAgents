package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSecretBoxRoundTrip(t *testing.T) {
	dir := t.TempDir()
	box, err := OpenSecretBox(dir)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := box.Encrypt("sk-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "" || enc == "sk-test-secret" {
		t.Fatalf("unexpected ciphertext: %q", enc)
	}
	plain, err := box.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "sk-test-secret" {
		t.Fatalf("plain = %q", plain)
	}
	// 再次打开同一密钥文件应可解密。
	box2, err := OpenSecretBox(dir)
	if err != nil {
		t.Fatal(err)
	}
	plain2, err := box2.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if plain2 != "sk-test-secret" {
		t.Fatalf("plain2 = %q", plain2)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, secretKeyFileName))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("key file perms too open: %v", info.Mode())
		}
	}
}

func TestLLMConfigStoreReplaceAndResolve(t *testing.T) {
	dir := t.TempDir()
	st, err := OpenLLMConfigs(filepath.Join(dir, "llm_configs.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	budget := 12000
	err = st.ReplaceAll(ctx, []LLMConfigRecord{
		{ID: "deepseek", Provider: "deepseek", Model: "deepseek-v4-pro", Thinking: "enabled", ReasoningEffort: "low"},
		{ID: "qwen", Provider: "qwen", Model: "qwen3-32b", ThinkingBudget: &budget},
		{ID: "mock", Provider: "mock", Mock: true},
	}, map[string]string{"deepseek": "sk-aaa"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].ID != "deepseek" || !list[0].HasAPIKey() || list[0].ReasoningEffort != "low" || list[1].ThinkingBudget == nil || *list[1].ThinkingBudget != budget {
		t.Fatalf("list = %+v", list)
	}
	key, err := st.ResolveAPIKey(ctx, "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-aaa" {
		t.Fatalf("key = %q", key)
	}

	// 更新但不传 key → 保留原密文
	err = st.ReplaceAll(ctx, []LLMConfigRecord{
		{ID: "deepseek", Provider: "deepseek", Model: "deepseek-chat", MultimodalEnabled: true},
		{ID: "qwen", Provider: "qwen", Model: "qwen3-32b", ThinkingBudget: &budget},
		{ID: "mock", Provider: "mock", Mock: true},
	}, map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	key, err = st.ResolveAPIKey(ctx, "deepseek")
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-aaa" {
		t.Fatalf("key after keep = %q", key)
	}
}

func TestLLMConfigStoreMigratesThinkingBudget(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "llm_configs.db")
	box, err := OpenSecretBox(dir)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := box.Encrypt("sk-existing")
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a database created before thinking_budget existed.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE llm_configs (
		id TEXT PRIMARY KEY,
		sort_order INTEGER NOT NULL,
		provider TEXT NOT NULL,
		base_url TEXT NOT NULL DEFAULT '',
		model TEXT NOT NULL DEFAULT '',
		api_key_ciphertext TEXT NOT NULL DEFAULT '',
		mock INTEGER NOT NULL DEFAULT 0,
		thinking TEXT NOT NULL DEFAULT '',
		reasoning_effort TEXT NOT NULL DEFAULT '',
		multimodal_enabled INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO llm_configs
		(id, sort_order, provider, model, api_key_ciphertext, thinking, reasoning_effort, created_at, updated_at)
		VALUES ('legacy', 0, 'openai', 'o3', ?, 'enabled', 'high', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`, ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	st, err := OpenLLMConfigs(dbPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != "legacy" || list[0].ReasoningEffort != "high" || list[0].Thinking != "enabled" || list[0].ThinkingBudget != nil {
		t.Fatalf("migrated legacy record = %+v", list)
	}
	key, err := st.ResolveAPIKey(ctx, "legacy")
	if err != nil || key != "sk-existing" {
		t.Fatalf("legacy key = %q, err = %v", key, err)
	}

	budget := 12000
	if err := st.ReplaceAll(ctx, []LLMConfigRecord{{
		ID: "legacy", Provider: "openai", Model: "o3", Thinking: "enabled", ReasoningEffort: "high", ThinkingBudget: &budget,
	}}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = OpenLLMConfigs(dbPath, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	list, err = st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ReasoningEffort != "high" || list[0].ThinkingBudget == nil || *list[0].ThinkingBudget != budget {
		t.Fatalf("reopened record = %+v", list)
	}
	key, err = st.ResolveAPIKey(ctx, "legacy")
	if err != nil || key != "sk-existing" {
		t.Fatalf("reopened key = %q, err = %v", key, err)
	}
}
