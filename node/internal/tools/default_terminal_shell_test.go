package tools

import (
	"runtime"
	"testing"
)

func TestLocalTerminalConfigUsesPlatformDefaultShell(t *testing.T) {
	want := "bash"
	if runtime.GOOS == "windows" {
		want = "powershell"
	}
	if got := localTerminalConfig().Shell; got != want {
		t.Fatalf("local terminal shell=%q, want %q", got, want)
	}
}
