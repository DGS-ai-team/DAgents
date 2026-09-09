package handbookfs

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func seedPending(t *testing.T, root, path string, before, after []byte, committed bool) {
	t.Helper()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	e := Entry{Revision: 1, Path: filepath.Base(path), BeforeDigest: Digest(before), AfterDigest: Digest(after), BeforeExists: before != nil, AfterExists: true, CreatedAt: time.Now()}
	if committed {
		if err := s.appendEntryLocked(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.writePending(pendingTxn{Revision: 1, Path: path, Before: before, BeforeExists: before != nil, AfterDigest: Digest(after), AfterExists: true}); err != nil {
		t.Fatal(err)
	}
}

func TestPendingRecoveryBreakpoints(t *testing.T) {
	tests := []struct {
		name                       string
		rename, manifest, external bool
		want                       string
		wantErr                    bool
	}{
		{"before rename", false, false, false, "before", false},
		{"after rename", true, false, false, "before", false},
		{"after manifest", true, true, false, "after", false},
		{"external edit", true, false, true, "external", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "x.md")
			before, after := []byte("before"), []byte("after")
			if err := os.WriteFile(path, func() []byte {
				if tc.rename {
					return after
				}
				return before
			}(), 0644); err != nil {
				t.Fatal(err)
			}
			seedPending(t, root, path, before, after, tc.manifest)
			if tc.external {
				if err := os.WriteFile(path, []byte("external"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			s, err := New(root)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected recovery error")
				}
				got, readErr := os.ReadFile(path)
				if readErr != nil || string(got) != "external" {
					t.Fatalf("external content changed: %q err=%v", got, readErr)
				}
				if _, statErr := os.Stat(filepath.Join(root, ".history", "pending.json")); statErr != nil {
					t.Fatalf("pending not retained: %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
			if _, err := os.Stat(s.pendingPath()); !os.IsNotExist(err) {
				t.Fatal("pending remains")
			}
		})
	}
}

func TestConcurrentServicesShareRevisionLock(t *testing.T) {
	root := t.TempDir()
	a, _ := New(root)
	b, _ := New(root)
	path := filepath.Join(root, "x.md")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, s := range []*Service{a, b} {
		wg.Add(1)
		go func(s *Service) {
			defer wg.Done()
			_, err := s.Write(context.Background(), path, "", []byte("x"))
			errs <- err
		}(s)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	h, err := a.History(context.Background(), path)
	if err != nil || len(h) != 2 {
		t.Fatalf("history=%d err=%v", len(h), err)
	}
}

func TestRestoreFirstWriteDeletesFile(t *testing.T) {
	root := t.TempDir()
	s, _ := New(root)
	path := filepath.Join(root, "new.md")
	e, err := s.Write(context.Background(), path, "", []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Restore(context.Background(), path, "", e.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file remains: %v", err)
	}
}

func TestPendingRecoveryAfterRestoreDelete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.md")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	s, _ := New(root)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := s.writePending(pendingTxn{Revision: 1, Path: path, Before: []byte("old"), BeforeExists: true, AfterExists: false}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "old" {
		t.Fatalf("restored=%q err=%v", got, err)
	}
}

func TestServiceRejectsSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	s, _ := New(root)
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := s.Write(context.Background(), filepath.Join(link, "x"), "", []byte("escape")); err == nil {
		t.Fatal("expected symlink escape rejection")
	}
}
