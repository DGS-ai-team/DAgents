package workspacecoord

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCanonicalAndConflictBoundaries(t *testing.T) {
	root := t.TempDir()
	if _, err := Canonical(root + string(filepath.Separator) + ".." + string(filepath.Separator) + "escape"); err == nil {
		t.Fatal("parent traversal accepted")
	}
	if got, err := Canonical(filepath.Join(root, "missing", "child")); err != nil || !strings.HasSuffix(filepath.ToSlash(got), "/missing/child") {
		t.Fatalf("canonical nonexistent=%q err=%v", got, err)
	}
	c := New()
	a, err := c.TryAcquire(context.Background(), root, "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.TryAcquire(context.Background(), root+"-other", "b"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.TryAcquire(ctx, filepath.Join(root, "child"), "c"); err == nil {
		t.Fatal("expected cancelled conflicting acquire")
	}
	a.Release()
	a.Release()
}

func TestCancelledAcquireNeverGrantedWhenIdle(t *testing.T) {
	c := New()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.TryAcquire(ctx, t.TempDir(), "cancelled"); err == nil {
		t.Fatal("cancelled idle acquire granted")
	}
}

func TestCoordinatorConcurrentOnlyOneOverlappingWriter(t *testing.T) {
	root := t.TempDir()
	c := New()
	start := make(chan struct{})
	results := make(chan *Lease, 2)
	for _, owner := range []string{"a", "b"} {
		go func(owner string) { <-start; l, _ := c.TryAcquire(context.Background(), root, owner); results <- l }(owner)
	}
	close(start)
	first := <-results
	select {
	case second := <-results:
		second.Release()
		t.Fatal("two overlapping leases acquired")
	case <-time.After(20 * time.Millisecond):
	}
	first.Release()
	second := <-results
	second.Release()
}

func TestOldLeaseCannotReleaseNewLease(t *testing.T) {
	c := New()
	root := t.TempDir()
	old, err := c.TryAcquire(context.Background(), root, "old")
	if err != nil {
		t.Fatal(err)
	}
	old.Release()
	newLease, err := c.TryAcquire(context.Background(), root, "new")
	if err != nil {
		t.Fatal(err)
	}
	old.Release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cancel()
	if _, err = c.TryAcquire(ctx, root, "blocked"); err == nil {
		t.Fatal("old release removed new lease")
	}
	newLease.Release()
}

func TestCanonicalSymlinkAndWindowsCaseRule(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	a, err := Canonical(target)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonical(link)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("symlink canonical mismatch %q %q", a, b)
	}
	if runtime.GOOS == "windows" && !conflict(`C:\Repo`, `c:\repo\child`) {
		t.Fatal("windows case-fold conflict missing")
	}
}
