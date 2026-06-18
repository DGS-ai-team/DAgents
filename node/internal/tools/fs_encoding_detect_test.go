package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestDetectEncodingGBKFileWithoutArg(t *testing.T) {
	dir := t.TempDir()
	// 模拟 Linux 默认 utf-8 配置下读取 GBK 遗留文件
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte("中文遗留文件"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.txt"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(context.Background(), "read_file", encodeToolArgs(t, map[string]any{
		"path": "legacy.txt",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "中文遗留文件") {
		t.Fatalf("expected decoded text: %q", out)
	}
	if !strings.Contains(out, "编码来源: 检测") {
		t.Fatalf("expected detect source: %q", out)
	}
	if !strings.Contains(out, "文件编码: gbk") && !strings.Contains(out, "文件编码: gb18030") {
		t.Fatalf("expected gbk family encoding: %q", out)
	}
}

func TestPathEncodingCacheOnSecondRead(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30, "", "gbk")
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte("缓存测试"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	args := encodeToolArgs(t, map[string]any{"path": "c.txt"})
	out1, err := reg.Execute(context.Background(), "read_file", args)
	if err != nil || !strings.Contains(out1, "缓存测试") {
		t.Fatalf("first read: err=%v out=%q", err, out1)
	}
	out2, err := reg.Execute(context.Background(), "read_file", args)
	if err != nil || !strings.Contains(out2, "缓存测试") {
		t.Fatalf("second read: err=%v out=%q", err, out2)
	}
	if !strings.Contains(out2, "编码来源: 缓存") {
		t.Fatalf("expected cache hit on second read: %q", out2)
	}
}

func TestTextDecodeScorePrefersValidUTF8(t *testing.T) {
	if textDecodeScore("hello 世界") <= textDecodeScore("æ\x9d\x82ä¹±") {
		t.Fatal("valid utf-8 should score higher than mojibake")
	}
}
