package goals

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type MaintenanceReceipt struct {
	AgentID         string          `json:"agent_id"`
	Fingerprint     string          `json:"fingerprint"`
	ProfileRevision int64           `json:"profile_revision"`
	EstimatedTokens int64           `json:"estimated_tokens"`
	UsedTokens      int64           `json:"used_tokens"`
	Unknown         bool            `json:"unknown"`
	Status          string          `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	CandidateJSON   json.RawMessage `json:"candidate_json,omitempty"`
	NextCursor      int64           `json:"next_cursor,omitempty"`
}

type MaintenanceReservation struct {
	Receipt MaintenanceReceipt
	// Claimed is true only for the caller that created a new pending receipt.
	// It is deliberately transient and is not persisted in the receipt.
	Claimed bool
}

func (s *Store) SaveMaintenanceResult(agentID, receiptID string, candidates json.RawMessage, nextCursor, used int64, unknown bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.data.MaintenanceReceipts[receiptID]
	if !ok || r.AgentID != strings.TrimSpace(agentID) {
		return ErrNotFound
	}
	old := r
	r.CandidateJSON = append([]byte(nil), candidates...)
	r.NextCursor = nextCursor
	r.UsedTokens = used
	r.Unknown = unknown
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
	if err := s.saveLocked(); err != nil {
		delete(s.data.MaintenanceReceipts, receiptID)
		return MaintenanceReservation{}, err
	}
	return MaintenanceReservation{Receipt: r, Claimed: true}, nil
}

func (s *Store) SettleMaintenance(agentID, receiptID string, used int64, unknown bool, now time.Time) (AgentUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	if err := s.saveLocked(); err != nil {
		if oldUsageExists {
			s.data.Usage[agentID] = old
		} else {
			delete(s.data.Usage, agentID)
		}
		s.data.MaintenanceReceipts[receiptID] = oldReceipt
		return AgentUsage{}, err
	}
	return u, nil
}

func (s *Store) GetMaintenanceReceipt(receiptID string) (MaintenanceReceipt, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.data.MaintenanceReceipts[strings.TrimSpace(receiptID)]
	return r, ok
}

func (s *Store) ListMaintenanceReceipts(agentID string) []MaintenanceReceipt {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agentID = strings.TrimSpace(agentID)
	out := make([]MaintenanceReceipt, 0)
	for _, r := range s.data.MaintenanceReceipts {
		if r.AgentID == agentID {
			out = append(out, r)
		}
	}
	return out
}
