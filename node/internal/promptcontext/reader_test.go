package promptcontext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
)

func TestReaderSidecarAndCustom(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	r := promptcontext.NewReader(root)
	dir := filepath.Join(root, "prompt_context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "soul.md"), []byte("  agent soul  "), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "custom.md"), []byte("do X"), 0o644); err != nil {
		t.Fatal(err)
	}

	stable := r.BuildStableContextSections()
	if !strings.Contains(stable, "以下是你的设定") || !strings.Contains(stable, "agent soul") {
		t.Fatalf("stable = %q", stable)
	}
	custom := r.BuildCustomSection()
	if !strings.Contains(custom, "临时/专项指令") || !strings.Contains(custom, "do X") {
		t.Fatalf("custom = %q", custom)
	}
}

func TestEnsureSidecarCreatesEmptyFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	r := promptcontext.NewReader(root)
	r.EnsureSidecarFiles()
	for _, name := range []string{"soul.md", "user.md", "custom.md"} {
		path := filepath.Join(root, "prompt_context", name)
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestReaderFilterDisablesLongTerm(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	if err := os.MkdirAll(filepath.Join(root, "memory"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "memory", "long_term.md"), []byte("remember me"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := promptcontext.NewReader(root)
	off := false
	r.SetFilter(promptcontext.Filter{LongTermEnabled: &off})
	if got := r.ReadLongTermMemory(); got != "" {
		t.Fatalf("expected empty when disabled, got %q", got)
	}
	on := true
	r.SetFilter(promptcontext.Filter{LongTermEnabled: &on})
	if got := r.ReadLongTermMemory(); got != "remember me" {
		t.Fatalf("got %q", got)
	}
}
