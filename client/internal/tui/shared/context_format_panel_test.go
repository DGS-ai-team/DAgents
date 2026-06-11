package shared

import (
	"strings"
	"testing"
	"unicode/utf8"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

func TestFormatSessionContextPanel_containsSections(t *testing.T) {
	text := FormatSessionContextPanel(&nodeapi.SessionContext{
		SessionID:    "s1",
		SystemPrompt: "hello world",
		MessagesCount: 3,
	})
	if !strings.Contains(text, "Session Context") {
		t.Fatalf("missing title: %q", text)
	}
	if !strings.Contains(text, "system_prompt") {
		t.Fatalf("missing section: %q", text)
	}
}

func TestWrapLines_doesNotSplitUTF8(t *testing.T) {
	// 72 显示宽 ≈ 36 个汉字；40 个汉字应折为两行且保持合法 UTF-8。
	line := strings.Repeat("中", 40)
	parts := wrapLines(line, 72)
	if len(parts) != 2 {
		t.Fatalf("wrap parts = %d, want 2: %v", len(parts), parts)
	}
	for i, p := range parts {
		if !utf8.ValidString(p) {
			t.Fatalf("part %d invalid UTF-8: %q", i, p)
		}
	}
	if parts[0] != strings.Repeat("中", 36) {
		t.Fatalf("first part = %d runes", len(parts[0]))
	}
	if parts[1] != strings.Repeat("中", 4) {
		t.Fatalf("second part = %q", parts[1])
	}
}
