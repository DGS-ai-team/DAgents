package api

import (
	"strings"
	"testing"
)

func TestTruncateContextPreview(t *testing.T) {
	t.Parallel()
	const emoji = "🌤️"
	long := strings.Repeat("天", 150) + emoji + strings.Repeat("气", 60)
	out := truncateContextPreview(long, 200)
	if len([]rune(out)) != 200 {
		t.Fatalf("rune len = %d, want 200", len([]rune(out)))
	}
	if strings.Contains(out, "\ufffd") {
		t.Fatalf("contains replacement char: %q", out)
	}
	if !strings.HasSuffix(out, "…") {
		t.Fatalf("expected ellipsis suffix, got %q", out)
	}
	if truncateContextPreview("短文本", 200) != "短文本" {
		t.Fatal("short text should pass through")
	}
}
