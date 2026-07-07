//go:build windows

package singleinstance

import (
	"path/filepath"
	"testing"
)

func TestAcquireShell_excludesSecondInstance(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	release, err := AcquireShell(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = AcquireShell(cfg)
	if err != ErrAlreadyRunning {
		t.Fatalf("second acquire = %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquireShell_differentConfigAllowed(t *testing.T) {
	dir := t.TempDir()
	cfg1 := filepath.Join(dir, "a", "config.yaml")
	cfg2 := filepath.Join(dir, "b", "config.yaml")

	r1, err := AcquireShell(cfg1)
	if err != nil {
		t.Fatal(err)
	}
	defer r1()

	r2, err := AcquireShell(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	defer r2()
}
