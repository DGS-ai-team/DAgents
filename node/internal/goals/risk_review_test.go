package goals

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRiskReviewReserveSettlePersistsAndDeduplicates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "goals.json")
	now := time.Now().UTC()
	s, _ := OpenStore(path)
	p, err := s.SaveProfile(AutoProfile{AgentID: "risk-agent", Enabled: true, TotalTokenBudget: 100}, 0, now)
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.BeginRiskReview("risk-agent", "op-1", "req-1", "fp", 10, now)
	if err != nil || !r.Claimed {
		t.Fatalf("reserve=%+v err=%v", r, err)
	}
	repeat, err := s.BeginRiskReview("risk-agent", "op-1", "req-1", "fp", 10, now)
	if err != nil || repeat.Claimed {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}
	if _, err := s.BeginRiskReview("risk-agent", "op-2", "req-2", "fp2", 10, now); err == nil {
		t.Fatal("pending risk review was not blocked")
	}
	u, err := s.SettleRiskReview("risk-agent", "op-1", 8, true, now.Add(time.Second))
	if err != nil || u.RiskReviewTokens != 8 || !u.RiskReviewUnknown || u.MaintenanceTokens != 0 {
		t.Fatalf("settle=%+v err=%v", u, err)
	}
	if _, err := OpenStore(path); err != nil {
		t.Fatal(err)
	}
	if p.Revision == 0 {
		t.Fatal("profile revision")
	}
}

func TestRiskObservationStoresDigestOnlyAndIsIdempotent(t *testing.T) {
	s, _ := OpenStore("")
	now := time.Now().UTC()
	if _, err := s.SaveProfile(AutoProfile{AgentID: "risk-agent", Enabled: true}, 0, now); err != nil {
		t.Fatal(err)
	}
	r := RiskObservationRecord{AgentID: "risk-agent", OperationID: "op", RequestID: "req", ToolName: "bash_run", ArgsDigest: DigestRiskArgs([]byte("secret command")), Level: "high", Reason: "review"}
	if _, err := s.BeginRiskReview("risk-agent", "op", "req", "fp", 1, now); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRiskObservation(r); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRiskObservation(r); err != nil {
		t.Fatal(err)
	}
	if r.ArgsDigest == "secret command" || len(r.ArgsDigest) != 64 {
		t.Fatalf("digest=%q", r.ArgsDigest)
	}
}

