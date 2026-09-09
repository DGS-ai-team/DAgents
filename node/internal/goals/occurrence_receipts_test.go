package goals

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

func seedMaintenanceOccurrence(t *testing.T) (*Store, string, int64, time.Time) {
	t.Helper()
	s, now := maintenanceStore(t)
	p, _ := s.GetProfile("auto-maint")
	p.MaintenanceEnabled, p.MaintenanceSchedule, p.Timezone = true, "daily 00:00", "UTC"
	if _, err := s.SaveProfile(p, p.Revision, now); err != nil {
		t.Fatal(err)
	}
	p, _ = s.GetProfile("auto-maint")
	date, rev := "2026-09-09", p.MaintenanceRevision
	s.mu.Lock()
	s.data.MaintenanceOccurrences[maintenanceOccurrenceKey("auto-maint", date, rev)] = MaintenanceOccurrence{AgentID: "auto-maint", LocalDate: date, ScheduleRevision: rev, Status: MaintenanceOccurrencePending, ScheduledAt: now, ClaimedAt: now, UpdatedAt: now}
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	return s, date, rev, now
}

func TestBeginMaintenanceForOccurrenceIsAtomicAndIdempotent(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	results := make(chan MaintenanceReservation, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reservation, err := s.BeginMaintenanceForOccurrence(" auto-maint ", date, rev, "scheduled-receipt", "source-fp", 2, now)
			results <- reservation
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	claimed := 0
	for reservation := range results {
		if reservation.Claimed {
			claimed++
		}
		if reservation.Receipt.OccurrenceLocalDate != date || reservation.Receipt.OccurrenceScheduleRevision != rev {
			t.Fatalf("reservation=%+v", reservation)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if claimed != 1 {
		t.Fatalf("claimed=%d", claimed)
	}
	if err := s.SaveMaintenanceResultWithEvidence("auto-maint", "scheduled-receipt", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"scheduled"}]}`), 4, 0, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "scheduled-receipt", 0, false, now); err != nil {
		t.Fatal(err)
	}
	child, err := s.PrepareHandbook("scheduled-receipt", "auto-maint", 1, now)
	if err != nil || !child.Claimed || child.Receipt.OccurrenceLocalDate != date || child.Receipt.OccurrenceScheduleRevision != rev {
		t.Fatalf("child=%+v err=%v", child, err)
	}
	childID := "handbook:scheduled-receipt"
	if claimed, err := s.MarkHandbookRunning("auto-maint", childID, "scheduled-session"); err != nil || !claimed.Claimed {
		t.Fatalf("mark child=%+v err=%v", claimed, err)
	}
	if _, err := s.SettleHandbook("auto-maint", childID, 1, false, json.RawMessage(`{"written":true}`), MaintenancePhaseComplete, now); err != nil {
		t.Fatal(err)
	}
	entries := s.ListMaintenanceOccurrenceReceipts("auto-maint", date, rev)
	if len(entries) != 2 || entries[0].ReceiptID != "handbook:scheduled-receipt" || entries[1].ReceiptID != "scheduled-receipt" {
		t.Fatalf("entries=%+v", entries)
	}
	var parentEntry, childEntry *MaintenanceReceiptEntry
	for i := range entries {
		if entries[i].ReceiptID == "scheduled-receipt" {
			parentEntry = &entries[i]
		}
		if entries[i].ReceiptID == "handbook:scheduled-receipt" {
			childEntry = &entries[i]
		}
	}
	if parentEntry == nil || childEntry == nil || len(parentEntry.Receipt.CandidateJSON) == 0 || len(parentEntry.Receipt.EvidenceJSON) == 0 || len(childEntry.Receipt.ResultJSON) == 0 {
		t.Fatalf("missing deep-copy fields entries=%+v", entries)
	}
	parentCandidate, parentEvidence, childResult := append([]byte(nil), parentEntry.Receipt.CandidateJSON...), append([]byte(nil), parentEntry.Receipt.EvidenceJSON...), append([]byte(nil), childEntry.Receipt.ResultJSON...)
	parentEntry.Receipt.CandidateJSON[0] = 'x'
	parentEntry.Receipt.EvidenceJSON[0] = 'x'
	childEntry.Receipt.ResultJSON[0] = 'x'
	storedParent, _ := s.GetMaintenanceReceipt("scheduled-receipt")
	storedChild, _ := s.GetMaintenanceReceipt("handbook:scheduled-receipt")
	if string(storedParent.CandidateJSON) != string(parentCandidate) || string(storedParent.EvidenceJSON) != string(parentEvidence) || string(storedChild.ResultJSON) != string(childResult) {
		t.Fatal("listed receipt exposed mutable storage")
	}
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	entries = reopened.ListMaintenanceOccurrenceReceipts("auto-maint", date, rev)
	if len(entries) != 2 || entries[0].ReceiptID != "handbook:scheduled-receipt" || entries[1].ReceiptID != "scheduled-receipt" {
		t.Fatalf("reopened entries=%+v", entries)
	}
	if _, err := reopened.BeginMaintenanceForOccurrence("auto-maint", "2026-09-10", rev, "scheduled-receipt", "source-fp", 2, now); err == nil {
		t.Fatal("receipt was rebound across dates")
	}
}

func TestBeginMaintenanceForOccurrenceRejectsRecoveryConfigAndManualConflicts(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	if _, err := s.BeginMaintenance("auto-maint", "manual-receipt", "manual-fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "manual-receipt", "manual-fp", 1, now); err == nil {
		t.Fatal("manual receipt was rebound")
	}
	profile, _ := s.GetProfile("auto-maint")
	profile.MaintenanceEnabled = false
	if _, err := s.SaveProfile(profile, profile.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "scheduled", "fp", 1, now); err == nil {
		t.Fatal("disabled maintenance occurrence was claimable")
	}
	if _, err := s.BeginMaintenanceForOccurrence("other", date, rev, "other", "fp", 1, now); err == nil {
		t.Fatal("cross-agent occurrence was claimable")
	}
}

func TestBeginMaintenanceForOccurrenceRejectsRestartRecovery(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	key := maintenanceOccurrenceKey("auto-maint", date, rev)
	s.mu.Lock()
	if s.recoveryOccurrences == nil {
		s.recoveryOccurrences = map[string]bool{}
	}
	s.recoveryOccurrences[key] = true
	s.mu.Unlock()
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "scheduled", "fp", 1, now); err == nil {
		t.Fatal("recovery occurrence was claimable")
	}
}

func TestBeginMaintenanceForOccurrenceSaveFailureRollsBack(t *testing.T) {
	s, date, rev, now := seedMaintenanceOccurrence(t)
	path, backup := s.path, s.path+".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path); _ = os.Rename(backup, path) }()
	if _, err := s.BeginMaintenanceForOccurrence("auto-maint", date, rev, "scheduled-fail", "fp", 1, now); err == nil {
		t.Fatal("save failure accepted")
	}
	if _, ok := s.GetMaintenanceReceipt("scheduled-fail"); ok {
		t.Fatal("failed occurrence receipt remained")
	}
}
