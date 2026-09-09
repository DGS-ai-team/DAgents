package goals

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintenanceSessionIdentityPersistenceAndValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	now := time.Now().UTC()
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveProfile(AutoProfile{AgentID: "identity-agent", Enabled: true, MaintenanceTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenance("identity-agent", "receipt-1", "fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMaintenanceSessionID("identity-agent", "receipt-1", ""); err == nil {
		t.Fatal("empty session accepted")
	}
	if err := s.SetMaintenanceSessionID("identity-agent", "receipt-1", "maintenance-user-123"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMaintenanceSessionID("other-agent", "receipt-1", "x"); err == nil {
		t.Fatal("cross-agent bind accepted")
	}
	if err := s.SetMaintenanceSessionID("identity-agent", "receipt-1", "other"); err == nil {
		t.Fatal("rebind accepted")
	}
	if err := s.SetMaintenanceSessionID("identity-agent", "receipt-1", "maintenance-user-123"); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.IsMaintenanceSession("identity-agent", "maintenance-user-123") {
		t.Fatal("identity lost after reopen")
	}
	if _, err := s2.SettleMaintenance("identity-agent", "receipt-1", 1, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s2.BeginMaintenance("identity-agent", "receipt-2", "fp-2", 1, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	// Force persistence failure on a previously unbound receipt and verify the
	// attempted identity does not remain in memory.
	block := filepath.Join(t.TempDir(), "block")
	if err := os.WriteFile(block, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	s2.path = filepath.Join(block, "goals.json")
	if err := s2.SetMaintenanceSessionID("identity-agent", "receipt-2", "maintenance-user-456"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if s2.IsMaintenanceSession("identity-agent", "maintenance-user-456") {
		t.Fatal("failed bind remained in memory")
	}
	if !s2.IsMaintenanceSession("identity-agent", "maintenance-user-123") {
		t.Fatal("original bind was not restored")
	}
}
