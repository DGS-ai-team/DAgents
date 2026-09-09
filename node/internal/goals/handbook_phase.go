package goals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	MaintenancePhasePrepared = "prepared"
	MaintenancePhaseRunning  = "running"
	MaintenancePhaseComplete = "completed"
	MaintenancePhaseRecovery = "recovery_required"
)

type MaintenanceReceiptEntry struct {
	ReceiptID string
	Receipt   MaintenanceReceipt
}

// BindHandbookTurn durably associates a running handbook child with the
// concrete turn created by the session lifecycle.  The association is
// immutable once established so recovery cannot accidentally attribute a
// later turn to the same receipt.
func (s *Store) BindHandbookTurn(agentID, childID, sessionID, turnID string, attemptedAt time.Time) error {
	agentID, childID = strings.TrimSpace(agentID), strings.TrimSpace(childID)
	sessionID, turnID = strings.TrimSpace(sessionID), strings.TrimSpace(turnID)
	if agentID == "" || childID == "" || sessionID == "" || turnID == "" || attemptedAt.IsZero() {
		return fmt.Errorf("invalid handbook turn binding")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	child, ok := s.data.MaintenanceReceipts[childID]
	if !ok || child.AgentID != agentID {
		return ErrNotFound
	}
	if child.Status != "pending" || child.PhaseState != MaintenancePhaseRunning {
		return fmt.Errorf("handbook receipt is not running")
	}
	if child.ParentReceiptID == "" {
		return fmt.Errorf("handbook parent is missing")
	}
	parent, ok := s.data.MaintenanceReceipts[child.ParentReceiptID]
	if !ok || parent.AgentID != agentID || parent.HandbookReceiptID != childID {
		return fmt.Errorf("handbook parent is not valid")
	}
	if child.SessionID != sessionID {
		return fmt.Errorf("handbook session differs")
	}
	if child.TurnID != "" {
		if child.TurnID == turnID {
			return nil
		}
		return fmt.Errorf("handbook turn already bound")
	}
	old := child
	child.TurnID = turnID
	child.AttemptedAt = attemptedAt.UTC()
	child.UpdatedAt = attemptedAt.UTC()
	s.data.MaintenanceReceipts[childID] = child
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceReceipts[childID] = old
		return err
	}
	return nil
}

// ListHandbookParents returns durable parent receipts with their map identity
// so callers can resume or block a handbook phase without guessing IDs.
func (s *Store) ListHandbookParents(agentID string, limit int) []MaintenanceReceiptEntry {
	agentID = strings.TrimSpace(agentID)
	if s == nil || agentID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]MaintenanceReceiptEntry, 0)
	for id, receipt := range s.data.MaintenanceReceipts {
		// Child receipts point to a parent; every settled memory receipt with
		// evidence is a resumable handbook parent, including those already
		// prepared or recovering.
		if receipt.AgentID != agentID || receipt.ParentReceiptID != "" || receipt.Status != "settled" || len(receipt.EvidenceJSON) == 0 {
			continue
		}
		receipt = cloneMaintenanceReceipt(receipt)
		entries = append(entries, MaintenanceReceiptEntry{ReceiptID: id, Receipt: receipt})
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if (a.Receipt.PhaseState == MaintenancePhaseComplete) != (b.Receipt.PhaseState == MaintenancePhaseComplete) {
			return a.Receipt.PhaseState != MaintenancePhaseComplete
		}
		if a.Receipt.NextCursor != b.Receipt.NextCursor {
			return a.Receipt.NextCursor < b.Receipt.NextCursor
		}
		if !a.Receipt.CreatedAt.Equal(b.Receipt.CreatedAt) {
			return a.Receipt.CreatedAt.Before(b.Receipt.CreatedAt)
		}
		return a.ReceiptID < b.ReceiptID
	})
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

