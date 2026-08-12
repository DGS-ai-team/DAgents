package tools

import (
	"strings"
	"testing"
)

func TestFormatHitLinesMultiline(t *testing.T) {
	text := "a\nb\nc\n"
	if got := formatHitLines(text, "b\nc"); got != "多行" {
		t.Fatalf("multiline needle: got %q want 多行", got)
	}
	if got := formatHitLines(text, "b"); got != "2" {
		t.Fatalf("single-line needle: got %q want 2", got)
	}
	if got := formatHitLines(text, ""); got != "多行" {
		t.Fatalf("empty needle: got %q want 多行", got)
	}
}

func TestFormatSearchReplacePreviewMultilineHint(t *testing.T) {
	out := formatSearchReplacePreview("old\nline", "new\nline", 1, "多行")
	if !strings.Contains(out, "@@ 多行 @@") {
		t.Fatalf("expected @@ 多行 @@, got %q", out)
	}
	if strings.Contains(out, "未知") {
		t.Fatalf("must not show 未知: %q", out)
	}
	if strings.Contains(out, "@@ 行 多行 @@") {
		t.Fatalf("should not use 行 多行 wording: %q", out)
	}
}

func TestSearchReplaceMultilinePreviewUsesDuoHang(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir, 30)
	if err != nil {
		t.Fatal(err)
	}
	ctx := WithSession(t.Context(), "sess-sr")
	if _, err := reg.Execute(ctx, "write_file", `{"path":"m.txt","content":"line1\noldA\noldB\nline4\n"}`); err != nil {
		t.Fatal(err)
	}
	out, err := reg.Execute(ctx, "search_replace", `{"path":"m.txt","old_string":"oldA\noldB","new_string":"newA\nnewB"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "成功: 是") || !strings.Contains(out, "替换次数: 1") {
		t.Fatalf("unexpected meta: %q", out)
	}
	if !strings.Contains(out, "@@ 多行 @@") {
		t.Fatalf("expected multiline hint, got %q", out)
	}
	if strings.Contains(out, "未知") {
		t.Fatalf("must not show 未知: %q", out)
	}
}
