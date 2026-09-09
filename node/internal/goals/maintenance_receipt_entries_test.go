package goals

import (
	"encoding/json"
	"testing"
)

func TestListMaintenanceReceiptEntriesUsesRealIDsAndDeepCopies(t *testing.T) {
	s, now := maintenanceStore(t)
	if _, err := s.BeginMaintenance("auto-maint", "receipt-from-manual-run", "manual-fingerprint", 1, now); err != nil {
		t.Fatal(err)
	}
	evidence := json.RawMessage(`{"messages":[{"role":"user","content":"audit"}]}`)
	if err := s.SaveMaintenanceResultWithEvidence("auto-maint", "receipt-from-manual-run", json.RawMessage(`[{"candidate":true}]`), evidence, 2, 0, false); err != nil {
		t.Fatal(err)
	}
	entries := s.ListMaintenanceReceiptEntries(" auto-maint ")
	if len(entries) != 1 || entries[0].ReceiptID != "receipt-from-manual-run" {
		t.Fatalf("entries=%+v", entries)
	}
	entries[0].Receipt.CandidateJSON[0] = 'x'
	entries[0].Receipt.EvidenceJSON[0] = 'x'
	stored, ok := s.GetMaintenanceReceipt("receipt-from-manual-run")
	if !ok || stored.CandidateJSON[0] == 'x' || stored.EvidenceJSON[0] == 'x' {
		t.Fatal("receipt entry exposed mutable storage")
	}
	if _, err := s.SettleMaintenance("auto-maint", "receipt-from-manual-run", 0, false, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginMaintenance("auto-maint", "z-real-receipt", "z-fingerprint", 1, now); err != nil {
		t.Fatal(err)
	}
	entries = s.ListMaintenanceReceiptEntries("auto-maint")
	if len(entries) != 2 || entries[0].ReceiptID != "receipt-from-manual-run" || entries[1].ReceiptID != "z-real-receipt" {
		t.Fatalf("sorted entries=%+v", entries)
	}
	if got := s.ListMaintenanceReceiptEntries("other-agent"); len(got) != 0 {
		t.Fatalf("cross-agent entries=%+v", got)
	}
}
