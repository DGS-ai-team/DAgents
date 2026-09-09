package handbookfs

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWritePersistsProvenanceAndOrdinaryWriteIsUnattributed(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "guide.md")
	ctx := WithProvenance(context.Background(), Provenance{MaintenanceReceiptID: "receipt-1", SessionID: "session-1", TurnID: "turn-1"})
	e, err := s.Write(ctx, path, "", []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Provenance == nil || e.Provenance.MaintenanceReceiptID != "receipt-1" || e.Provenance.SessionID != "session-1" || e.Provenance.TurnID != "turn-1" {
		t.Fatalf("provenance=%+v", e.Provenance)
	}
	history, err := s.History(context.Background(), path)
	if err != nil || len(history) != 1 || history[0].Provenance == nil || history[0].Provenance.TurnID != "turn-1" {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if _, err := s.Write(context.Background(), path, Digest([]byte("first")), []byte("second")); err != nil {
		t.Fatal(err)
	}
	history, err = s.History(context.Background(), path)
	if err != nil || len(history) != 2 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if history[1].Provenance != nil {
		t.Fatalf("ordinary write reused provenance=%+v", history[1].Provenance)
	}
}

func TestPendingRecoveryPreservesProvenance(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "guide.md")
	if err := os.WriteFile(path, []byte("after"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	prov := provenancePtr(Provenance{MaintenanceReceiptID: "receipt-2", SessionID: "session-2", TurnID: "turn-2"})
	e := Entry{Revision: 1, Path: "guide.md", BeforeDigest: Digest([]byte("before")), AfterDigest: Digest([]byte("after")), BeforeExists: true, AfterExists: true, CreatedAt: time.Now().UTC(), Provenance: cloneProvenance(prov)}
	if err := s.appendEntryLocked(e); err != nil {
		t.Fatal(err)
	}
	if err := s.writePending(pendingTxn{Revision: 1, Path: path, Before: []byte("before"), BeforeExists: true, AfterDigest: Digest([]byte("after")), AfterExists: true, Provenance: cloneProvenance(prov)}); err != nil {
		t.Fatal(err)
	}
	reopened, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	history, err := reopened.History(context.Background(), path)
	if err != nil || len(history) != 1 || history[0].Provenance == nil || history[0].Provenance.MaintenanceReceiptID != "receipt-2" {
		t.Fatalf("recovered history=%+v err=%v", history, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".history", "pending.json")); !os.IsNotExist(err) {
		t.Fatalf("pending remains: %v", err)
	}
}

func TestPendingRecoveryRejectsProvenanceMismatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "guide.md")
	if err := os.WriteFile(path, []byte("after"), 0644); err != nil {
		t.Fatal(err)
	}
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.appendEntryLocked(Entry{Revision: 1, Path: "guide.md", AfterDigest: Digest([]byte("after")), AfterExists: true, Provenance: provenancePtr(Provenance{MaintenanceReceiptID: "owner-a"})}); err != nil {
		t.Fatal(err)
	}
	if err := s.writePending(pendingTxn{Revision: 1, Path: path, Before: []byte("before"), BeforeExists: true, AfterDigest: Digest([]byte("after")), AfterExists: true, Provenance: provenancePtr(Provenance{MaintenanceReceiptID: "owner-b"})}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root); err == nil {
		t.Fatal("provenance mismatch was accepted")
	}
	if _, err := os.Stat(filepath.Join(root, ".history", "pending.json")); err != nil {
		t.Fatalf("mismatched pending was removed: %v", err)
	}
}

func TestRestoreUsesCurrentProvenanceAndDoesNotInheritHistory(t *testing.T) {
	root := t.TempDir()
	s, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "guide.md")
	original := WithProvenance(context.Background(), Provenance{MaintenanceReceiptID: "original", SessionID: "session-old", TurnID: "turn-old"})
	if _, err := s.Write(original, path, "", []byte("old")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write(context.Background(), path, Digest([]byte("old")), []byte("new")); err != nil {
		t.Fatal(err)
	}
	restoreCtx := WithProvenance(context.Background(), Provenance{MaintenanceReceiptID: "restore", SessionID: "session-new", TurnID: "turn-new"})
	restored, err := s.Restore(restoreCtx, path, Digest([]byte("new")), 2)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Provenance == nil || restored.Provenance.MaintenanceReceiptID != "restore" {
		t.Fatalf("restore provenance=%+v", restored.Provenance)
	}
	if restored.Provenance.MaintenanceReceiptID == "original" {
		t.Fatal("restore inherited selected revision provenance")
	}
	history, err := s.History(context.Background(), path)
	if err != nil || len(history) != 3 {
		t.Fatalf("history=%+v err=%v", history, err)
	}
	if history[2].Provenance == nil || history[2].Provenance.TurnID != "turn-new" {
		t.Fatalf("restore history provenance=%+v", history[2].Provenance)
	}
	ordinary, err := s.Restore(context.Background(), path, Digest([]byte("old")), 1)
	if err != nil {
		t.Fatal(err)
	}
	if ordinary.Provenance != nil {
		t.Fatalf("ordinary restore inherited provenance=%+v", ordinary.Provenance)
	}
}
