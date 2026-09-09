package goals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type MaintenanceReceipt struct {
	AgentID                    string          `json:"agent_id"`
	Fingerprint                string          `json:"fingerprint"`
	ProfileRevision            int64           `json:"profile_revision"`
	EstimatedTokens            int64           `json:"estimated_tokens"`
	UsedTokens                 int64           `json:"used_tokens"`
	Unknown                    bool            `json:"unknown"`
	Status                     string          `json:"status"`
	CreatedAt                  time.Time       `json:"created_at"`
	UpdatedAt                  time.Time       `json:"updated_at"`
	CandidateJSON              json.RawMessage `json:"candidate_json,omitempty"`
	NextCursor                 int64           `json:"next_cursor,omitempty"`
	SessionID                  string          `json:"session_id,omitempty"`
	TurnID                     string          `json:"turn_id,omitempty"`
	AttemptedAt                time.Time       `json:"attempted_at,omitempty"`
	HandbookRoot               string          `json:"handbook_root,omitempty"`
	EvidenceJSON               json.RawMessage `json:"evidence_json,omitempty"`
	ParentReceiptID            string          `json:"parent_receipt_id,omitempty"`
	HandbookReceiptID          string          `json:"handbook_receipt_id,omitempty"`
	OccurrenceLocalDate        string          `json:"occurrence_local_date,omitempty"`
	OccurrenceScheduleRevision int64           `json:"occurrence_schedule_revision,omitempty"`
	PhaseState                 string          `json:"phase_state,omitempty"`
	ResultJSON                 json.RawMessage `json:"result_json,omitempty"`
	ReconciliationEvidence     json.RawMessage `json:"reconciliation_evidence,omitempty"`
	ReconciledAt               time.Time       `json:"reconciled_at,omitempty"`
	ReconciliationToken        string          `json:"reconciliation_token,omitempty"`
}

const maxMaintenanceEvidenceBytes = 64 * 1024

func (s *Store) SetMaintenanceSessionID(agentID, receiptID, sessionID string) error {
	agentID, receiptID, sessionID = strings.TrimSpace(agentID), strings.TrimSpace(receiptID), strings.TrimSpace(sessionID)
	if agentID == "" || receiptID == "" || sessionID == "" {
		return fmt.Errorf("maintenance session identity is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.data.MaintenanceReceipts[receiptID]
	if !ok || r.AgentID != strings.TrimSpace(agentID) {
		return ErrNotFound
	}
	if r.SessionID != "" && r.SessionID != sessionID {
		return fmt.Errorf("maintenance receipt session already bound")
	}
	old := r
	r.SessionID = strings.TrimSpace(sessionID)
	s.data.MaintenanceReceipts[receiptID] = r
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceReceipts[receiptID] = old
		return err
	}
	return nil
}

func (s *Store) IsMaintenanceSession(agentID, sessionID string) bool {
	agentID, sessionID = strings.TrimSpace(agentID), strings.TrimSpace(sessionID)
	if agentID == "" || sessionID == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.data.MaintenanceReceipts {
		if r.AgentID == agentID && r.SessionID == sessionID {
			return true
		}
	}
	return false
}

type MaintenanceReservation struct {
	Receipt MaintenanceReceipt
	// Claimed is true only for the caller that created a new pending receipt.
	// It is deliberately transient and is not persisted in the receipt.
	Claimed bool
}

// MaintenanceAvailableTokens returns the remaining maintenance allowance after
// all settled usage and pending risk/maintenance reservations. A zero cap means
// unlimited maintenance; disabled/unknown profiles return zero with false.
func (s *Store) MaintenanceAvailableTokens(agentID string) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agentID = strings.TrimSpace(agentID)
	p, ok := s.data.Profiles[agentID]
	if !ok || !p.Enabled {
		return 0, false
	}
	u := s.data.Usage[agentID]
	if u.Unknown || u.UnknownTokens > 0 {
		return 0, false
	}
	available := int64(math.MaxInt64)
	if p.MaintenanceTokenBudget > 0 {
		available = p.MaintenanceTokenBudget - u.MaintenanceTokens
		if available < 0 {
			available = 0
		}
	}
	if p.TotalTokenBudget > 0 {
		total, good := s.totalUsageLocked(agentID, u)
		if !good {
			return 0, false
		}
		rem := p.TotalTokenBudget - total
		if rem < available {
			available = rem
		}
		if available < 0 {
			available = 0
		}
	}
	return available, true
}

func (s *Store) SaveMaintenanceResult(agentID, receiptID string, candidates json.RawMessage, nextCursor, used int64, unknown bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveMaintenanceResultLocked(agentID, receiptID, candidates, nextCursor, used, unknown, nil, false)
}

