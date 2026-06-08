package shared

import (
	"regexp"
	"strings"
	"testing"
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

func stripANSI(s string) string {
	return regexp.MustCompile("\033\\[[0-9;]*m").ReplaceAllString(s, "")
}

func TestApplyRoundUsageToAssistantPartial(t *testing.T) {
	tr := NewTranscript(0)
	tr.AppendPartial("assistant", "hello")
	tr.ApplyRoundUsage(" · ↑10 ↓2")
	lines := tr.Lines()
	if len(lines) != 1 || stripANSI(lines[0]) != "[assistant] hello · ↑10 ↓2" {
		t.Fatalf("lines = %v", lines)
	}
	if !strings.Contains(lines[0], "\033[90m") {
		t.Fatalf("expected gray ANSI in %q", lines[0])
	}
}

func TestApplyRoundUsageMultilineSameLine(t *testing.T) {
	tr := NewTranscript(0)
	tr.AppendPartial("assistant", "line1\nline2\n")
	tr.ApplyRoundUsage(" · ↑10 ↓2")
	lines := tr.Lines()
	if len(lines) != 1 || stripANSI(lines[0]) != "[assistant] line1\nline2 · ↑10 ↓2" {
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
	if len(lines) != 1 || stripANSI(lines[0]) != "[assistant] answer · ↑10 ↓2" {
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
