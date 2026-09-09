package goals

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// BeginMaintenanceForOccurrence claims a maintenance receipt for one durable
// scheduled occurrence. The occurrence identity is persisted with the receipt
// so a manual receipt cannot be reused by a different calendar tick.
func (s *Store) BeginMaintenanceForOccurrence(agentID, localDate string, scheduleRevision int64, receiptID, fingerprint string, estimated int64, now time.Time) (MaintenanceReservation, error) {
	agentID, localDate, receiptID, fingerprint = strings.TrimSpace(agentID), strings.TrimSpace(localDate), strings.TrimSpace(receiptID), strings.TrimSpace(fingerprint)
	if agentID == "" || localDate == "" || scheduleRevision <= 0 || receiptID == "" || fingerprint == "" || estimated < 0 || now.IsZero() {
		return MaintenanceReservation{}, fmt.Errorf("invalid occurrence maintenance reservation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := maintenanceOccurrenceKey(agentID, localDate, scheduleRevision)
	occur, ok := s.data.MaintenanceOccurrences[key]
	if !ok || occur.AgentID != agentID || occur.LocalDate != localDate || occur.ScheduleRevision != scheduleRevision || occur.Status != MaintenanceOccurrencePending || s.recoveryOccurrences[key] {
		return MaintenanceReservation{}, fmt.Errorf("maintenance occurrence is not claimable")
	}
	profile, ok := s.data.Profiles[agentID]
	if !ok || !profile.Enabled || !profile.MaintenanceEnabled || profile.MaintenanceRevision != scheduleRevision {
		return MaintenanceReservation{}, fmt.Errorf("maintenance occurrence configuration changed")
	}
	if existing, exists := s.data.MaintenanceReceipts[receiptID]; exists {
		if existing.AgentID != agentID || existing.Fingerprint != fingerprint || existing.EstimatedTokens != estimated || existing.OccurrenceLocalDate != localDate || existing.OccurrenceScheduleRevision != scheduleRevision {
			return MaintenanceReservation{}, fmt.Errorf("maintenance receipt occurrence differs")
		}
		return MaintenanceReservation{Receipt: cloneMaintenanceReceipt(existing), Claimed: false}, nil
	}
	reservation, err := s.beginMaintenanceLocked(agentID, receiptID, fingerprint, estimated, now, false)
	if err != nil {
		return MaintenanceReservation{}, err
	}
	r := reservation.Receipt
	r.OccurrenceLocalDate = localDate
	r.OccurrenceScheduleRevision = scheduleRevision
	s.data.MaintenanceReceipts[receiptID] = r
	if err := s.saveLocked(); err != nil {
		delete(s.data.MaintenanceReceipts, receiptID)
		return MaintenanceReservation{}, err
	}
	reservation.Receipt = cloneMaintenanceReceipt(r)
	return reservation, nil
}

// ListMaintenanceOccurrenceReceipts returns both the memory parent and its
// handbook children for one exact scheduled occurrence.
func (s *Store) ListMaintenanceOccurrenceReceipts(agentID, localDate string, scheduleRevision int64) []MaintenanceReceiptEntry {
	agentID, localDate = strings.TrimSpace(agentID), strings.TrimSpace(localDate)
	if s == nil || agentID == "" || localDate == "" || scheduleRevision <= 0 {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]MaintenanceReceiptEntry, 0)
	for id, receipt := range s.data.MaintenanceReceipts {
		if receipt.AgentID == agentID && receipt.OccurrenceLocalDate == localDate && receipt.OccurrenceScheduleRevision == scheduleRevision {
			entries = append(entries, MaintenanceReceiptEntry{ReceiptID: id, Receipt: cloneMaintenanceReceipt(receipt)})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ReceiptID < entries[j].ReceiptID })
	return entries
}