// SaveMaintenanceResultWithEvidence durably records the memory result and the
// bounded source evidence in one transaction. Evidence is written before the
// memory cursor is advanced, allowing a later handbook phase to recover the
// exact input after a process restart.
func (s *Store) SaveMaintenanceResultWithEvidence(agentID, receiptID string, candidates, evidence json.RawMessage, nextCursor, used int64, unknown bool) error {
	if nextCursor < 0 || used < 0 {
		return fmt.Errorf("invalid maintenance evidence")
	}
	normalizedEvidence, err := normalizeMaintenanceEvidence(evidence)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveMaintenanceResultLocked(agentID, receiptID, candidates, nextCursor, used, unknown, normalizedEvidence, true)
}

func normalizeMaintenanceEvidence(evidence json.RawMessage) ([]byte, error) {
	if len(evidence) == 0 || len(evidence) > maxMaintenanceEvidenceBytes || !json.Valid(evidence) {
		return nil, fmt.Errorf("invalid maintenance evidence")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(evidence, &object) != nil {
		return nil, fmt.Errorf("invalid maintenance evidence")
	}
	if object == nil {
		return nil, fmt.Errorf("invalid maintenance evidence")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, evidence); err != nil {
		return nil, fmt.Errorf("invalid maintenance evidence: %w", err)
	}
	return compact.Bytes(), nil
}

func (s *Store) saveMaintenanceResultLocked(agentID, receiptID string, candidates json.RawMessage, nextCursor, used int64, unknown bool, evidence json.RawMessage, setEvidence bool) error {
	agentID, receiptID = strings.TrimSpace(agentID), strings.TrimSpace(receiptID)
	if agentID == "" || receiptID == "" || nextCursor < 0 || used < 0 {
		return fmt.Errorf("invalid maintenance result")
	}
	r, ok := s.data.MaintenanceReceipts[receiptID]
	if !ok || r.AgentID != agentID {
		return ErrNotFound
	}
	if setEvidence && r.Status != "pending" {
		return fmt.Errorf("maintenance receipt is not pending")
	}
	if setEvidence && len(r.EvidenceJSON) > 0 {
		oldEvidence, err := normalizeMaintenanceEvidence(r.EvidenceJSON)
		if err != nil || !bytes.Equal(oldEvidence, evidence) {
			return fmt.Errorf("maintenance evidence differs")
		}
	}
	old := r
	r.CandidateJSON = append([]byte(nil), candidates...)
	r.NextCursor = nextCursor
	r.UsedTokens = used
	r.Unknown = unknown
	if setEvidence {
		r.EvidenceJSON = append([]byte(nil), evidence...)
	}
	r.UpdatedAt = time.Now().UTC()
	s.data.MaintenanceReceipts[receiptID] = r
	if err := s.saveLocked(); err != nil {
		s.data.MaintenanceReceipts[receiptID] = old
		return err
	}
	return nil
}

func (s *Store) BeginMaintenance(agentID, receiptID, fingerprint string, estimated int64, now time.Time) (MaintenanceReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.beginMaintenanceLocked(agentID, receiptID, fingerprint, estimated, now, true)
}

func (s *Store) beginMaintenanceLocked(agentID, receiptID, fingerprint string, estimated int64, now time.Time, persist bool) (MaintenanceReservation, error) {
	agentID, receiptID, fingerprint = strings.TrimSpace(agentID), strings.TrimSpace(receiptID), strings.TrimSpace(fingerprint)
	if agentID == "" || receiptID == "" || fingerprint == "" || estimated < 0 || now.IsZero() {
		return MaintenanceReservation{}, fmt.Errorf("invalid maintenance reservation")
	}
	if old, ok := s.data.MaintenanceReceipts[receiptID]; ok {
		if old.AgentID != agentID || old.Fingerprint != fingerprint || old.EstimatedTokens != estimated {
			return MaintenanceReservation{}, fmt.Errorf("%w: maintenance receipt differs", ErrConflict)
		}
		return MaintenanceReservation{Receipt: old, Claimed: false}, nil
	}
	p, ok := s.data.Profiles[agentID]
	if !ok || !p.Enabled {
		return MaintenanceReservation{}, ErrNotRunnable
	}
	u := s.data.Usage[agentID]
	if u.Unknown || u.UnknownTokens > 0 {
		return MaintenanceReservation{}, ErrUsageUnknown
	}
	for _, pending := range s.data.MaintenanceReceipts {
		if pending.AgentID == agentID && pending.Status == "pending" {
			return MaintenanceReservation{}, fmt.Errorf("%w: maintenance reservation pending", ErrConflict)
		}
	}
	for _, runs := range s.data.Runs {
		for _, run := range runs {
			if run.GoalID != "" && run.FinishedAt == nil {
				if g, exists := s.data.Goals[run.GoalID]; exists && g.AgentID == agentID {
					return MaintenanceReservation{}, fmt.Errorf("%w: business run active", ErrNotRunnable)
				}
			}
		}
	}
	if p.MaintenanceTokenBudget > 0 && (u.MaintenanceTokens > p.MaintenanceTokenBudget || estimated > p.MaintenanceTokenBudget-u.MaintenanceTokens) {
		return MaintenanceReservation{}, fmt.Errorf("maintenance budget exhausted")
	}
	if p.TotalTokenBudget > 0 {
		used, ok := s.totalUsageLocked(agentID, u)
		if !ok {
			return MaintenanceReservation{}, fmt.Errorf("total token budget exhausted")
		}
		if used > p.TotalTokenBudget || estimated > p.TotalTokenBudget-used {
			return MaintenanceReservation{}, fmt.Errorf("total token budget exhausted")
		}
	}
	now = now.UTC()
	r := MaintenanceReceipt{AgentID: agentID, Fingerprint: fingerprint, ProfileRevision: p.Revision, EstimatedTokens: estimated, Status: "pending", CreatedAt: now, UpdatedAt: now}
	s.data.MaintenanceReceipts[receiptID] = r
	if persist {
		if err := s.saveLocked(); err != nil {
			delete(s.data.MaintenanceReceipts, receiptID)
			return MaintenanceReservation{}, err
		}
	}
	return MaintenanceReservation{Receipt: r, Claimed: true}, nil
}

func (s *Store) SettleMaintenance(agentID, receiptID string, used int64, unknown bool, now time.Time) (AgentUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settleMaintenanceLocked(agentID, receiptID, used, unknown, now, true)
}

func (s *Store) settleMaintenanceLocked(agentID, receiptID string, used int64, unknown bool, now time.Time, persist bool) (AgentUsage, error) {
	agentID, receiptID = strings.TrimSpace(agentID), strings.TrimSpace(receiptID)
	if agentID == "" || receiptID == "" || used < 0 {
		return AgentUsage{}, fmt.Errorf("invalid maintenance settlement")
	}
	r, ok := s.data.MaintenanceReceipts[receiptID]
	if !ok || r.AgentID != agentID {
		return AgentUsage{}, ErrNotFound
	}
	if r.Status == "settled" {
		if r.UsedTokens != used || r.Unknown != unknown {
			return AgentUsage{}, fmt.Errorf("%w: maintenance receipt differs", ErrConflict)
		}
		return s.data.Usage[agentID], nil
	}
	u, oldUsageExists := s.data.Usage[agentID]
	old := u
	oldReceipt := r
	if used > math.MaxInt64-u.MaintenanceTokens {
		return AgentUsage{}, fmt.Errorf("usage overflow")
	}
	u.AgentID = agentID
	u.MaintenanceTokens += used
	if unknown {
		u.Unknown = true
		u.UnknownReason = "maintenance usage reconciliation required"
	}
	u.UpdatedAt = now.UTC()
	r.Status, r.UsedTokens, r.Unknown, r.UpdatedAt = "settled", used, unknown, now.UTC()
	s.data.Usage[agentID], s.data.MaintenanceReceipts[receiptID] = u, r
	if persist {
		if err := s.saveLocked(); err != nil {
			if oldUsageExists {
				s.data.Usage[agentID] = old
			} else {
				delete(s.data.Usage, agentID)
			}
			s.data.MaintenanceReceipts[receiptID] = oldReceipt
			return AgentUsage{}, err
		}
	}
	return u, nil
}

func (s *Store) GetMaintenanceReceipt(receiptID string) (MaintenanceReceipt, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data.MaintenanceReceipts[strings.TrimSpace(receiptID)]
	r = cloneMaintenanceReceipt(r)
	return r, ok
}

func (s *Store) ListMaintenanceReceipts(agentID string) []MaintenanceReceipt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agentID = strings.TrimSpace(agentID)
	out := make([]MaintenanceReceipt, 0)
	for _, r := range s.data.MaintenanceReceipts {
		if r.AgentID == agentID {
			r = cloneMaintenanceReceipt(r)
			out = append(out, r)
		}
	}
	return out
}

// ListMaintenanceReceiptEntries preserves the durable receipt map identity so
// callers can resume a specific operation without reconstructing IDs.
func (s *Store) ListMaintenanceReceiptEntries(agentID string) []MaintenanceReceiptEntry {
	agentID = strings.TrimSpace(agentID)
	if s == nil || agentID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries := make([]MaintenanceReceiptEntry, 0)
	for id, receipt := range s.data.MaintenanceReceipts {
		if receipt.AgentID != agentID {
			continue
		}
		entries = append(entries, MaintenanceReceiptEntry{ReceiptID: id, Receipt: cloneMaintenanceReceipt(receipt)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ReceiptID < entries[j].ReceiptID })
	return entries
}

func cloneMaintenanceReceipt(r MaintenanceReceipt) MaintenanceReceipt {
	r.CandidateJSON = append([]byte(nil), r.CandidateJSON...)
	r.EvidenceJSON = append([]byte(nil), r.EvidenceJSON...)
	r.ResultJSON = append([]byte(nil), r.ResultJSON...)
	r.ReconciliationEvidence = append([]byte(nil), r.ReconciliationEvidence...)
	return r
}
