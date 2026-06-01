package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func TestSplitBashStatements(t *testing.T) {
	parts := policy.SplitBashStatements(`echo a && echo b; echo c`)
	if len(parts) != 3 {
		t.Fatalf("parts = %v", parts)
	}
}

func TestBlockedSudoWithoutNonInteractive(t *testing.T) {
	snap := hostsnapshot.Get()
	if snap.EffectiveUID == nil || *snap.EffectiveUID == 0 || snap.OSKind == "windows" {
		t.Skip("requires non-root POSIX uid")
	}
	msg := blockedNonRootPasswordPromptingShell("sudo apt update", shellBash)
	if msg == "" || !strings.Contains(msg, "sudo") {
		t.Fatalf("msg = %q", msg)
	}
}

func TestBlockedSudoWithNonInteractiveAllowed(t *testing.T) {
	snap := hostsnapshot.Get()
	if snap.EffectiveUID == nil || *snap.EffectiveUID == 0 || snap.OSKind == "windows" {
		t.Skip("requires non-root POSIX uid")
	}
	msg := blockedNonRootPasswordPromptingShell("sudo -n apt update", shellBash)
	if msg != "" {
		t.Fatalf("msg = %q", msg)
	}
}

func TestResolveRunCWDDefaultAndSubdir(t *testing.T) {
	root := t.TempDir()
	sub := root + "/sub"
	if err := osMkdir(sub); err != nil {
		t.Fatal(err)
	}
	reg, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := reg.resolveRunCWD("")
	if err != nil || cwd != reg.fsRoot {
		t.Fatalf("default cwd = %q err=%v", cwd, err)
	}
	cwd2, err := reg.resolveRunCWD("sub")
	if err != nil || cwd2 != sub {
		t.Fatalf("sub cwd = %q err=%v", cwd2, err)
	}
}

func TestResolveRunCWDRejectsEscape(t *testing.T) {
	root := t.TempDir()
	reg, err := NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.resolveRunCWD("../outside"); err == nil {
		t.Fatal("expected escape error")
	}
}

func osMkdir(path string) error {
	return os.MkdirAll(path, 0o755)
}
