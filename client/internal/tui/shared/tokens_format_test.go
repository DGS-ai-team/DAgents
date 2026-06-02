package shared

import "testing"

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
	if got != "↑100 ↓20 · hit 80" {
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
