package tools

import (
	"runtime"
	"strings"
	"testing"
)

func TestWrapShellCommandForPipe(t *testing.T) {
	in := "Write-Output 中文"
	got := wrapShellCommandForPipe(shellPowerShell, in)
	if runtime.GOOS == "windows" {
		if !strings.HasPrefix(got, powerShellPipeEncodingPrefix) {
			t.Fatalf("got %q", got)
		}
		if !strings.HasSuffix(got, in) {
			t.Fatalf("suffix missing: %q", got)
		}
		return
	}
	if got != in {
		t.Fatalf("non-windows should pass through: %q", got)
	}
}

func TestWrapShellCommandForPipe_otherShells(t *testing.T) {
	in := "echo ok"
	for _, st := range []shellType{shellBash, shellCmd} {
		if got := wrapShellCommandForPipe(st, in); got != in {
			t.Fatalf("%s: got %q", st, got)
		}
	}
}
