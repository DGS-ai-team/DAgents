package store

import (
	"context"
	"os"
	"path/filepath"
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
	info, err := os.Stat(filepath.Join(dir, secretKeyFileName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("key file perms too open: %v", info.Mode())
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
	err = st.ReplaceAll(ctx, []LLMConfigRecord{
		{ID: "deepseek", Provider: "deepseek", Model: "deepseek-chat"},
		{ID: "mock", Provider: "mock", Mock: true},
	}, map[string]string{"deepseek": "sk-aaa"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "deepseek" || !list[0].HasAPIKey() {
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
