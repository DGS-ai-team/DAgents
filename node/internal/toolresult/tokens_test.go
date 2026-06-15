package toolresult

import (
	"strings"
	"testing"
)

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

func TestPackage_spillByTokenBudget(t *testing.T) {
	root := t.TempDir()
	// 25000 汉字 × 0.6 = 15000 tokens > 12000
	long := strings.Repeat("测", 25000)
	cfg := DefaultConfig(root)
	cfg.MaxHistoryTokens = 12000
	res, err := Package(cfg, "s", "c", "bash_run", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled {
		t.Fatal("expected spill by token estimate")
	}
}
