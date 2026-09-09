package goals

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func reconciliationFixture(t *testing.T) (*Store, string, int64, string, string, string, time.Time) {
	t.Helper()
	s, date, rev, now := seedMaintenanceOccurrence(t)
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "memory-fixture", "fp-fixture", 2, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMaintenanceResultWithEvidence("auto-maint", "memory-fixture", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"evidence"}]}`), 2, 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "memory-fixture", 1, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareHandbook("memory-fixture", "auto-maint", 3, now); err != nil {
		t.Fatal(err)
	}
	child := "handbook:memory-fixture"
	if _, err := s.MarkHandbookRunning("auto-maint", child, "session-fixture"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindHandbookTurn("auto-maint", child, "session-fixture", "turn-fixture", now); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if s.recoveryOccurrences == nil {
		s.recoveryOccurrences = make(map[string]bool)
	}
	s.recoveryOccurrences[maintenanceOccurrenceKey("auto-maint", date, rev)] = true
	s.mu.Unlock()
	snap, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
	if err != nil {
		t.Fatal(err)
	}
	return s, date, rev, snap.Token, child, "session-fixture", now
}

func TestReconcileHandbookCompletionCASAndIdempotency(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "memory-reconcile", "fp-reconcile", 2, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMaintenanceResultWithEvidence("auto-maint", "memory-reconcile", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"evidence"}]}`), 2, 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "memory-reconcile", 1, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PrepareHandbook("memory-reconcile", "auto-maint", 3, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:memory-reconcile"
	if _, err := s.MarkHandbookRunning("auto-maint", childID, "session-reconcile"); err != nil {
		t.Fatal(err)
	}
	if err := s.BindHandbookTurn("auto-maint", childID, "session-reconcile", "turn-reconcile", now); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	if s.recoveryOccurrences == nil {
		s.recoveryOccurrences = make(map[string]bool)
	}
	s.recoveryOccurrences[maintenanceOccurrenceKey("auto-maint", date, rev)] = true
	s.mu.Unlock()
	snapshot, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
	if err != nil {
		t.Fatal(err)
	}
	result := json.RawMessage(`{"files":["guide.md"]}`)
	got, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, snapshot.Token, childID, "session-reconcile", "turn-reconcile", 2, result, now.Add(time.Second))
	if err != nil || got.PhaseState != MaintenancePhaseComplete || got.UsedTokens != 2 || got.ReconciledAt.IsZero() {
		t.Fatalf("reconcile=%+v err=%v", got, err)
	}
	usage, _ := s.GetUsage("auto-maint")
	if usage.MaintenanceTokens != 3 {
		t.Fatalf("usage=%+v", usage)
	}
	afterCompletion, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResumeMaintenanceOccurrence("auto-maint", date, rev, afterCompletion.Token, now.Add(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	// The original request token is retained for a retry of the exact request.
	retry, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, snapshot.Token, childID, "session-reconcile", "turn-reconcile", 2, result, now.Add(2*time.Second))
	if err != nil || retry.PhaseState != MaintenancePhaseComplete {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}
	usage, _ = s.GetUsage("auto-maint")
	if usage.MaintenanceTokens != 3 {
		t.Fatalf("idempotent retry charged usage=%+v", usage)
	}
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReconcileHandbookCompletion("auto-maint", date, rev, snapshot.Token, childID, "session-reconcile", "turn-reconcile", 2, result, now.Add(3*time.Second)); err != nil {
		t.Fatalf("reopened idempotent retry: %v", err)
	}
	usage, _ = reopened.GetUsage("auto-maint")
	if usage.MaintenanceTokens != 3 {
		t.Fatalf("reopened retry charged usage=%+v", usage)
	}
	if _, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, snapshot.Token, childID, "session-reconcile", "turn-reconcile", 3, result, now.Add(2*time.Second)); err == nil {
		t.Fatal("different completion accepted")
	}
}

func TestReconcileHandbookCompletionRejectsUnknownStaleAndSaveFailure(t *testing.T) {
	for _, mode := range []string{"unknown", "stale", "save-failure"} {
		t.Run(mode, func(t *testing.T) {
			s, date, rev, token, child, session, now := reconciliationFixture(t)
			if mode == "unknown" {
				s.mu.Lock()
				r := s.data.MaintenanceReceipts[child]
				r.Unknown = true
				s.data.MaintenanceReceipts[child] = r
				if err := s.saveLocked(); err != nil {
					s.mu.Unlock()
					t.Fatal(err)
				}
				s.mu.Unlock()
				fresh, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, fresh.Token, child, session, "turn-fixture", 2, json.RawMessage(`{"ok":true}`), now); err == nil {
					t.Fatal("unknown child accepted")
				}
				return
			}
			if mode == "stale" {
				s.mu.Lock()
				o := s.data.MaintenanceOccurrences[maintenanceOccurrenceKey("auto-maint", date, rev)]
				o.UpdatedAt = now.Add(time.Second)
				s.data.MaintenanceOccurrences[maintenanceOccurrenceKey("auto-maint", date, rev)] = o
				if err := s.saveLocked(); err != nil {
					s.mu.Unlock()
					t.Fatal(err)
				}
				s.mu.Unlock()
				if _, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, token, child, session, "turn-fixture", 2, json.RawMessage(`{"ok":true}`), now); err == nil {
					t.Fatal("stale token accepted")
				}
				return
			}
			before, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
			if err != nil {
				t.Fatal(err)
			}
			block := filepath.Join(t.TempDir(), "block")
			if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
				t.Fatal(err)
			}
			s.path = filepath.Join(block, "goals.json")
			if _, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, token, child, session, "turn-fixture", 2, json.RawMessage(`{"ok":true}`), now); err == nil {
				t.Fatal("save failure accepted")
			}
			got, _ := s.GetMaintenanceReceipt(child)
			if got.PhaseState != MaintenancePhaseRunning || got.Status != "pending" {
				t.Fatalf("rollback=%+v", got)
			}
			after, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
			if err != nil {
				t.Fatal(err)
			}
			if before.Token != after.Token {
				t.Fatalf("save failure changed snapshot token: before=%s after=%s", before.Token, after.Token)
			}
		})
	}
}

func TestReconcileHandbookCompletionConcurrentChargesOnce(t *testing.T) {
	s, date, rev, token, child, session, now := reconciliationFixture(t)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, token, child, session, "turn-fixture", 2, json.RawMessage(`{"ok":true}`), now)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	usage, _ := s.GetUsage("auto-maint")
	if usage.MaintenanceTokens != 3 {
		t.Fatalf("usage charged twice=%+v", usage)
	}
}

func TestReconcileHandbookCompletionReopenCanonicalEvidenceBound(t *testing.T) {
	s, date, rev, token, child, session, now := reconciliationFixture(t)
	result := []byte(`{"x":[` + strings.Repeat("0,", 19999) + `0]}`)
	if _, err := s.ReconcileHandbookCompletion("auto-maint", date, rev, token, child, session, "turn-fixture", 2, result, now); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ReconcileHandbookCompletion("auto-maint", date, rev, token, child, session, "turn-fixture", 2, result, now.Add(time.Second)); err != nil {
		t.Fatalf("canonical evidence retry: %v", err)
	}
}
