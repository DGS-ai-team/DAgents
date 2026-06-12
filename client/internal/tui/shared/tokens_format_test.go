package shared

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func TestFormatInputStripTokens(t *testing.T) {
	tests := []struct {
		tokens int
		want   string
	}{
		{-1, ""},
		{0, "ctx 0"},
		{999, "ctx 999"},
		{1234, "ctx 1,234"},
		{9999, "ctx 9,999"},
		{10000, "ctx 10k"},
		{12345, "ctx 12.3k"},
	}
	for _, tc := range tests {
		got := FormatInputStripTokens(tc.tokens)
		if got != tc.want {
			t.Errorf("FormatInputStripTokens(%d) = %q, want %q", tc.tokens, got, tc.want)
		}
	}
}

func TestParseUsageRoundAndInlineFormat(t *testing.T) {
	s := ParseUsageRound(map[string]any{
		"round_prompt_tokens":     float64(1200),
		"round_completion_tokens": float64(80),
		"round_reasoning_tokens":  float64(42),
	})
	if !s.HasData || s.PromptTokens != 1200 || s.ReasoningTokens != 42 {
		t.Fatalf("round snapshot = %+v", s)
	}
	inline := FormatInlineUsage(s)
	if inline != " · ↑1,200 ↓80 · think 42" {
		t.Fatalf("inline = %q", inline)
	}
}

func TestApplyRoundUsageToAssistantPartial(t *testing.T) {
	tr := NewTranscript(0)
	tr.AppendPartial("assistant", "hello")
	tr.ApplyRoundUsage(" · ↑10 ↓2")
	lines := tr.Lines()
	want := "[assistant] hello" + usageStorageSep + " · ↑10 ↓2"
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("lines = %v, want %q", lines, want)
	}
	display := FormatTranscriptLineForDisplay(lines[0], 40)
	if !strings.Contains(display, "hello") || !strings.Contains(stripANSI(display), "↑10 ↓2") {
		t.Fatalf("display = %q", display)
	}
}

func TestApplyRoundUsageMultilineStorageSingleLine(t *testing.T) {
	tr := NewTranscript(0)
	tr.AppendPartial("assistant", "line1\nline2\n")
	tr.ApplyRoundUsage(" · ↑10 ↓2")
	lines := tr.Lines()
	want := "[assistant] line1\nline2" + usageStorageSep + " · ↑10 ↓2"
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("lines = %v", lines)
	}
}

func TestApplyRoundUsageSkipsReasoning(t *testing.T) {
	tr := NewTranscript(0)
	tr.AppendPartial("reasoning", "think hard")
	tr.ApplyRoundUsage(" · ↑10 ↓2")
	if len(tr.Lines()) != 0 {
		t.Fatalf("reasoning should not be finalized with usage: %v", tr.Lines())
	}
	tr.AppendPartial("assistant", "answer")
	tr.FinishPartial("assistant")
	lines := tr.Lines()
	want := "[assistant] answer" + usageStorageSep + " · ↑10 ↓2"
	if len(lines) != 1 || lines[0] != want {
		t.Fatalf("lines = %v", lines)
	}
}

func TestParseUsageStrip(t *testing.T) {
	s := ParseUsageStrip(map[string]any{
		"prompt_tokens":           float64(100),
		"completion_tokens":       float64(20),
		"prompt_cache_hit_tokens": float64(80),
	})
	if !s.HasData || s.PromptTokens != 100 || s.CompletionTokens != 20 || s.CacheHitTokens != 80 {
		t.Fatalf("snapshot = %+v", s)
	}
	got := FormatInputStripUsage(s)
	if got != "↑100 ↓20 · hit 80 (80%)" {
		t.Fatalf("format = %q", got)
	}
}

func TestParseUsageStripReasoningFromDetails(t *testing.T) {
	s := ParseUsageStrip(map[string]any{
		"prompt_tokens":     float64(10),
		"completion_tokens": float64(5),
		"completion_tokens_details": map[string]any{
			"reasoning_tokens": float64(42),
		},
	})
	if s.ReasoningTokens != 42 {
		t.Fatalf("reasoning = %d", s.ReasoningTokens)
	}
	got := FormatInputStripUsage(s)
	if !strings.Contains(got, "think 42") {
		t.Fatalf("format = %q", got)
	}
}

func TestParseUsageStripCachedFallback(t *testing.T) {
	s := ParseUsageStrip(map[string]any{
		"prompt_tokens":        float64(10),
		"completion_tokens":    float64(2),
		"prompt_cached_tokens": float64(6),
	})
	if s.CacheHitTokens != 6 {
		t.Fatalf("cache hit = %d", s.CacheHitTokens)
	}
}

func TestFormatInputStripRight(t *testing.T) {
	got := FormatInputStripRight("开启 · high", "↑1.2k ↓300")
	if got != "thinking 开启 · high · ↑1.2k ↓300" {
		t.Fatalf("got %q", got)
	}
	if FormatInputStripRight("", "↑1k") != "↑1k" {
		t.Fatal("usage only")
	}
	if FormatInputStripRight("关闭", "") != "thinking 关闭" {
		t.Fatal("thinking only")
	}
}

func TestFormatInputStripLineRightAlignsUsage(t *testing.T) {
	left := "临时 Agent: 2 活跃 · 1 待审批"
	right := FormatInputStripUsage(UsageStripSnapshot{
		PromptTokens: 7329, CompletionTokens: 64, ReasoningTokens: 42, HasData: true,
	})
	const width = 80
	line := FormatInputStripLine(left, right, width)
	if runewidth.StringWidth(line) > width {
		t.Fatalf("line too wide: w=%d %q", runewidth.StringWidth(line), line)
	}
	if !strings.HasSuffix(strings.TrimRight(line, " "), right) {
		t.Fatalf("usage not intact at end: %q", line)
	}
	if strings.Contains(line, "think\n") || strings.TrimSpace(line) == "42" {
		t.Fatalf("usage split: %q", line)
	}
}

func TestFormatInputStripLineTruncatesLeftWhenNarrow(t *testing.T) {
	left := "临时 Agent: 9 活跃 · 9 待审批 （队列 9）"
	right := "↑7,329 ↓64 · think 42"
	line := FormatInputStripLine(left, right, 40)
	if !strings.HasSuffix(strings.TrimRight(line, " "), right) {
		t.Fatalf("usage lost when truncating left: %q", line)
	}
	if runewidth.StringWidth(line) > 40 {
		t.Fatalf("exceeds width: %q", line)
	}
}
