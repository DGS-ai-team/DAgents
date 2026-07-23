package logfiles

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDatedName(t *testing.T) {
	day := time.Date(2026, 7, 23, 15, 0, 0, 0, time.UTC)
	if got := DatedName("node", false, day); got != "node-2026-07-23.log" {
		t.Fatalf("full = %q", got)
	}
	if got := DatedName("node", true, day); got != "node-2026-07-23.err.log" {
		t.Fatalf("err = %q", got)
	}
	if got := DatedName("shell", false, day); got != "shell-2026-07-23.log" {
		t.Fatalf("shell full = %q", got)
	}
}

func TestJoinDated(t *testing.T) {
	day := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	got := JoinDated(filepath.FromSlash("/tmp/logs"), "browser", true, day)
	want := filepath.Join(filepath.FromSlash("/tmp/logs"), "browser-2026-07-23.err.log")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
