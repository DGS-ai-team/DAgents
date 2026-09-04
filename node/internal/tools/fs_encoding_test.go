package tools

import (
	"bytes"
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
	if got := reg2.resolveFileEncoding(nil); got != "utf-8" {
		t.Fatalf("platform default = %q, want utf-8", got)
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
	if !strings.Contains(out, "文件编码: gbk") && !strings.Contains(out, "文件编码: gb18030") {
		t.Fatalf("read_file header missing encoding: %q", out)
	}
	if !strings.Contains(out, "编码来源:") {
		t.Fatalf("read_file header missing source: %q", out)
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

func TestWriteFileEncodingGBKWithUnicodeBeyondGBK(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30, "", "gbk")
	if err != nil {
		t.Fatal(err)
	}
	content := "中文→emoji🎉"
	out, err := reg.Execute(context.Background(), "write_file", encodeToolArgs(t, map[string]any{
		"path":    "unicode.txt",
		"content": content,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ERROR:") {
		t.Fatalf("write_file failed: %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "unicode.txt"))
	if err != nil {
		t.Fatal(err)
	}
	text, ok := transcodeShellOutput(raw, "gb18030")
	if !ok {
		text, ok = transcodeShellOutput(raw, "gbk")
	}
	if !ok || !strings.Contains(text, "中文") {
		t.Fatalf("decoded = %q ok=%v", text, ok)
	}
}

func TestSearchReplacePreservesUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	original := append(append([]byte(nil), utf8BOMPrefix...), []byte("# script\r\nold line\r\n")...)
	if err := os.WriteFile(filepath.Join(dir, "script.ps1"), original, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "search_replace", encodeToolArgs(t, map[string]any{
		"path":       "script.ps1",
		"old_string": "old line",
		"new_string": "new line",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "成功: 是") {
		t.Fatalf("search_replace = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "script.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 3 || raw[0] != 0xEF || raw[1] != 0xBB || raw[2] != 0xBF {
		t.Fatalf("UTF-8 BOM missing after replace: % x", raw[:min(8, len(raw))])
	}
	if !strings.Contains(string(raw[3:]), "new line") {
		t.Fatalf("content = %q", string(raw[3:]))
	}
}

func TestWriteFileNewFileNoBOM(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "write_file", encodeToolArgs(t, map[string]any{
		"path":    "new.txt",
		"content": "hello",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ERROR:") {
		t.Fatalf("write_file = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(raw, utf8BOMPrefix) {
		t.Fatal("new .txt should not get BOM by default")
	}
}

func TestWriteFileNewPs1GetsUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "write_file", encodeToolArgs(t, map[string]any{
		"path":    "script.ps1",
		"content": "# 中文脚本\nWrite-Host 'ok'",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "ERROR:") {
		t.Fatalf("write_file = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "script.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, utf8BOMPrefix) {
		t.Fatalf("new .ps1 should get UTF-8 BOM: % x", raw[:min(8, len(raw))])
	}
}

func TestWriteFileNewCmdGetsUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Execute(context.Background(), "write_file", encodeToolArgs(t, map[string]any{
		"path":    "run.cmd",
		"content": "@echo off\r\necho ok",
	}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "run.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, utf8BOMPrefix) {
		t.Fatalf("new .cmd should get UTF-8 BOM: % x", raw[:min(8, len(raw))])
	}
}

func TestWriteFilePs1GBKNoUTF8BOM(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30, "", "gbk")
	if err != nil {
		t.Fatal(err)
	}
	_, err = reg.Execute(context.Background(), "write_file", encodeToolArgs(t, map[string]any{
		"path":     "legacy.ps1",
		"content":  "echo test",
		"encoding": "gbk",
	}))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "legacy.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(raw, utf8BOMPrefix) {
		t.Fatal("gbk .ps1 should not get UTF-8 BOM")
	}
}

func TestSearchReplacePs1WithoutBOMAddsBOM(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hook.ps1"), []byte("old\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "search_replace", encodeToolArgs(t, map[string]any{
		"path":       "hook.ps1",
		"old_string": "old",
		"new_string": "new",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "成功: 是") {
		t.Fatalf("search_replace = %q", out)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "hook.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, utf8BOMPrefix) {
		t.Fatalf("search_replace on .ps1 should add UTF-8 BOM: % x", raw[:min(8, len(raw))])
	}
}

func TestEncodeFileContentWithBOM(t *testing.T) {
	raw, err := encodeFileContentWithBOM("test", "utf-8", true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, utf8BOMPrefix) {
		t.Fatalf("missing BOM: % x", raw)
	}
	raw, err = encodeFileContentWithBOM("test", "gbk", true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(raw, utf8BOMPrefix) {
		t.Fatal("gbk should not prepend UTF-8 BOM")
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
