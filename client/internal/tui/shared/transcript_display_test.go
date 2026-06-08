package shared

import (
	"strings"
	"testing"
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
	// usage 应在行尾附近（右对齐）
	idxShort := strings.Index(plain, "short")
	idxUsage := strings.Index(plain, "↑1,200")
	if idxUsage <= idxShort {
		t.Fatalf("usage should follow content: %q", plain)
	}
}

func TestFormatTranscriptLineForDisplayUsageWrapLine(t *testing.T) {
	long := strings.Repeat("x", 45)
	line := "[assistant] " + long + usageStorageSep + " · ↑10 ↓2"
	got := FormatTranscriptLineForDisplay(line, 50)
	plain := stripANSI(got)
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapped usage line, got %q", plain)
	}
	last := lines[len(lines)-1]
	if !strings.Contains(last, "↑10 ↓2") {
		t.Fatalf("usage not on last line: %q", last)
	}
	if strings.TrimLeft(last, " ") == last {
		t.Fatalf("expected leading padding on usage line: %q", last)
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
