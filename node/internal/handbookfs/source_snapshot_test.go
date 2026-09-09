package handbookfs

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func snapshotSource() Provenance {
	return Provenance{MaintenanceReceiptID: "receipt-a", SessionID: "session-a", TurnID: "turn-a"}
}

func writeSnapshotManifest(t *testing.T, root string, entries []Entry) {
	t.Helper()
	var b strings.Builder
	for i, e := range entries {
		if e.CreatedAt.IsZero() {
			e.CreatedAt = time.Unix(int64(i+1), 0).UTC()
		}
		if !validSnapshotDigest(e.AfterDigest) {
			e.AfterDigest = Digest([]byte(e.AfterDigest))
		}
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(root, ".history", "manifest.jsonl"), []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReadSourceSnapshotMatchesOnlyCompleteProvenance(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	source := snapshotSource()
	other := source
	other.TurnID = "turn-other"
	entries := []Entry{
		{Revision: 1, Path: "guide.md", AfterDigest: "a", AfterExists: true, Provenance: &source},
		{Revision: 2, Path: "other.md", AfterDigest: "b", AfterExists: true, Provenance: &other},
		{Revision: 3, Path: "manual.md", AfterDigest: "c", AfterExists: true},
		{Revision: 4, Path: "nested/topic.md", AfterDigest: "d", AfterExists: true, Provenance: &source},
	}
	writeSnapshotManifest(t, root, entries)
	got, digest, err := s.ReadSourceSnapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Revision != 1 || got[1].Revision != 4 {
		t.Fatalf("matching entries=%+v", got)
	}
	wantBytes, _ := json.Marshal(got)
	if digest != Digest(wantBytes) {
		t.Fatalf("digest=%q want %q", digest, Digest(wantBytes))
	}
}

func TestReadSourceSnapshotEmptyIsReadOnly(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	entries, digest, err := s.ReadSourceSnapshot(context.Background(), snapshotSource())
	if err != nil || len(entries) != 0 || digest != Digest([]byte("[]")) {
		t.Fatalf("entries=%v digest=%q err=%v", entries, digest, err)
	}
	if _, err := os.Stat(s.pendingPath()); !os.IsNotExist(err) {
		t.Fatalf("read created or changed pending transaction: %v", err)
	}
}

func TestReadSourceSnapshotRejectsPendingWithoutRecovery(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.pendingPath(), []byte(`{"revision":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, _, err = s.ReadSourceSnapshot(context.Background(), snapshotSource())
	if err == nil || !strings.Contains(err.Error(), "pending") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(s.pendingPath()); err != nil {
		t.Fatalf("read-only snapshot removed pending: %v", err)
	}
}

func TestReadSourceSnapshotRejectsMalformedAndUnsafeManifest(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"malformed", "{"},
		{"unsafe path", `{"revision":1,"path":"../outside","after_digest":"0000000000000000000000000000000000000000000000000000000000000000","created_at":"2026-01-01T00:00:00Z","provenance":{"maintenance_receipt_id":"receipt-a","session_id":"session-a","turn_id":"turn-a"}}`},
		{"duplicate revision", `{"revision":1,"path":"a.md","after_digest":"0000000000000000000000000000000000000000000000000000000000000000","created_at":"2026-01-01T00:00:00Z"}` + "\n" + `{"revision":1,"path":"b.md","after_digest":"0000000000000000000000000000000000000000000000000000000000000000","created_at":"2026-01-01T00:00:00Z"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			s, err := New(root)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(s.manifest(), []byte(tt.line+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			entries, digest, err := s.ReadSourceSnapshot(context.Background(), snapshotSource())
			if err == nil || entries != nil || digest != "" {
				t.Fatalf("entries=%v digest=%q err=%v", entries, digest, err)
			}
		})
	}
}

func TestReadSourceSnapshotRejectsLimits(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		root := t.TempDir()
		s, err := New(root)
		if err != nil {
			t.Fatal(err)
		}
		entry := Entry{Revision: 1, Path: "guide.md", AfterExists: true, Provenance: provenancePtr(snapshotSource())}
		line, _ := json.Marshal(entry)
		padding := make([]byte, maxSourceSnapshotBytes-len(line)-1)
		for i := range padding {
			padding[i] = 'x'
		}
		// The unknown JSON member is still part of the bounded manifest input.
		data := append([]byte(`{"revision":1,"path":"guide.md","after_digest":"0000000000000000000000000000000000000000000000000000000000000000","created_at":"2026-01-01T00:00:00Z","after_exists":true,"provenance":{"maintenance_receipt_id":"receipt-a","session_id":"session-a","turn_id":"turn-a"},"padding":"`), padding...)
		data = append(data, []byte(`"}`)...)
		data = append(data, '\n')
		if err := os.WriteFile(s.manifest(), data, 0600); err != nil {
			t.Fatal(err)
		}
		got, digest, err := s.ReadSourceSnapshot(context.Background(), snapshotSource())
		if err == nil || got != nil || digest != "" {
			t.Fatalf("got=%v digest=%q err=%v", got, digest, err)
		}
	})

	t.Run("entries", func(t *testing.T) {
		root := t.TempDir()
		s, err := New(root)
		if err != nil {
			t.Fatal(err)
		}
		entries := make([]Entry, maxSourceSnapshotEntries+1)
		source := snapshotSource()
		for i := range entries {
			entries[i] = Entry{Revision: int64(i + 1), Path: "guide.md", AfterExists: true, Provenance: &source}
		}
		writeSnapshotManifest(t, root, entries)
		got, digest, err := s.ReadSourceSnapshot(context.Background(), source)
		if err == nil || got != nil || digest != "" {
			t.Fatalf("got=%v digest=%q err=%v", got, digest, err)
		}
	})
}

func TestReadSourceSnapshotRequiresCompleteProvenance(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []Provenance{{}, {MaintenanceReceiptID: "r", SessionID: "s"}} {
		entries, digest, err := s.ReadSourceSnapshot(context.Background(), source)
		if err == nil || !errors.Is(err, errors.New("complete provenance required")) && !strings.Contains(err.Error(), "complete provenance") || entries != nil || digest != "" {
			t.Fatalf("source=%+v entries=%v digest=%q err=%v", source, entries, digest, err)
		}
	}
}
