package toolresult

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
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
	res, err := Package(DefaultConfig(root), "s", "c", "write_file", long)
	if err != nil {
		t.Fatal(err)
	}
	if res.ForHistory != long || res.Spilled {
		t.Fatal("write_file should pass through")
	}
}

func TestPackage_longBashSpillsAndHeadTail(t *testing.T) {
	root := t.TempDir()
	// 50000 ASCII × 0.3 = 15000 tokens > 12000
	long := strings.Repeat("o", 50000)
	cfg := DefaultConfig(root)
	cfg.SpillThresholdTokens = 12000
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
	if tokens.Estimate(res.ForHistory) > float64(cfg.SpillThresholdTokens)+80 {
		t.Fatalf("history token estimate too high: %v", tokens.Estimate(res.ForHistory))
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

func TestPackage_longReadFileSpills(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("o", 50000)
	cfg := DefaultConfig(root)
	res, err := Package(cfg, "sess-r", "call-r", "read_file", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled {
		t.Fatal("read_file should spill when over token budget")
	}
	if res.ForClient != long {
		t.Fatal("client must get full tool output")
	}
	if !strings.Contains(res.ForHistory, "read_file") {
		t.Fatalf("hint should mention read_file paging: %q", res.ForHistory)
	}
}

func TestPackage_longGrepFileSpills(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("测", 25000)
	cfg := DefaultConfig(root)
	res, err := Package(cfg, "sess-g", "call-g", "grep_file", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled {
		t.Fatal("grep_file should spill when over token budget")
	}
}

func TestPackage_longGrepFilesSpills(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("测", 25000)
	cfg := DefaultConfig(root)
	res, err := Package(cfg, "sess-gf", "call-gf", "grep_files", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled {
		t.Fatal("grep_files should spill when over token budget")
	}
}

func TestPackage_longSearchReplaceSpills(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("o", 50000)
	cfg := DefaultConfig(root)
	res, err := Package(cfg, "sess-sr", "call-sr", "search_replace", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled {
		t.Fatal("search_replace should spill when over token budget")
	}
}

func TestPackage_longGlobFilesSpills(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("x", 50000)
	cfg := DefaultConfig(root)
	res, err := Package(cfg, "sess-gl", "call-gl", "glob_files", long)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Spilled {
		t.Fatal("glob_files should spill when over token budget")
	}
}

func TestPackage_spillByTokenBudget(t *testing.T) {
	root := t.TempDir()
	// 25000 汉字 × 0.6 = 15000 tokens > 12000
	long := strings.Repeat("测", 25000)
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

func TestDefaultToolResultTools_includesDefaultGroups(t *testing.T) {
	cfg := DefaultConfig("")
	want := map[string]bool{
		"bash_run": true, "terminal_command": true, "read_file": true, "grep_file": true, "grep_files": true,
		"search_replace": true, "glob_files": true,
	}
	for _, name := range cfg.Tools {
		if !want[name] {
			t.Fatalf("unexpected default tool %q", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing defaults: %v", want)
	}
}