func TestRiskReservationCountsAgainstBusinessAndMaintenanceBudget(t *testing.T) {
	now := time.Now().UTC()
	s, _ := OpenStore("")
	if _, err := s.SaveProfile(AutoProfile{AgentID: "budget-agent", Enabled: true, TotalTokenBudget: 100, MaintenanceTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRiskReview("budget-agent", "risk-80", "request-80", "fp", 80, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordRunUsage("budget-agent", "business-30", 30, 0, 0, now); err != nil {
		t.Fatalf("already-produced business usage was not recorded: %v", err)
	}
	if _, err := s.BeginMaintenance("budget-agent", "maintenance-30", "fp-maint", 30, now); err == nil {
		t.Fatal("maintenance operation ignored pending risk reservation")
	}
}

func TestRiskReservationIncludesPendingMaintenance(t *testing.T) {
	now := time.Now().UTC()
	s, _ := OpenStore("")
	if _, err := s.SaveProfile(AutoProfile{AgentID: "cross-agent", Enabled: true, TotalTokenBudget: 100, MaintenanceTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenance("cross-agent", "maintenance-80", "fp-maint", 80, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRiskReview("cross-agent", "risk-30", "request-30", "fp-risk", 30, now); err == nil {
		t.Fatal("risk reservation ignored pending maintenance reservation")
	}
}

func TestUnknownRiskReservationKeepsBudgetAndCombinedObservationIsAtomic(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "goals.json")
	s, _ := OpenStore(path)
	if _, err := s.SaveProfile(AutoProfile{AgentID: "unknown-agent", Enabled: true, TotalTokenBudget: 100, MaintenanceTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRiskReview("unknown-agent", "risk-80", "request-80", "fp", 80, now); err != nil {
		t.Fatal(err)
	}
	obs := RiskObservationRecord{AgentID: "unknown-agent", OperationID: "risk-80", RequestID: "request-80", ToolName: "shell", ArgsDigest: DigestRiskArgs([]byte("secret")), Level: "unknown", Reason: "timeout"}
	usage, err := s.SettleRiskReviewWithObservation("unknown-agent", "risk-80", 0, true, obs, now.Add(time.Second))
	if err != nil || usage.RiskReviewTokens != 0 || !usage.RiskReviewUnknown {
		t.Fatalf("usage=%+v err=%v", usage, err)
	}
	if _, err := s.BeginMaintenance("unknown-agent", "maintenance-30", "fp-maint", 30, now.Add(2*time.Second)); err == nil {
		t.Fatal("unknown risk reservation was released from total budget")
	}
	if _, err := s.SettleRiskReviewWithObservation("unknown-agent", "risk-80", 0, true, obs, now.Add(3*time.Second)); err != nil {
		t.Fatalf("idempotent combined settle: %v", err)
	}
	if got := s.data.Usage["unknown-agent"].RiskReviewTokens; got != 0 {
		t.Fatalf("duplicate settlement charged %d", got)
	}
}

func TestRiskSettlementObservationRollbackTogether(t *testing.T) {
	now := time.Now().UTC()
	path := filepath.Join(t.TempDir(), "goals.json")
	s, _ := OpenStore(path)
	if _, err := s.SaveProfile(AutoProfile{AgentID: "rollback-agent", Enabled: true, TotalTokenBudget: 100}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRiskReview("rollback-agent", "risk-1", "request-1", "fp", 10, now); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.path = filepath.Join(blocker, "goals.json")
	s.mu.Unlock()
	obs := RiskObservationRecord{AgentID: "rollback-agent", OperationID: "risk-1", RequestID: "request-1", ArgsDigest: DigestRiskArgs([]byte("x")), Level: "low"}
	if _, err := s.SettleRiskReviewWithObservation("rollback-agent", "risk-1", 7, false, obs, now.Add(time.Second)); err == nil {
		t.Fatal("write failure accepted")
	}
	if got := s.data.Usage["rollback-agent"].RiskReviewTokens; got != 0 {
		t.Fatalf("usage changed after failed transaction: %d", got)
	}
	if got := s.data.RiskReviewReceipts["risk-1"].Status; got != "pending" {
		t.Fatalf("receipt changed after failed transaction: %s", got)
	}
	if _, ok := s.data.RiskObservations["risk-1:request-1"]; ok {
		t.Fatal("observation remained after failed transaction")
	}
}

func TestRiskSettlementNormalizesOperationMapKey(t *testing.T) {
	now := time.Now().UTC()
	s, _ := OpenStore("")
	if _, err := s.SaveProfile(AutoProfile{AgentID: "trim-agent", Enabled: true}, 0, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginRiskReview("trim-agent", "op-trim", "req-trim", "fp", 2, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SettleRiskReview(" trim-agent ", "  op-trim  ", 2, false, now); err != nil {
		t.Fatal(err)
	}
	if got := s.data.RiskReviewReceipts["op-trim"].Status; got != "settled" {
		t.Fatalf("normalized receipt status=%q", got)
	}
	if _, ok := s.data.RiskReviewReceipts["  op-trim  "]; ok {
		t.Fatal("untrimmed operation key was created")
	}
}

func TestRiskReservationOverflowIsReported(t *testing.T) {
	s, _ := OpenStore("")
	s.mu.Lock()
	s.data.RiskReviewReceipts["one"] = RiskReviewReceipt{AgentID: "a", Status: "pending", EstimatedTokens: 1}
	s.data.RiskReviewReceipts["max"] = RiskReviewReceipt{AgentID: "a", Status: "pending", EstimatedTokens: int64(^uint64(0) >> 1)}
	_, ok := s.totalUsageLocked("a", AgentUsage{})
	s.mu.Unlock()
	if ok {
		t.Fatal("risk reservation overflow was silently accepted")
	}
}

func TestRiskSchemaV3BackupAndFutureRejection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "goals.json")
	original := []byte(`{"schema_version":3,"profiles":{},"usage":{}}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(path); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(path + ".v3.bak")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("backup err=%v bytes=%q", err, backup)
	}
	if _, err := OpenStore(path); err != nil {
		t.Fatal(err)
	}
	backupAgain, _ := os.ReadFile(path + ".v3.bak")
	if !bytes.Equal(backupAgain, original) {
		t.Fatal("migration overwrote existing v3 backup")
	}
	future := filepath.Join(dir, "future.json")
	b, _ := json.Marshal(map[string]any{"schema_version": CurrentSchemaVersion + 1})
	if err := os.WriteFile(future, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenStore(future); err == nil {
		t.Fatal("future schema was accepted")
	}
}
