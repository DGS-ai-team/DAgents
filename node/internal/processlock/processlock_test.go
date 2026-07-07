package processlock

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestAcquireNode_serialSameConfig(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "config.yaml")
	release, err := AcquireNode(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	_, err = AcquireNode(cfg)
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second acquire = %v, want ErrAlreadyRunning", err)
	}
}

func TestAcquireNode_differentConfig(t *testing.T) {
	dir := t.TempDir()
	cfg1 := filepath.Join(dir, "a.yaml")
	cfg2 := filepath.Join(dir, "b.yaml")

	r1, err := AcquireNode(cfg1)
	if err != nil {
		t.Fatal(err)
	}
	defer r1()

	r2, err := AcquireNode(cfg2)
	if err != nil {
		t.Fatal(err)
	}
	defer r2()
}
