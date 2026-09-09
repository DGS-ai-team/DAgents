package goals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ReconcileHandbookCompletion commits a completion proven by an already
// persisted session turn. The occurrence itself is deliberately untouched;
// its owner must perform the separate occurrence transition.
func (s *Store) ReconcileHandbookCompletion(agentID, localDate string, revision int64, expectedSnapshotToken, childID, sessionID, turnID string, actualUsed int64, result json.RawMessage, now time.Time) (MaintenanceReceipt, error) {
	agentID, localDate, expectedSnapshotToken = strings.TrimSpace(agentID), strings.TrimSpace(localDate), strings.TrimSpace(expectedSnapshotToken)
	childID, sessionID, turnID = strings.TrimSpace(childID), strings.TrimSpace(sessionID), strings.TrimSpace(turnID)
	if agentID == "" || localDate == "" || revision <= 0 || expectedSnapshotToken == "" || childID == "" || sessionID == "" || turnID == "" || actualUsed < 0 || now.IsZero() {
		return MaintenanceReceipt{}, fmt.Errorf("invalid handbook reconciliation")
	}
	normalized, err := normalizeMaintenanceEvidence(result)
	if err != nil {
		return MaintenanceReceipt{}, fmt.Errorf("invalid handbook result: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := maintenanceOccurrenceKey(agentID, localDate, revision)
	occurrence, ok := s.data.MaintenanceOccurrences[key]
	if !ok || occurrence.AgentID != agentID {
		return MaintenanceReceipt{}, ErrNotFound
	}
	profile, ok := s.data.Profiles[agentID]
	if !ok {
		return MaintenanceReceipt{}, ErrNotFound
	}
	child, ok := s.data.MaintenanceReceipts[childID]
	if !ok || child.AgentID != agentID || child.OccurrenceLocalDate != localDate || child.OccurrenceScheduleRevision != revision {
		return MaintenanceReceipt{}, ErrNotFound
	}
	if child.SessionID != sessionID || child.TurnID != turnID || child.ParentReceiptID == "" {
		return MaintenanceReceipt{}, fmt.Errorf("handbook turn identity differs")
	}
	parent, ok := s.data.MaintenanceReceipts[child.ParentReceiptID]
	if !ok || parent.AgentID != agentID || parent.ParentReceiptID != "" || parent.HandbookReceiptID != childID || parent.OccurrenceLocalDate != localDate || parent.OccurrenceScheduleRevision != revision || parent.Status != "settled" || parent.Unknown {
		return MaintenanceReceipt{}, fmt.Errorf("handbook parent is not valid")
	}
	if child.Unknown {
		return MaintenanceReceipt{}, ErrUsageUnknown
	}
	// A retry of the exact request is idempotent even though the occurrence
	// token changed when the first completion was saved.
	if child.Status == "settled" && child.PhaseState == MaintenancePhaseComplete && parent.PhaseState == MaintenancePhaseComplete && child.UsedTokens == actualUsed && child.ReconciliationToken == expectedSnapshotToken {
		storedEvidence, normalizeErr := normalizeStoredMaintenanceEvidence(child.ReconciliationEvidence)
		if normalizeErr == nil && bytes.Equal(storedEvidence, normalized) {
			return cloneMaintenanceReceipt(child), nil
		}
	}
	if occurrence.Status != MaintenanceOccurrenceRecoveryRequired && !(occurrence.Status == MaintenanceOccurrencePending && s.recoveryOccurrences[key]) {
		return MaintenanceReceipt{}, fmt.Errorf("maintenance occurrence is not awaiting recovery")
	}
	snapshot := maintenanceRecoverySnapshotLocked(s, agentID, localDate, revision, occurrence, profile)
	if snapshot.Token != expectedSnapshotToken {
		return MaintenanceReceipt{}, ErrConflict
	}
	if child.Status == "settled" {
		if child.PhaseState != MaintenancePhaseRecovery || parent.PhaseState != MaintenancePhaseRecovery || child.UsedTokens != actualUsed || len(child.ReconciliationEvidence) > 0 && !bytes.Equal(child.ReconciliationEvidence, normalized) {
			return MaintenanceReceipt{}, ErrConflict
		}
	} else if child.Status != "pending" || child.PhaseState != MaintenancePhaseRunning {
		return MaintenanceReceipt{}, fmt.Errorf("handbook receipt is not reconcilable")
	} else if parent.PhaseState != MaintenancePhaseRunning {
		return MaintenanceReceipt{}, fmt.Errorf("handbook parent phase is not running")
	}
	oldChild, oldParent := child, parent
	oldUsage, oldUsageExists := s.data.Usage[agentID]
	if child.Status == "pending" {
		if _, err := s.settleMaintenanceLocked(agentID, childID, actualUsed, false, now, false); err != nil {
			return MaintenanceReceipt{}, err
		}
		child = s.data.MaintenanceReceipts[childID]
	}
	child.PhaseState = MaintenancePhaseComplete
	child.ReconciliationEvidence = append([]byte(nil), normalized...)
	child.ReconciledAt = now.UTC()
	child.ReconciliationToken = expectedSnapshotToken
	parent.PhaseState = MaintenancePhaseComplete
	s.data.MaintenanceReceipts[childID], s.data.MaintenanceReceipts[child.ParentReceiptID] = child, parent
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceReceipts[childID], s.data.MaintenanceReceipts[child.ParentReceiptID] = oldChild, oldParent
		if oldUsageExists {
			s.data.Usage[agentID] = oldUsage
		} else {
			delete(s.data.Usage, agentID)
		}
		return MaintenanceReceipt{}, err
	}
	return cloneMaintenanceReceipt(child), nil
}

// normalizeStoredMaintenanceEvidence compacts the store's JSON encoding
// before applying the bound. saveLocked may pretty-print otherwise-valid
// evidence, so the bound is on the canonical value rather than whitespace.
func normalizeStoredMaintenanceEvidence(evidence json.RawMessage) ([]byte, error) {
	if len(evidence) == 0 || !json.Valid(evidence) {
		return nil, fmt.Errorf("invalid maintenance evidence")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, evidence); err != nil || compact.Len() > maxMaintenanceEvidenceBytes {
		return nil, fmt.Errorf("invalid maintenance evidence")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(compact.Bytes(), &object) != nil || object == nil {
		return nil, fmt.Errorf("invalid maintenance evidence")
	}
	return compact.Bytes(), nil
}
