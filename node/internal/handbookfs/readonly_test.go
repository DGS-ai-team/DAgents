package handbookfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenReadOnlyDoesNotCreateMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	if _, err := OpenReadOnly(root); err == nil {
		t.Fatal("missing root was accepted")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("read-only open created root: %v", err)
	}
}

func TestOpenReadOnlyDoesNotRecoverPending(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.pendingPath(), []byte(`{"revision":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ro.History(context.Background(), "guide.md"); err == nil {
		t.Fatal("pending history was accepted")
	}
	if _, err := os.Stat(s.pendingPath()); err != nil {
		t.Fatalf("read-only open removed pending: %v", err)
	}
}

func TestOpenReadOnlySharesCanonicalRootLock(t *testing.T) {
	root := t.TempDir()
	created, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenReadOnly(filepath.Join(root, "."))
	if err != nil {
		t.Fatal(err)
	}
	if created.mu != opened.mu {
		t.Fatal("read-only service did not share root lock")
	}
}

func TestOpenReadOnlyReadsHistory(t *testing.T) {
	root := t.TempDir()
	created, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := created.Write(context.Background(), filepath.Join(root, "guide.md"), "", []byte("guide")); err != nil {
		t.Fatal(err)
	}
	opened, err := OpenReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := opened.History(context.Background(), filepath.Join(root, "guide.md"))
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
}
