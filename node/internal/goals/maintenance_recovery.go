package goals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type MaintenanceRecoverySnapshot struct {
	Occurrence MaintenanceOccurrence
	Receipts   []MaintenanceReceiptEntry
	Profile    AutoProfile
	Usage      AgentUsage
	Token      string
}

func (s *Store) GetMaintenanceRecoverySnapshot(agentID, localDate string, revision int64) (MaintenanceRecoverySnapshot, error) {
	agentID, localDate = strings.TrimSpace(agentID), strings.TrimSpace(localDate)
	if s == nil || agentID == "" || localDate == "" || revision <= 0 {
		return MaintenanceRecoverySnapshot{}, fmt.Errorf("invalid maintenance recovery snapshot")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := maintenanceOccurrenceKey(agentID, localDate, revision)
	occurrence, ok := s.data.MaintenanceOccurrences[key]
	if !ok || occurrence.AgentID != agentID {
		return MaintenanceRecoverySnapshot{}, ErrNotFound
	}
	profile, ok := s.data.Profiles[agentID]
	if !ok {
		return MaintenanceRecoverySnapshot{}, ErrNotFound
	}
	return maintenanceRecoverySnapshotLocked(s, agentID, localDate, revision, occurrence, profile), nil
}

func maintenanceRecoveryToken(snapshot MaintenanceRecoverySnapshot) string {
	canonical := struct {
		Occurrence MaintenanceOccurrence
		Receipts   []MaintenanceReceiptEntry
		Profile    AutoProfile
		Usage      AgentUsage
	}{snapshot.Occurrence, snapshot.Receipts, snapshot.Profile, snapshot.Usage}
	raw, _ := json.Marshal(canonical)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *Store) ResumeMaintenanceOccurrence(agentID, localDate string, revision int64, expectedToken string, now time.Time) (MaintenanceOccurrence, error) {
	agentID, localDate, expectedToken = strings.TrimSpace(agentID), strings.TrimSpace(localDate), strings.TrimSpace(expectedToken)
	if s == nil || agentID == "" || localDate == "" || revision <= 0 || expectedToken == "" || now.IsZero() {
		return MaintenanceOccurrence{}, fmt.Errorf("invalid maintenance recovery")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := maintenanceOccurrenceKey(agentID, localDate, revision)
	occurrence, ok := s.data.MaintenanceOccurrences[key]
	if !ok || occurrence.AgentID != agentID {
		return MaintenanceOccurrence{}, ErrNotFound
	}
	if occurrence.Status != MaintenanceOccurrenceRecoveryRequired && !(occurrence.Status == MaintenanceOccurrencePending && s.recoveryOccurrences[key]) {
		return MaintenanceOccurrence{}, fmt.Errorf("maintenance occurrence is not awaiting recovery")
	}
	profile, ok := s.data.Profiles[agentID]
	if !ok || !profile.Enabled || !profile.MaintenanceEnabled || profile.MaintenanceRevision != revision {
		return MaintenanceOccurrence{}, fmt.Errorf("maintenance recovery configuration changed")
	}
	u := s.data.Usage[agentID]
	if u.Unknown || u.UnknownTokens > 0 {
		return MaintenanceOccurrence{}, ErrUsageUnknown
	}
	parents := make([]MaintenanceReceiptEntry, 0)
	children := make(map[string]MaintenanceReceipt)
	for id, receipt := range s.data.MaintenanceReceipts {
		if receipt.AgentID != agentID || receipt.OccurrenceLocalDate != localDate || receipt.OccurrenceScheduleRevision != revision {
			continue
		}
		if receipt.ParentReceiptID == "" {
			parents = append(parents, MaintenanceReceiptEntry{ReceiptID: id, Receipt: receipt})
		} else {
			children[id] = receipt
		}
	}
	if len(parents) == 0 {
		return MaintenanceOccurrence{}, fmt.Errorf("maintenance memory parent is missing")
	}
	for _, entry := range parents {
		parent := entry.Receipt
		if parent.Status != "settled" || parent.Unknown || parent.NextCursor <= 0 || len(parent.CandidateJSON) == 0 || len(parent.EvidenceJSON) == 0 {
			return MaintenanceOccurrence{}, fmt.Errorf("maintenance memory parent is not recoverable")
		}
		if parent.HandbookReceiptID == "" {
			if parent.PhaseState != "" {
				return MaintenanceOccurrence{}, fmt.Errorf("maintenance parent phase is not recoverable")
			}
			continue
		}
		child, exists := children[parent.HandbookReceiptID]
		if !exists || child.ParentReceiptID != entry.ReceiptID || child.AgentID != agentID || (child.PhaseState == MaintenancePhasePrepared && (child.Status != "pending" || child.Unknown)) || !(child.PhaseState == MaintenancePhasePrepared || (child.PhaseState == MaintenancePhaseComplete && child.Status == "settled" && !child.Unknown)) {
			return MaintenanceOccurrence{}, fmt.Errorf("maintenance handbook child is not recoverable")
		}
		if (child.PhaseState == MaintenancePhasePrepared && parent.PhaseState != MaintenancePhasePrepared) || (child.PhaseState == MaintenancePhaseComplete && parent.PhaseState != MaintenancePhaseComplete) {
			return MaintenanceOccurrence{}, fmt.Errorf("maintenance parent phase does not match child")
		}
	}
	for childID, child := range children {
		parent, exists := s.data.MaintenanceReceipts[child.ParentReceiptID]
		if !exists || parent.ParentReceiptID != "" || parent.AgentID != agentID || parent.OccurrenceLocalDate != localDate || parent.OccurrenceScheduleRevision != revision || parent.HandbookReceiptID != childID {
			return MaintenanceOccurrence{}, fmt.Errorf("maintenance handbook child is orphaned")
		}
	}
	// Rebuild the snapshot from the locked state and compare its CAS token.
	snapshot := maintenanceRecoverySnapshotLocked(s, agentID, localDate, revision, occurrence, profile)
	if snapshot.Token != expectedToken {
		return MaintenanceOccurrence{}, ErrConflict
	}
	old := occurrence
	oldRecovery := s.recoveryOccurrences[key]
	now = now.UTC()
	occurrence.Status = MaintenanceOccurrencePending
	occurrence.FinishedAt = nil
	occurrence.RecoveryCount++
	occurrence.LastRecoveryAt = &now
	occurrence.UpdatedAt = now
	s.data.MaintenanceOccurrences[key] = occurrence
	delete(s.recoveryOccurrences, key)
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceOccurrences[key] = old
		if oldRecovery {
			s.recoveryOccurrences[key] = true
		}
		return MaintenanceOccurrence{}, err
	}
	return cloneMaintenanceOccurrence(occurrence), nil
}

func maintenanceRecoverySnapshotLocked(s *Store, agentID, localDate string, revision int64, occurrence MaintenanceOccurrence, profile AutoProfile) MaintenanceRecoverySnapshot {
	key := maintenanceOccurrenceKey(agentID, localDate, revision)
	occurrence = cloneMaintenanceOccurrence(occurrence)
	if occurrence.Status == MaintenanceOccurrencePending && s.recoveryOccurrences[key] {
		occurrence.Status = MaintenanceOccurrenceRecoveryRequired
	}
	receipts := make([]MaintenanceReceiptEntry, 0)
	for id, receipt := range s.data.MaintenanceReceipts {
		if receipt.AgentID == agentID && receipt.OccurrenceLocalDate == localDate && receipt.OccurrenceScheduleRevision == revision {
			receipts = append(receipts, MaintenanceReceiptEntry{ReceiptID: id, Receipt: cloneMaintenanceReceipt(receipt)})
		}
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].ReceiptID < receipts[j].ReceiptID })
	snapshot := MaintenanceRecoverySnapshot{Occurrence: occurrence, Receipts: receipts, Profile: profile, Usage: s.data.Usage[agentID]}
	snapshot.Token = maintenanceRecoveryToken(snapshot)
	return snapshot
}

func cloneMaintenanceOccurrence(o MaintenanceOccurrence) MaintenanceOccurrence {
	if o.FinishedAt != nil {
		v := *o.FinishedAt
		o.FinishedAt = &v
	}
	if o.LastRecoveryAt != nil {
		v := *o.LastRecoveryAt
		o.LastRecoveryAt = &v
	}
	return o
}
