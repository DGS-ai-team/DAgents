package goals

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
)

func TestMaintenanceRecoverySnapshotAndResumeCAS(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "scheduled-recovery", "fp-recovery", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMaintenanceResultWithEvidence("auto-maint", "scheduled-recovery", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"recover"}]}`), 5, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "scheduled-recovery", 0, false, now); err != nil {
		t.Fatal(err)
	}
	// Reopening a store converts a pending occurrence into startup recovery.
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
	if err != nil || snapshot.Occurrence.Status != MaintenanceOccurrenceRecoveryRequired || snapshot.Token == "" || len(snapshot.Receipts) != 1 || snapshot.Receipts[0].ReceiptID != "scheduled-recovery" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if _, err := reopened.ResumeMaintenanceOccurrence("auto-maint", date, rev, "stale-token", now); err == nil {
		t.Fatal("stale recovery token accepted")
	}
	resumed, err := reopened.ResumeMaintenanceOccurrence("auto-maint", date, rev, snapshot.Token, now)
	if err != nil || resumed.Status != MaintenanceOccurrencePending || resumed.FinishedAt != nil || resumed.RecoveryCount != 1 || resumed.LastRecoveryAt == nil {
		t.Fatalf("resumed=%+v err=%v", resumed, err)
	}
	if _, err := reopened.ResumeMaintenanceOccurrence("auto-maint", date, rev, snapshot.Token, now); err == nil {
		t.Fatal("recovery occurrence resumed twice")
	}
	got, _ := reopened.GetMaintenanceReceipt("scheduled-recovery")
	if got.OccurrenceLocalDate != date || got.OccurrenceScheduleRevision != rev {
		t.Fatalf("receipt lost occurrence identity=%+v", got)
	}
}

func TestMaintenanceResumeConcurrentAndSaveFailure(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "resume-concurrent", "fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMaintenanceResultWithEvidence("auto-maint", "resume-concurrent", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"x"}]}`), 1, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "resume-concurrent", 0, false, now); err != nil {
		t.Fatal(err)
	}
	key := maintenanceOccurrenceKey("auto-maint", date, rev)
	s.mu.Lock()
	if s.recoveryOccurrences == nil {
		s.recoveryOccurrences = map[string]bool{}
	}
	s.recoveryOccurrences[key] = true
	s.mu.Unlock()
	snapshot, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.ResumeMaintenanceOccurrence("auto-maint", date, rev, snapshot.Token, now)
			results <- e
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("concurrent resume successes=%d", success)
	}

	// A fresh startup recovery must roll back entirely when the goals save fails.
	s2, date2, rev2, now2 := seedMaintenanceOccurrence(t)
	if _, err := s2.BeginMaintenanceForOccurrence("auto-maint", date2, rev2, "resume-fail", "fp2", 1, now2); err != nil {
		t.Fatal(err)
	}
	if err := s2.SaveMaintenanceResultWithEvidence("auto-maint", "resume-fail", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"y"}]}`), 1, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.SettleMaintenance("auto-maint", "resume-fail", 0, false, now2); err != nil {
		t.Fatal(err)
	}
	key2 := maintenanceOccurrenceKey("auto-maint", date2, rev2)
	s2.mu.Lock()
	if s2.recoveryOccurrences == nil {
		s2.recoveryOccurrences = map[string]bool{}
	}
	s2.recoveryOccurrences[key2] = true
	s2.mu.Unlock()
	snap2, err := s2.GetMaintenanceRecoverySnapshot("auto-maint", date2, rev2)
	if err != nil {
		t.Fatal(err)
	}
	path, backup := s2.path, s2.path+".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.ResumeMaintenanceOccurrence("auto-maint", date2, rev2, snap2.Token, now2); err == nil {
		t.Fatal("save failure accepted")
	}
	occ, _ := s2.GetMaintenanceRecoverySnapshot("auto-maint", date2, rev2)
	if occ.Occurrence.Status != MaintenanceOccurrenceRecoveryRequired || occ.Occurrence.RecoveryCount != 0 {
		t.Fatalf("rollback snapshot=%+v", occ)
	}
	_ = os.Remove(path)
	_ = os.Rename(backup, path)
}

func TestMaintenanceResumeRejectsTerminalFlagAndMalformedChild(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	key := maintenanceOccurrenceKey("auto-maint", date, rev)
	s.mu.Lock()
	o := s.data.MaintenanceOccurrences[key]
	o.Status = MaintenanceOccurrenceCompleted
	s.data.MaintenanceOccurrences[key] = o
	if s.recoveryOccurrences == nil {
		s.recoveryOccurrences = map[string]bool{}
	}
	s.recoveryOccurrences[key] = true
	_ = s.saveLocked()
	s.mu.Unlock()
	snap, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResumeMaintenanceOccurrence("auto-maint", date, rev, snap.Token, now); err == nil {
		t.Fatal("terminal occurrence resumed")
	}
}

func TestMaintenanceResumeRejectsMalformedChildStates(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Store, string, string)
	}{
		{"settled-prepared", func(s *Store, parentID, childID string) {
			r := s.data.MaintenanceReceipts[childID]
			r.Status = "settled"
			s.data.MaintenanceReceipts[childID] = r
		}},
		{"unknown", func(s *Store, parentID, childID string) {
			r := s.data.MaintenanceReceipts[childID]
			r.Unknown = true
			s.data.MaintenanceReceipts[childID] = r
		}},
		{"running", func(s *Store, parentID, childID string) {
			r := s.data.MaintenanceReceipts[childID]
			r.PhaseState = MaintenancePhaseRunning
			s.data.MaintenanceReceipts[childID] = r
		}},
		{"wrong-parent-link", func(s *Store, parentID, childID string) {
			r := s.data.MaintenanceReceipts[parentID]
			r.HandbookReceiptID = "handbook:wrong"
			s.data.MaintenanceReceipts[parentID] = r
		}},
		{"orphan", func(s *Store, parentID, childID string) {
			r := s.data.MaintenanceReceipts[childID]
			r.ParentReceiptID = "missing-parent"
			s.data.MaintenanceReceipts[childID] = r
		}},
		{"child-chain", func(s *Store, parentID, childID string) {
			r := s.data.MaintenanceReceipts[childID]
			r.ParentReceiptID = "handbook:another-child"
			s.data.MaintenanceReceipts[childID] = r
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, date, rev, now := seedMaintenanceOccurrence(t)
			parentID := "recovery-parent-" + tc.name
			if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, parentID, "fp-"+tc.name, 1, now); err != nil {
				t.Fatal(err)
			}
			if err := s.SaveMaintenanceResultWithEvidence("auto-maint", parentID, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"x"}]}`), 1, 0, false); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SettleMaintenance("auto-maint", parentID, 0, false, now); err != nil {
				t.Fatal(err)
			}
			childID := "handbook:" + parentID
			if _, err := s.PrepareHandbook(parentID, "auto-maint", 1, now); err != nil {
				t.Fatal(err)
			}
			key := maintenanceOccurrenceKey("auto-maint", date, rev)
			s.mu.Lock()
			if s.recoveryOccurrences == nil {
				s.recoveryOccurrences = map[string]bool{}
			}
			s.recoveryOccurrences[key] = true
			tc.mutate(s, parentID, childID)
			if err := s.saveLocked(); err != nil {
				s.mu.Unlock()
				t.Fatal(err)
			}
			s.mu.Unlock()
			snapshot, err := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.ResumeMaintenanceOccurrence("auto-maint", date, rev, snapshot.Token, now); err == nil {
				t.Fatal("malformed child accepted")
			}
			occ, _ := s.GetMaintenanceRecoverySnapshot("auto-maint", date, rev)
			if occ.Occurrence.Status != MaintenanceOccurrenceRecoveryRequired || occ.Occurrence.RecoveryCount != 0 {
				t.Fatalf("state changed=%+v", occ.Occurrence)
			}
		})
	}
}
