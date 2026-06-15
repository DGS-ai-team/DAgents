package hitl

import (
	"strings"
	"testing"
)

func TestFormatContextCompression(t *testing.T) {
	line := FormatContextCompression("context_compression_blocking", map[string]any{
		"phase": "start", "compressed_message_count": 4,
	})
	if !strings.Contains(line, "blocking") || !strings.Contains(line, "阻塞") {
		t.Fatalf("blocking start = %q", line)
	}
	line = FormatContextCompression("context_compression_silent", map[string]any{
		"phase": "end", "status": "applied", "compressed_message_count": 3,
	})
	if !strings.Contains(line, "silent") || !strings.Contains(line, "应用") {
		t.Fatalf("silent end = %q", line)
	}
	line = FormatContextCompression("context_compression_blocking", map[string]any{
		"phase":                    "end",
		"status":                   "applied",
		"compressed_message_count": 4,
		"prompt_tokens":            1200,
		"completion_tokens":        480,
		"token_reduction_rate":     0.6,
		"prompt_cache_hit_tokens":  900,
		"prompt_cache_miss_tokens": 100,
		"prompt_cache_hit_rate":    0.9,
	})
	if !strings.Contains(line, "1200→completion 480") && !strings.Contains(line, "prompt 1200") {
		t.Fatalf("token range = %q", line)
	}
	if !strings.Contains(line, "60%") || !strings.Contains(line, "cache hit") {
		t.Fatalf("metrics = %q", line)
	}
}
