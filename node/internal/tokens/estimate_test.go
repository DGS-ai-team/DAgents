package tokens

import (
	"strings"
	"testing"
)

func TestEstimate_deepseekHeuristic(t *testing.T) {
	if got := Estimate("0123456789"); got < 2.9 || got > 3.1 {
		t.Fatalf("ascii tokens=%v want ~3", got)
	}
	if got := Estimate("一二三四五六七八九十"); got < 5.9 || got > 6.1 {
		t.Fatalf("han tokens=%v want ~6", got)
	}
}

func TestEstimateInt_rounds(t *testing.T) {
	if got := EstimateInt("0123456789"); got != 3 {
		t.Fatalf("EstimateInt = %d want 3", got)
	}
}

func TestClipToTokenBudget(t *testing.T) {
	text := "测"
	for Estimate(text) <= 1200 {
		text += "测"
	}
	clipped, ok := ClipToTokenBudget(text, 1200)
	if !ok {
		t.Fatal("expected truncation")
	}
	if Estimate(clipped) > 1200.1 {
		t.Fatalf("clipped tokens=%v", Estimate(clipped))
	}
	if len(clipped) >= len(text) {
		t.Fatal("clipped should be shorter")
	}
}

func TestTakeSuffixForTokenBudget(t *testing.T) {
	s := "0123456789"
	got := TakeSuffixForTokenBudget(s, 1.5)
	if got == "" {
		t.Fatal("expected non-empty suffix")
	}
	if Estimate(got) > 1.6 {
		t.Fatalf("suffix tokens=%v", Estimate(got))
	}
	if !strings.HasSuffix(s, got) {
		t.Fatalf("suffix %q not from end of %q", got, s)
	}
}
