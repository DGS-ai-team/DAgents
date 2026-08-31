package compression

import (
	"strings"
	"testing"
	"time"
)

func TestFinalizeCompressionSummary_appendsJournalHint(t *testing.T) {
	at := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	got := FinalizeCompressionSummary("summary body", "sess-a", true, at)
	if !strings.HasPrefix(got, "summary body") {
		t.Fatalf("got = %q", got)
	}
	if !strings.Contains(got, "Node 已将原始消息记录到 <runtime_root>/history/20260621/sess-a.jsonl") {
		t.Fatalf("got = %q", got)
	}
}

func TestFinalizeCompressionSummary_skipsWhenJournalDisabled(t *testing.T) {
	got := FinalizeCompressionSummary("summary body", "sess-a", false, time.Now())
	if got != "summary body" {
		t.Fatalf("got = %q", got)
	}
}

func TestFinalizeCompressionSummary_skipsEmptySession(t *testing.T) {
	got := FinalizeCompressionSummary("summary body", "  ", true, time.Now())
	if got != "summary body" {
		t.Fatalf("got = %q", got)
	}
}
