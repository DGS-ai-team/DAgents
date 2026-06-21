package shared

import "testing"

func TestFormatSideEffectSeqLine(t *testing.T) {
	line := FormatSideEffectSeqLine("已入库", []any{float64(1), float64(2)})
	if line == "" {
		t.Fatal("expected non-empty line")
	}
	if line != "旁路回调 已入库: #1, #2" {
		t.Fatalf("line = %q", line)
	}
	if FormatSideEffectSeqLine("已失效", nil) != "" {
		t.Fatal("nil seqs should be empty")
	}
}
