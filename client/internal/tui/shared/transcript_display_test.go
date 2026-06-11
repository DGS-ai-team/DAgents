package shared

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

func TestFormatTranscriptLineForDisplayUsageRightAlign(t *testing.T) {
	line := "[assistant] short" + usageStorageSep + " · ↑1,200 ↓80"
	got := FormatTranscriptLineForDisplay(line, 50)
	plain := stripANSI(got)
	if !strings.HasPrefix(plain, "● short") {
		t.Fatalf("missing dot prefix: %q", plain)
	}
	if !strings.Contains(plain, "↑1,200 ↓80") {
		t.Fatalf("missing usage: %q", plain)
	}
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected usage on dedicated line, got %q", plain)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "↑1,200 ↓80") {
		t.Fatalf("usage not on last line: %q", plain)
	}
	if strings.TrimLeft(last, " ") == last {
		t.Fatalf("expected leading padding on usage line: %q", last)
	}
}

func TestFormatTranscriptLineForDisplayUsageDedicatedLine(t *testing.T) {
	long := strings.Repeat("x", 45)
	line := "[assistant] " + long + usageStorageSep + " · ↑10 ↓2"
	got := FormatTranscriptLineForDisplay(line, 50)
	plain := stripANSI(got)
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected usage on dedicated line, got %q", plain)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "↑10 ↓2") {
		t.Fatalf("usage not on last line: %q", last)
	}
	if strings.TrimLeft(last, " ") == last {
		t.Fatalf("expected leading padding on usage line: %q", last)
	}
}

func TestSanitizeTerminalText_stripsControlAndExpandsTab(t *testing.T) {
	raw := "50\t| sailfish" + usageStorageSep + "leak"
	got := sanitizeTerminalText(raw)
	if strings.Contains(got, "\t") || strings.Contains(got, usageStorageSep) {
		t.Fatalf("controls remain: %q", got)
	}
	if !strings.HasPrefix(got, "50    | sailfish") {
		t.Fatalf("tab expand = %q", got)
	}
}

func TestFormatTranscriptLineForDisplayRoleDots(t *testing.T) {
	cases := []struct {
		line, wantSub string
	}{
		{"[user] hi", "● hi"},
		{"[reasoning] think", "● think"},
		{"[tool] ✓ bash", "● ✓ bash"},
	}
	for _, tc := range cases {
		got := stripANSI(FormatTranscriptLineForDisplay(tc.line, 80))
		if !strings.Contains(got, tc.wantSub) {
			t.Fatalf("line %q => %q, want contains %q", tc.line, got, tc.wantSub)
		}
	}
}

func TestFormatTranscriptLineForDisplayUsageCJKNoMidUsageWrap(t *testing.T) {
	body := "现在是 **2026年6月10日 (周三) 上午 9:10** 左右。"
	usage := FormatInlineUsage(UsageStripSnapshot{
		PromptTokens:     7329,
		CompletionTokens: 64,
		ReasoningTokens:  42,
		HasData:          true,
	})
	line := "[assistant] " + body + usageStorageSep + usage
	for _, width := range []int{70, 80, 90, 100, 120} {
		got := FormatTranscriptLineForDisplay(line, width)
		plain := stripANSI(got)
		for i, ln := range strings.Split(plain, "\n") {
			w := runewidth.StringWidth(ln)
			if w > width {
				t.Fatalf("width=%d line %d exceeds (w=%d): %q", width, i, w, ln)
			}
		}
		rendered := lipgloss.NewStyle().Width(width).Render(got)
		rplain := stripANSI(rendered)
		for i, ln := range strings.Split(rplain, "\n") {
			w := runewidth.StringWidth(ln)
			if w > width {
				t.Fatalf("width=%d lipgloss line %d exceeds (w=%d): %q", width, i, w, ln)
			}
			if strings.TrimSpace(ln) == "42" {
				t.Fatalf("width=%d orphaned usage fragment: %q full=%q", width, ln, rplain)
			}
		}
	}
}