// PrepareHandbook durably links a handbook child receipt to a settled memory
// receipt and reserves its budget under one store lock. The child ID is stable
// so retries after a restart cannot create another handbook reservation.
func (s *Store) PrepareHandbook(parentID, agentID string, estimated int64, now time.Time) (MaintenanceReservation, error) {
	parentID, agentID = strings.TrimSpace(parentID), strings.TrimSpace(agentID)
	if parentID == "" || agentID == "" || estimated < 0 || now.IsZero() {
		return MaintenanceReservation{}, fmt.Errorf("invalid handbook preparation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	parent, ok := s.data.MaintenanceReceipts[parentID]
	if !ok || parent.AgentID != agentID {
		return MaintenanceReservation{}, ErrNotFound
	}
	if parent.ParentReceiptID != "" || parent.Unknown || parent.Status != "settled" || len(parent.EvidenceJSON) == 0 {
		return MaintenanceReservation{}, fmt.Errorf("memory maintenance is not ready for handbook")
	}
	childID := "handbook:" + parentID
	if parent.HandbookReceiptID != "" && parent.HandbookReceiptID != childID {
		return MaintenanceReservation{}, fmt.Errorf("maintenance parent already linked")
	}
	oldParent := parent
	child, childExists := s.data.MaintenanceReceipts[childID]
	oldChild := child
	reservation, err := s.beginMaintenanceLocked(agentID, childID, "handbook:"+parentID, estimated, now, false)
	if err != nil {
		return MaintenanceReservation{}, err
	}
	if childExists {
		child = s.data.MaintenanceReceipts[childID]
		if child.AgentID != agentID || child.Fingerprint != "handbook:"+parentID || child.EstimatedTokens != estimated || child.ParentReceiptID != parentID {
			return MaintenanceReservation{}, fmt.Errorf("handbook receipt parent differs")
		}
		// A retry must preserve a running/completed/recovery state.
		return MaintenanceReservation{Receipt: cloneMaintenanceReceipt(child), Claimed: false}, nil
	} else {
		child = reservation.Receipt
	}
	child.ParentReceiptID = parentID
	child.OccurrenceLocalDate = parent.OccurrenceLocalDate
	child.OccurrenceScheduleRevision = parent.OccurrenceScheduleRevision
	child.PhaseState = MaintenancePhasePrepared
	s.data.MaintenanceReceipts[childID] = child
	parent.HandbookReceiptID = childID
	parent.PhaseState = MaintenancePhasePrepared
	s.data.MaintenanceReceipts[parentID] = parent
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceReceipts[parentID] = oldParent
		if childExists {
			s.data.MaintenanceReceipts[childID] = oldChild
		} else {
			delete(s.data.MaintenanceReceipts, childID)
		}
		return MaintenanceReservation{}, err
	}
	reservation.Receipt = cloneMaintenanceReceipt(child)
	reservation.Claimed = !childExists && reservation.Claimed
	return reservation, nil
}

// MarkHandbookRunning claims a prepared child receipt for exactly one worker.
func (s *Store) MarkHandbookRunning(agentID, childID, sessionID string) (MaintenanceReservation, error) {
	agentID, childID, sessionID = strings.TrimSpace(agentID), strings.TrimSpace(childID), strings.TrimSpace(sessionID)
	if agentID == "" || childID == "" || sessionID == "" {
		return MaintenanceReservation{}, fmt.Errorf("invalid handbook claim")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	child, ok := s.data.MaintenanceReceipts[childID]
	if !ok || child.AgentID != agentID {
		return MaintenanceReservation{}, ErrNotFound
	}
	if child.PhaseState != MaintenancePhasePrepared || child.Status != "pending" {
		return MaintenanceReservation{Receipt: cloneMaintenanceReceipt(child), Claimed: false}, nil
	}
	if child.ParentReceiptID == "" {
		return MaintenanceReservation{}, fmt.Errorf("handbook parent is missing")
	}
	parent, ok := s.data.MaintenanceReceipts[child.ParentReceiptID]
	if !ok || parent.AgentID != agentID || parent.HandbookReceiptID != childID || parent.Status != "settled" || parent.Unknown {
		return MaintenanceReservation{}, fmt.Errorf("handbook parent is not valid")
	}
	p, ok := s.data.Profiles[agentID]
	if !ok || !p.Enabled || !p.MaintenanceEnabled {
		return MaintenanceReservation{}, ErrNotRunnable
	}
	u := s.data.Usage[agentID]
	if u.Unknown || u.UnknownTokens > 0 {
		return MaintenanceReservation{}, ErrUsageUnknown
	}
	if p.MaintenanceTokenBudget > 0 && (u.MaintenanceTokens > p.MaintenanceTokenBudget || child.EstimatedTokens > p.MaintenanceTokenBudget-u.MaintenanceTokens) {
		return MaintenanceReservation{}, fmt.Errorf("maintenance budget exhausted")
	}
	if p.TotalTokenBudget > 0 {
		total, good := s.totalUsageLocked(agentID, u)
		if !good || total > p.TotalTokenBudget {
			return MaintenanceReservation{}, fmt.Errorf("total token budget exhausted")
		}
	}
	oldChild, oldParent := child, parent
	if child.SessionID != "" && child.SessionID != sessionID {
		return MaintenanceReservation{}, fmt.Errorf("handbook session already bound")
	}
	child.SessionID = sessionID
	child.PhaseState = MaintenancePhaseRunning
	parent.PhaseState = MaintenancePhaseRunning
	s.data.MaintenanceReceipts[childID], s.data.MaintenanceReceipts[child.ParentReceiptID] = child, parent
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceReceipts[childID], s.data.MaintenanceReceipts[child.ParentReceiptID] = oldChild, oldParent
		return MaintenanceReservation{}, err
	}
	return MaintenanceReservation{Receipt: cloneMaintenanceReceipt(child), Claimed: true}, nil
}

// SettleHandbook records handbook usage and its bounded result atomically with
// the parent phase transition. It never modifies the parent's memory usage.
func (s *Store) SettleHandbook(agentID, childID string, used int64, unknown bool, result json.RawMessage, state string, now time.Time) (MaintenanceReceipt, error) {
	agentID, childID = strings.TrimSpace(agentID), strings.TrimSpace(childID)
	if agentID == "" || childID == "" || used < 0 || now.IsZero() || (state != MaintenancePhaseComplete && state != MaintenancePhaseRecovery) {
		return MaintenanceReceipt{}, fmt.Errorf("invalid handbook settlement")
	}
	if state == MaintenancePhaseComplete && unknown {
		return MaintenanceReceipt{}, fmt.Errorf("completed handbook usage must be known")
	}
	normalized, err := normalizeMaintenanceEvidence(result)
	if err != nil {
		return MaintenanceReceipt{}, fmt.Errorf("invalid handbook result: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	child, ok := s.data.MaintenanceReceipts[childID]
	if !ok || child.AgentID != agentID {
		return MaintenanceReceipt{}, ErrNotFound
	}
	if child.Status == "settled" {
		oldResult, normalizeErr := normalizeMaintenanceEvidence(child.ResultJSON)
		if child.PhaseState == state && child.UsedTokens == used && child.Unknown == unknown && normalizeErr == nil && bytes.Equal(oldResult, normalized) {
			return cloneMaintenanceReceipt(child), nil
		}
		return MaintenanceReceipt{}, ErrConflict
	}
	if child.Status != "pending" || child.PhaseState != MaintenancePhaseRunning || child.ParentReceiptID == "" {
		return MaintenanceReceipt{}, fmt.Errorf("handbook receipt is not running")
	}
	parent, ok := s.data.MaintenanceReceipts[child.ParentReceiptID]
	if !ok || parent.AgentID != agentID || parent.HandbookReceiptID != childID {
		return MaintenanceReceipt{}, fmt.Errorf("handbook parent is missing")
	}
	oldChild, oldParent := child, parent
	oldUsage, oldUsageExists := s.data.Usage[agentID]
	if _, err := s.settleMaintenanceLocked(agentID, childID, used, unknown, now, false); err != nil {
		return MaintenanceReceipt{}, err
	}
	child = s.data.MaintenanceReceipts[childID]
	child.PhaseState, child.ResultJSON = state, append([]byte(nil), normalized...)
	parent.PhaseState = state
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
