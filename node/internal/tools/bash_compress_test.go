package tools

import (
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeCLIOutputANSIAndBlankLines(t *testing.T) {
	raw := "\x1b[31mhello\x1b[0m\n\n\nworld\r\n"
	out := sanitizeCLIOutput(raw)
	if strings.Contains(out, "\x1b") {
		t.Fatalf("ansi not stripped: %q", out)
	}
	if strings.Contains(out, "\n\n\n") {
		t.Fatalf("too many consecutive blank lines: %q", out)
	}
	if !strings.Contains(out, "hello") || !strings.Contains(out, "world") {
		t.Fatalf("content lost: %q", out)
	}
}

func TestSanitizeCLIOutputDedupLines(t *testing.T) {
	raw := "same\nsame\nsame\nother\n"
	out := sanitizeCLIOutput(raw)
	if !strings.Contains(out, "repeated 3 identical") {
		t.Fatalf("dedup note missing: %q", out)
	}
	if !strings.Contains(out, "other") {
		t.Fatalf("other line missing: %q", out)
	}
}

func TestClipTextRunesUTF8(t *testing.T) {
	s := "你好世界测试"
	clipped, truncated := clipTextRunes(s, 3)
	if !truncated {
		t.Fatal("expected truncation")
	}
	if utf8.RuneCountInString(clipped) != 3 {
		t.Fatalf("runes=%d text=%q", utf8.RuneCountInString(clipped), clipped)
	}
}

func TestCompressBashStreamEnabled(t *testing.T) {
	cfg := DefaultBashCompressConfig()
	raw := "\x1b[32mok\x1b[0m\nok\nok\n"
	out, meta := compressBashStream(cfg, raw, 100)
	if meta.inRunes <= meta.outRunes && !meta.sanitized {
		t.Fatalf("expected sanitize effect meta=%+v out=%q", meta, out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("ansi leaked: %q", out)
	}
}

func TestCompressBashStreamDisabledSkipsSanitize(t *testing.T) {
	cfg := DefaultBashCompressConfig()
	cfg.Enabled = false
	raw := "\x1b[32mok\x1b[0m"
	out, meta := compressBashStream(cfg, raw, 100)
	if meta.sanitized {
		t.Fatal("should not sanitize when disabled")
	}
	if !strings.Contains(out, "\x1b") {
		t.Fatalf("expected raw ansi when disabled: %q", out)
	}
}

func TestFormatShellCompletedOutputCompressStats(t *testing.T) {
	params := shellRunParams{
		shellType:      shellBash,
		cwd:            "/tmp",
		timeoutSec:     30,
		outputEncoding: "utf-8",
		compress:       DefaultBashCompressConfig(),
	}
	out, stats := formatShellCompletedOutput(params, "\x1b[31m"+strings.Repeat("x", 200)+"\x1b[0m\n", "", nil, nil)
	if strings.Contains(out, "[COMPRESS]") {
		t.Fatalf("compress meta should not be in content: %q", out)
	}
	if strings.Contains(out, "\x1b") {
		t.Fatalf("stdout should be sanitized: %q", out)
	}
	if stats == nil || stats.SavedPct <= 0 {
		t.Fatalf("expected compress stats, got %+v", stats)
	}
}

func TestBashRunIntegrationCompress(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	reg.SetBashCompress(DefaultBashCompressConfig())
	ctx := WithToolCallID(t.Context(), "call-compress-test")
	args := `{"command":"yes same | head -n 100","shell_type":"bash"}`
	if runtime.GOOS == "windows" {
		args = `{"command":"1..100 | ForEach-Object { Write-Output 'same' }","shell_type":"powershell"}`
	}
	out, err := reg.Execute(ctx, "bash_run", args)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "[COMPRESS]") {
		t.Fatalf("compress meta should not be in content: %q", out)
	}
	fields := reg.TakeBashCompressStatsForCall("call-compress-test")
	if fields == nil {
		t.Fatal("expected compress SSE fields")
	}
	if pct, _ := fields["output_compress_saved_pct"].(int); pct <= 0 {
		t.Fatalf("expected saved pct, fields=%v", fields)
	}
}
