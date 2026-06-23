package tools

import (
	"testing"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

func TestDecodeShellOutputGBK(t *testing.T) {
	src := []byte("中文输出")
	gbk, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), src)
	if err != nil {
		t.Fatal(err)
	}
	if utf8.Valid(gbk) {
		t.Fatal("expected non-utf8 gbk bytes")
	}
	got := decodeShellOutput(gbk, "gbk")
	if got != "中文输出" {
		t.Fatalf("decode gbk = %q, want 中文输出", got)
	}
}

func TestDecodeShellOutputUTF8Passthrough(t *testing.T) {
	src := "hello 世界"
	got := decodeShellOutput([]byte(src), "utf-8")
	if got != src {
		t.Fatalf("got %q", got)
	}
}

func TestResolveShellOutputEncodingOverride(t *testing.T) {
	if got := resolveShellOutputEncoding(shellCmd, "utf-8"); got != "utf-8" {
		t.Fatalf("override = %q", got)
	}
}

func TestResolveShellOutputEncodingDefaultsUTF8(t *testing.T) {
	for _, st := range []shellType{shellCmd, shellBash, shellPowerShell} {
		if got := resolveShellOutputEncoding(st, ""); got != "utf-8" {
			t.Fatalf("%s default = %q, want utf-8", st, got)
		}
	}
}

func TestDecodeShellOutputGBKPrefersValidUTF8(t *testing.T) {
	src := "网页标题：测试"
	got := decodeShellOutput([]byte(src), "gbk")
	if got != src {
		t.Fatalf("valid utf-8 under gbk config = %q, want %q", got, src)
	}
}

func TestNormalizeOutputEncoding(t *testing.T) {
	cases := map[string]string{
		"UTF-8":   "utf-8",
		"cp936":   "gbk",
		"GB18030": "gb18030",
		"":        "",
	}
	for in, want := range cases {
		if got := normalizeOutputEncoding(in); got != want {
			t.Fatalf("%q => %q, want %q", in, got, want)
		}
	}
}
