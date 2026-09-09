package goals

import (
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

func prepareHandbookParent(t *testing.T) (*Store, string, time.Time) {
	t.Helper()
	s, now := maintenanceStore(t)
	profile, ok := s.GetProfile("auto-maint")
	if !ok {
		t.Fatal("maintenance profile missing")
	}
	profile.MaintenanceEnabled = true
	profile.MaintenanceSchedule = "daily 00:00"
	profile.Timezone = "UTC"
	if _, err := s.SaveProfile(profile, profile.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenance("auto-maint", "memory-parent", "memory-fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMaintenanceResultWithEvidence("auto-maint", "memory-parent", json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"evidence"}]}`), 3, 1, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "memory-parent", 1, false, now); err != nil {
		t.Fatal(err)
	}
	return s, "memory-parent", now
}

func TestPrepareHandbookStableParentChildAndIdempotent(t *testing.T) {
	s, parentID, now := prepareHandbookParent(t)
	child, err := s.PrepareHandbook(parentID, " auto-maint ", 5, now)
	if err != nil || !child.Claimed || child.Receipt.ParentReceiptID != parentID || child.Receipt.PhaseState != MaintenancePhasePrepared {
		t.Fatalf("prepare=%+v err=%v", child, err)
	}
	parent, _ := s.GetMaintenanceReceipt(parentID)
	if parent.HandbookReceiptID != "handbook:"+parentID || parent.PhaseState != MaintenancePhasePrepared {
		t.Fatalf("parent=%+v", parent)
	}
	entries := s.ListHandbookParents(" auto-maint ", 10)
	if len(entries) != 1 || entries[0].ReceiptID != parentID || entries[0].Receipt.HandbookReceiptID != "handbook:"+parentID {
		t.Fatalf("handbook parents=%+v", entries)
	}
	again, err := s.PrepareHandbook(parentID, "auto-maint", 5, now)
	if err != nil || again.Claimed || again.Receipt.ParentReceiptID != parentID {
		t.Fatalf("repeat=%+v err=%v", again, err)
	}
	s.mu.Lock()
	childReceipt := s.data.MaintenanceReceipts["handbook:"+parentID]
	childReceipt.PhaseState = MaintenancePhaseComplete
	s.data.MaintenanceReceipts["handbook:"+parentID] = childReceipt
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	again, err = s.PrepareHandbook(parentID, "auto-maint", 5, now)
	if err != nil || again.Claimed || again.Receipt.PhaseState != MaintenancePhaseComplete {
		t.Fatalf("completed repeat=%+v err=%v", again, err)
	}
	if _, err := s.PrepareHandbook(parentID, "other-agent", 5, now); err == nil {
		t.Fatal("cross-agent prepare accepted")
	}
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := reopened.GetMaintenanceReceipt("handbook:" + parentID)
	if !ok || got.ParentReceiptID != parentID || got.PhaseState != MaintenancePhaseComplete {
		t.Fatalf("reopened child=%+v ok=%v", got, ok)
	}
}

func TestListHandbookParentsDeepCopiesReceiptPayloads(t *testing.T) {
	s, parentID, _ := prepareHandbookParent(t)
	s.mu.Lock()
	r := s.data.MaintenanceReceipts[parentID]
	r.CandidateJSON = []byte(`[{"information":"candidate"}]`)
	r.EvidenceJSON = []byte(`{"evidence":"source"}`)
	r.ResultJSON = []byte(`{"result":"ok"}`)
	s.data.MaintenanceReceipts[parentID] = r
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	entries := s.ListHandbookParents("auto-maint", 10)
	if len(entries) != 1 {
		t.Fatalf("entries=%d", len(entries))
	}
	entries[0].Receipt.CandidateJSON[0] = 'X'
	entries[0].Receipt.EvidenceJSON[0] = 'X'
	entries[0].Receipt.ResultJSON[0] = 'X'
	got, _ := s.GetMaintenanceReceipt(parentID)
	if string(got.CandidateJSON) != `[{"information":"candidate"}]` || string(got.EvidenceJSON) != `{"evidence":"source"}` || string(got.ResultJSON) != `{"result":"ok"}` {
		t.Fatalf("store payload mutated: %+v", got)
	}
}

func TestPrepareHandbookSaveFailureRollsBackParentAndChild(t *testing.T) {
	s, parentID, _ := prepareHandbookParent(t)
	path := s.path
	backup := path + ".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path); _ = os.Rename(backup, path) }()
	if _, err := s.PrepareHandbook(parentID, "auto-maint", 5, time.Now().UTC()); err == nil {
		t.Fatal("save failure accepted")
	}
	parent, _ := s.GetMaintenanceReceipt(parentID)
	if parent.HandbookReceiptID != "" || parent.PhaseState != "" {
		t.Fatalf("parent not rolled back=%+v", parent)
	}
	if _, ok := s.GetMaintenanceReceipt("handbook:" + parentID); ok {
		t.Fatal("child remained after failed prepare")
	}
}

func TestMarkAndSettleHandbookClaimsOnceAndIsIdempotent(t *testing.T) {
	s, parentID, now := prepareHandbookParent(t)
	if _, err := s.PrepareHandbook(parentID, "auto-maint", 5, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	type claimResult struct {
		reservation MaintenanceReservation
		err         error
	}
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for _, sessionID := range []string{"maintenance-a", "maintenance-b"} {
		wg.Add(1)
		go func(sessionID string) {
			defer wg.Done()
			reservation, err := s.MarkHandbookRunning("auto-maint", childID, sessionID)
			results <- claimResult{reservation, err}
		}(sessionID)
	}
	wg.Wait()
	close(results)
	claimed := 0
	var winner MaintenanceReservation
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.reservation.Claimed {
			claimed++
			winner = result.reservation
		}
	}
	if claimed != 1 || winner.Receipt.SessionID == "" {
		t.Fatalf("claims=%d winner=%+v", claimed, winner)
	}
	resultJSON := json.RawMessage(`{"changed":true}`)
	settled, err := s.SettleHandbook("auto-maint", childID, 3, false, resultJSON, MaintenancePhaseComplete, now)
	if err != nil || settled.PhaseState != MaintenancePhaseComplete || settled.UsedTokens != 3 {
		t.Fatalf("settled=%+v err=%v", settled, err)
	}
	repeated, err := s.SettleHandbook("auto-maint", childID, 3, false, resultJSON, MaintenancePhaseComplete, now)
	if err != nil || repeated.UsedTokens != 3 {
		t.Fatalf("repeated=%+v err=%v", repeated, err)
	}
	if _, err := s.SettleHandbook("auto-maint", childID, 4, false, resultJSON, MaintenancePhaseComplete, now); err == nil {
		t.Fatal("different repeated settlement accepted")
	}
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.SettleHandbook("auto-maint", childID, 3, false, resultJSON, MaintenancePhaseComplete, now); err != nil {
		t.Fatalf("equivalent settlement after reopen rejected: %v", err)
	}
	parent, _ := reopened.GetMaintenanceReceipt(parentID)
	usage, _ := reopened.GetUsage("auto-maint")
	if parent.UsedTokens != 1 || usage.MaintenanceTokens != 4 {
		t.Fatalf("parent=%+v usage=%+v", parent, usage)
	}
}

func TestMarkHandbookRunningAfterReopenDoesNotReclaim(t *testing.T) {
	s, parentID, now := prepareHandbookParent(t)
	if _, err := s.PrepareHandbook(parentID, "auto-maint", 5, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	if claimed, err := s.MarkHandbookRunning("auto-maint", childID, "maintenance-running"); err != nil || !claimed.Claimed {
		t.Fatalf("claim=%+v err=%v", claimed, err)
	}
	reopened, err := OpenStore(s.path)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := reopened.MarkHandbookRunning("auto-maint", childID, "maintenance-other")
	if err != nil || claimed.Claimed || claimed.Receipt.SessionID != "maintenance-running" {
		t.Fatalf("reopened claim=%+v err=%v", claimed, err)
	}
}

func TestHandbookPhaseSaveFailuresRollBackStateAndUsage(t *testing.T) {
	s, parentID, now := prepareHandbookParent(t)
	if _, err := s.PrepareHandbook(parentID, "auto-maint", 5, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	path := s.path
	backup := path + ".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path); _ = os.Rename(backup, path) }()
	if _, err := s.MarkHandbookRunning("auto-maint", childID, "maintenance-fail"); err == nil {
		t.Fatal("claim save failure accepted")
	}
	child, _ := s.GetMaintenanceReceipt(childID)
	parent, _ := s.GetMaintenanceReceipt(parentID)
	if child.PhaseState != MaintenancePhasePrepared || child.SessionID != "" || parent.PhaseState != MaintenancePhasePrepared {
		t.Fatalf("claim state not rolled back child=%+v parent=%+v", child, parent)
	}
	// Restore the file so the claim can be persisted, then make settlement fail.
	_ = os.Remove(path)
	_ = os.Rename(backup, path)
	claimed, err := s.MarkHandbookRunning("auto-maint", childID, "maintenance-fail")
	if err != nil || !claimed.Claimed {
		t.Fatalf("claim after restore=%+v err=%v", claimed, err)
	}
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleHandbook("auto-maint", childID, 2, false, json.RawMessage(`{"ok":true}`), MaintenancePhaseComplete, now); err == nil {
		t.Fatal("settlement save failure accepted")
	}
	child, _ = s.GetMaintenanceReceipt(childID)
	parent, _ = s.GetMaintenanceReceipt(parentID)
	usage, _ := s.GetUsage("auto-maint")
	if child.PhaseState != MaintenancePhaseRunning || child.Status != "pending" || parent.PhaseState != MaintenancePhaseRunning || usage.MaintenanceTokens != 1 {
		t.Fatalf("settlement state not rolled back child=%+v parent=%+v usage=%+v", child, parent, usage)
	}
}

func TestMarkHandbookRunningRechecksMaintenanceEnabledAndSessionBinding(t *testing.T) {
	s, parentID, now := prepareHandbookParent(t)
	if _, err := s.PrepareHandbook(parentID, "auto-maint", 5, now); err != nil {
		t.Fatal(err)
	}
	childID := "handbook:" + parentID
	profile, _ := s.GetProfile("auto-maint")
	profile.MaintenanceEnabled = false
	if _, err := s.SaveProfile(profile, profile.Revision, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkHandbookRunning("auto-maint", childID, "session-a"); err == nil {
		t.Fatal("disabled maintenance was claimable")
	}
	profile, _ = s.GetProfile("auto-maint")
	profile.MaintenanceEnabled = true
	if _, err := s.SaveProfile(profile, profile.Revision, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMaintenanceSessionID("auto-maint", childID, "session-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarkHandbookRunning("auto-maint", childID, "session-b"); err == nil {
		t.Fatal("session binding was replaced")
	}
	claimed, err := s.MarkHandbookRunning("auto-maint", childID, "session-a")
	if err != nil || !claimed.Claimed {
		t.Fatalf("same session claim=%+v err=%v", claimed, err)
	}
}

func TestListHandbookParentsIncludesUnpreparedAndActiveBeforeCompleted(t *testing.T) {
	s, now := maintenanceStore(t)
	for i, id := range []string{"parent-0", "parent-1", "parent-2", "parent-3"} {
		if _, err := s.BeginMaintenance("auto-maint", id, id+"-fp", 1, now.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveMaintenanceResultWithEvidence("auto-maint", id, json.RawMessage(`[]`), json.RawMessage(`{"messages":[{"role":"user","content":"e"}]}`), int64(i+1), 0, false); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SettleMaintenance("auto-maint", id, 0, false, now); err != nil {
			t.Fatal(err)
		}
	}
	s.mu.Lock()
	for id, phase := range map[string]string{"parent-1": MaintenancePhasePrepared, "parent-2": MaintenancePhaseRunning, "parent-3": MaintenancePhaseComplete} {
		r := s.data.MaintenanceReceipts[id]
		r.HandbookReceiptID = "handbook:" + id
		r.PhaseState = phase
		s.data.MaintenanceReceipts[id] = r
	}
	if err := s.saveLocked(); err != nil {
		s.mu.Unlock()
		t.Fatal(err)
	}
	s.mu.Unlock()
	entries := s.ListHandbookParents("auto-maint", 0)
	if len(entries) != 4 || entries[0].ReceiptID != "parent-0" || entries[1].ReceiptID != "parent-1" || entries[2].ReceiptID != "parent-2" || entries[3].ReceiptID != "parent-3" {
		t.Fatalf("ordered parents=%+v", entries)
	}
	if limited := s.ListHandbookParents("auto-maint", 2); len(limited) != 2 || limited[0].ReceiptID != "parent-0" || limited[1].ReceiptID != "parent-1" {
		t.Fatalf("limited parents=%+v", limited)
	}
}
