package toolresult

import "testing"

func TestEstimateTokens_deepseekHeuristic(t *testing.T) {
	// 10 个 ASCII ≈ 3 tokens
	if got := EstimateTokens("0123456789"); got < 2.9 || got > 3.1 {
		t.Fatalf("ascii tokens=%v want ~3", got)
	}
	// 10 个汉字 ≈ 6 tokens
	if got := EstimateTokens("一二三四五六七八九十"); got < 5.9 || got > 6.1 {
		t.Fatalf("han tokens=%v want ~6", got)
	}
}

func TestClipToTokenBudget(t *testing.T) {
	text := "测"
	for EstimateTokens(text) <= 1200 {
		text += "测"
	}
	clipped, ok := ClipToTokenBudget(text, 1200)
	if !ok {
		t.Fatal("expected truncation")
	}
	if EstimateTokens(clipped) > 1200.1 {
		t.Fatalf("clipped tokens=%v", EstimateTokens(clipped))
	}
	if len(clipped) >= len(text) {
		t.Fatal("clipped should be shorter")
	}
}

func TestPackage_spillByTokenBudget(t *testing.T) {
	root := t.TempDir()
	// 25000 汉字 × 0.6 = 15000 tokens > 12000
	long := stringsRepeat("测", 25000)
	cfg := DefaultConfig(root)
	cfg.SpillThresholdTokens = 12000
	res, err := Package(cfg, "s", "c", "bash_run", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled {
		t.Fatal("expected spill by token estimate")
	}
}

func stringsRepeat(s string, n int) string {
	var b []byte
	chunk := []byte(s)
	for i := 0; i < n; i++ {
		b = append(b, chunk...)
	}
	return string(b)
}
