package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestResolveFileEncodingPriority(t *testing.T) {
	reg, err := NewRegistry(t.TempDir(), 30, "utf-8", "gbk")
	if err != nil {
		t.Fatal(err)
	}
	arg := "utf-8"
	if got := reg.resolveFileEncoding(&arg); got != "utf-8" {
		t.Fatalf("arg override = %q", got)
	}
	if got := reg.resolveFileEncoding(nil); got != "gbk" {
		t.Fatalf("config default = %q", got)
	}
	reg2, err := NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	got := reg2.resolveFileEncoding(nil)
	if got != defaultFileEncoding() {
		t.Fatalf("platform default = %q, want %q", got, defaultFileEncoding())
	}
}

func TestReadWriteFileEncodingGBK(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30, "", "gbk")
	if err != nil {
		t.Fatal(err)
	}

	writeRaw, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte("中文内容"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "legacy.txt")
	if err := os.WriteFile(path, writeRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := reg.Execute(context.Background(), "read_file", encodeToolArgs(t, map[string]any{
		"path": "legacy.txt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "中文内容") {
		t.Fatalf("read_file = %q", out)
	}
	if !strings.Contains(out, "文件编码: gbk") {
		t.Fatalf("read_file header missing encoding: %q", out)
	}

	out, err = reg.Execute(context.Background(), "write_file", encodeToolArgs(t, map[string]any{
		"path":    "out.txt",
		"content": "写入测试",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "encoding=gbk") {
		t.Fatalf("write_file = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	text, ok := transcodeShellOutput(raw, "gbk")
	if !ok || text != "写入测试" {
		t.Fatalf("disk bytes not gbk: %q ok=%v", text, ok)
	}
}

func TestSearchReplaceEncodingGBK(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30, "", "gbk")
	if err != nil {
		t.Fatal(err)
	}
	oldRaw, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte("旧词\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "edit.txt"), oldRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := reg.Execute(context.Background(), "search_replace", encodeToolArgs(t, map[string]any{
		"path":       "edit.txt",
		"old_string": "旧词",
		"new_string": "新词",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "成功: 是") {
		t.Fatalf("search_replace = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "edit.txt"))
	if err != nil {
		t.Fatal(err)
	}
	text, ok := transcodeShellOutput(raw, "gbk")
	if !strings.Contains(text, "新词") {
		t.Fatalf("after replace = %q ok=%v", text, ok)
	}
}

func encodeToolArgs(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
