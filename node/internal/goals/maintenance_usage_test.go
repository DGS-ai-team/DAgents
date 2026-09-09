package goals

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func maintenanceStore(t *testing.T) (*Store, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	s, err := OpenStore(filepath.Join(t.TempDir(), "goals.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProfile(AutoProfile{AgentID: "auto-maint", Enabled: true, Revision: 1, MaintenanceTokenBudget: 100, TotalTokenBudget: 200}, 0, now); err != nil {
		t.Fatal(err)
	}
	return s, now
}

func TestMaintenanceAvailableTokensTable(t *testing.T) {
	s, now := maintenanceStore(t)
	if got, ok := s.MaintenanceAvailableTokens(" auto-maint "); !ok || got != 100 {
		t.Fatalf("initial=%d ok=%v", got, ok)
	}
	if _, err := s.SettleMaintenance("auto-maint", "seed", 100, false, now); err == nil { /* no seed receipt */
	}
	if _, err := s.BeginMaintenance("auto-maint", "m-risk", "fp", 10, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MaintenanceAvailableTokens("auto-maint"); got != 100 {
		t.Fatalf("pending maintenance=%d", got)
	}
	if _, err := s.SaveProfile(AutoProfile{AgentID: "unlimited", Enabled: true}, 0, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MaintenanceAvailableTokens("unlimited"); got != math.MaxInt64 {
		t.Fatalf("unlimited=%d", got)
	}
	if _, err := s.SaveProfile(AutoProfile{AgentID: "exhausted", Enabled: true, MaintenanceTokenBudget: 1}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenance("exhausted", "e", "fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("exhausted", "e", 1, false, now); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.MaintenanceAvailableTokens("exhausted"); got != 0 {
		t.Fatalf("exhausted=%d", got)
	}
}

func TestMaintenanceReceiptPersistsAndSettlesIdempotently(t *testing.T) {
	s, now := maintenanceStore(t)
	reservation, err := s.BeginMaintenance("auto-maint", "m-1", "journal-1", 10, now)
	if err != nil || reservation.Receipt.Status != "pending" {
		t.Fatalf("begin=%+v err=%v", reservation, err)
	}
	if _, err := s.StartRun("missing", "wake", now); err == nil {
		t.Fatal("missing run unexpectedly started")
	}
	usage, err := s.SettleMaintenance("auto-maint", "m-1", 12, false, now.Add(time.Second))
	if err != nil || usage.MaintenanceTokens != 12 {
		t.Fatalf("settle=%+v err=%v", usage, err)
	}
	repeated, err := s.SettleMaintenance("auto-maint", "m-1", 12, false, now.Add(2*time.Second))
	if err != nil || repeated.MaintenanceTokens != 12 {
		t.Fatalf("repeat=%+v err=%v", repeated, err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "m-1", 13, false, now); err == nil {
		t.Fatal("different settlement accepted")
	}
	if again, err := s.BeginMaintenance("auto-maint", "m-1", "journal-1", 10, now.Add(time.Hour)); err != nil || again.Receipt.UsedTokens != 12 {
		t.Fatalf("idempotent begin=%+v err=%v", again, err)
	}
	path := s.path
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := reopened.GetMaintenanceReceipt("m-1")
	if !ok || r.Status != "settled" || r.UsedTokens != 12 {
		t.Fatalf("receipt=%+v ok=%v", r, ok)
	}
}

func TestBeginMaintenanceRejectsBusinessRunAndSharesReservationSlot(t *testing.T) {
	s, now := maintenanceStore(t)
	g, err := s.CreateManagedCycle(CreateInput{AgentID: "auto-maint", Objective: "work", Acceptance: "done"}, "cycle-1", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(g.ID, "wake", now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenance("auto-maint", "m-active", "journal", 1, now); err == nil {
		t.Fatal("maintenance allowed during business run")
	}
}

func TestPendingMaintenanceBlocksBusinessStart(t *testing.T) {
	s, now := maintenanceStore(t)
	if _, err := s.BeginMaintenance("auto-maint", "m-slot", "journal", 1, now); err != nil {
		t.Fatal(err)
	}
	g, err := s.CreateManagedCycle(CreateInput{AgentID: "auto-maint", Objective: "work", Acceptance: "done"}, "cycle-slot", 1, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRun(g.ID, "wake", now); err == nil {
		t.Fatal("business run started while maintenance reservation pending")
	}
}

func TestMaintenanceReceiptWriteFailureLeavesNoReceipt(t *testing.T) {
	s, now := maintenanceStore(t)
	path := s.path
	backup := path + ".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Remove(path); _ = os.Rename(backup, path) }()
	if _, err := s.BeginMaintenance("auto-maint", "m-fail", "journal", 1, now); err == nil {
		t.Fatal("write failure accepted")
	}
	if _, ok := s.GetMaintenanceReceipt("m-fail"); ok {
		t.Fatal("failed receipt remained in memory")
	}
}

func TestMaintenanceBudgetUnknownAndSettlementFailure(t *testing.T) {
	s, now := maintenanceStore(t)
	if _, err := s.BeginMaintenance("auto-maint", "m-unknown", "journal-u", 1, now); err != nil {
		t.Fatal(err)
	}
	path := s.path
	backup := path + ".bak"
	if err := os.Rename(path, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "m-unknown", 1, false, now); err == nil {
		t.Fatal("settlement write failure accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleMaintenance("auto-maint", "m-unknown", 1, true, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenance("auto-maint", "m-after-unknown", "journal-a", 1, now); err == nil {
		t.Fatal("unknown usage did not block maintenance")
	}
}

func TestMaintenanceBudgetAndOverflowBoundaries(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T, *Store, time.Time)
	}{
		{"maintenance exhausted", func(t *testing.T, s *Store, now time.Time) {
			if _, err := s.BeginMaintenance("auto-maint", "m-budget", "j-budget", 1, now); err != nil {
				t.Fatal(err)
			}
			if _, err := s.SettleMaintenance("auto-maint", "m-budget", 100, false, now); err != nil {
				t.Fatal(err)
			}
			if _, err := s.BeginMaintenance("auto-maint", "m-next", "j-next", 1, now); err == nil {
				t.Fatal("maintenance budget exhaustion accepted")
			}
		}},
		{"total exhausted", func(t *testing.T, s *Store, now time.Time) {
			if _, err := s.RecordRunUsage("auto-maint", "business-run", 200, 0, 0, now); err != nil {
				t.Fatal(err)
			}
			if _, err := s.BeginMaintenance("auto-maint", "m-total", "j-total", 1, now); err == nil {
				t.Fatal("total budget exhaustion accepted")
			}
		}},
		{"settlement may exceed estimate", func(t *testing.T, s *Store, now time.Time) {
			if _, err := s.BeginMaintenance("auto-maint", "m-over", "j-over", 1, now); err != nil {
				t.Fatal(err)
			}
			usage, err := s.SettleMaintenance("auto-maint", "m-over", 150, false, now)
			if err != nil || usage.MaintenanceTokens != 150 {
				t.Fatalf("usage=%+v err=%v", usage, err)
			}
		}},
		{"settlement overflow", func(t *testing.T, s *Store, now time.Time) {
			if _, err := s.BeginMaintenance("auto-maint", "m-overflow", "j-overflow", 1, now); err != nil {
				t.Fatal(err)
			}
			s.mu.Lock()
			s.data.Usage["auto-maint"] = AgentUsage{AgentID: "auto-maint", MaintenanceTokens: math.MaxInt64}
			s.mu.Unlock()
			if _, err := s.SettleMaintenance("auto-maint", "m-overflow", 1, false, now); err == nil {
				t.Fatal("usage overflow accepted")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, now := maintenanceStore(t)
			tc.run(t, s, now)
		})
	}
}

func TestBeginMaintenanceRejectsInvalidTimeAndFingerprint(t *testing.T) {
	s, now := maintenanceStore(t)
	if _, err := s.BeginMaintenance("auto-maint", "m-empty", "", 1, now); err == nil {
		t.Fatal("empty fingerprint accepted")
	}
	if _, err := s.BeginMaintenance("auto-maint", "m-zero", "journal", 1, time.Time{}); err == nil {
		t.Fatal("zero time accepted")
	}
}

func TestMaintenanceReceiptConcurrentReservationsPersistOneSlot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "goals.json")
	s1, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s1.SaveProfile(AutoProfile{AgentID: "auto-maint", Enabled: true, Revision: 1, MaintenanceTokenBudget: 1000, TotalTokenBudget: 2000}, 0, now); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int, st *Store) {
			defer wg.Done()
			_, beginErr := st.BeginMaintenance("auto-maint", "m-concurrent-"+string(rune('a'+i)), "j", 1, now)
			results <- beginErr
		}(i, s1)
	}
	wg.Wait()
	close(results)
	successes := 0
	for beginErr := range results {
		if beginErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent reservations succeeded=%d want 1", successes)
	}
	reopened, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, id := range []string{"m-concurrent-a", "m-concurrent-b"} {
		if _, ok := reopened.GetMaintenanceReceipt(id); ok {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("persisted concurrent receipts=%d want 1", found)
	}
}
