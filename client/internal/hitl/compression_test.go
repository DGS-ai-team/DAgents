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
}
