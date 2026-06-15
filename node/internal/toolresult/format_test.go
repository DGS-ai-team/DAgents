package toolresult

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackage_shortNoSpill(t *testing.T) {
	root := t.TempDir()
	res, err := Package(DefaultConfig(root), "sess-1", "call-1", "bash_run", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if res.Spilled || res.SpillPath != "" {
		t.Fatalf("unexpected spill: %+v", res)
	}
	if res.ForClient != "hello" || res.ForHistory != "hello" {
		t.Fatalf("content mismatch: %+v", res)
	}
}

func TestPackage_nonListedToolPassthrough(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 20000)
	res, err := Package(DefaultConfig(root), "s", "c", "read_file", long)
	if err != nil {
		t.Fatal(err)
	}
	if res.ForHistory != long || res.Spilled {
		t.Fatal("read_file should pass through")
	}
}

func TestPackage_longBashSpillsAndHeadTail(t *testing.T) {
	root := t.TempDir()
	// 50000 ASCII × 0.3 = 15000 tokens > 12000
	long := strings.Repeat("o", 50000)
	cfg := DefaultConfig(root)
	cfg.MaxHistoryTokens = 12000
	res, err := Package(cfg, "sess-a", "call-b", "bash_run", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled || res.SpillPath == "" {
		t.Fatalf("expected spill: %+v", res)
	}
	if res.ForClient != long {
		t.Fatal("client must get full normalized output")
	}
	if EstimateTokens(res.ForHistory) > float64(cfg.MaxHistoryTokens)+80 {
		t.Fatalf("history token estimate too high: %v", EstimateTokens(res.ForHistory))
	}
	if !strings.Contains(res.ForHistory, "tokens") {
		t.Fatalf("missing token hint: %q", res.ForHistory)
	}
	if !strings.Contains(res.ForHistory, res.SpillPath) {
		t.Fatalf("missing path in hint: %q", res.ForHistory)
	}
	abs := filepath.Join(root, filepath.FromSlash(res.SpillPath))
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != long {
		t.Fatal("spill file content mismatch")
	}
}
