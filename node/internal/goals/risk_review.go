package goals

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type RiskReviewReceipt struct {
	AgentID, OperationID, RequestID, Fingerprint string
	EstimatedTokens, UsedTokens                  int64
	Unknown                                      bool
	Status                                       string
	CreatedAt, UpdatedAt                         time.Time
}
type RiskReviewReservation struct {
	Receipt RiskReviewReceipt
	Claimed bool
}
type RiskObservationRecord struct {
	AgentID, OperationID, RequestID, ToolName, ArgsDigest, PolicyAction, Level, Reason, Recommendation string
	RiskUnknown, UsageUnknown                                                                          bool
	Error                                                                                              string
	CreatedAt                                                                                          time.Time
}

// ListRiskObservations returns digest-only observations for one Agent.
func (s *Store) ListRiskObservations(agentID string, limit int) []RiskObservationRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agentID = strings.TrimSpace(agentID)
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	out := make([]RiskObservationRecord, 0, limit)
	for _, r := range s.data.RiskObservations {
		if r.AgentID == agentID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].OperationID < out[j].OperationID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) riskReservedLocked(agentID string) (int64, bool) {
	var total int64
	for _, r := range s.data.RiskReviewReceipts {
		if r.AgentID != agentID {
			continue
		}
		reserve := int64(0)
		if r.Status == "pending" {
			reserve = r.EstimatedTokens
		} else if r.Status == "settled" && r.Unknown && r.EstimatedTokens > r.UsedTokens {
			reserve = r.EstimatedTokens - r.UsedTokens
		}
		if reserve > math.MaxInt64-total {
			return 0, false
		}
		total += reserve
	}
	return total, true
}

func (s *Store) maintenanceReservedLocked(agentID string) (int64, bool) {
	var total int64
	for _, r := range s.data.MaintenanceReceipts {
		if r.AgentID == agentID && r.Status == "pending" {
			if r.EstimatedTokens > math.MaxInt64-total {
				return 0, false
			}
			total += r.EstimatedTokens
		}
	}
	return total, true
}

// totalUsageLocked includes settled risk usage and all pending reservations.
// The bool is false when any addition would overflow int64.
func (s *Store) totalUsageLocked(agentID string, u AgentUsage) (int64, bool) {
	total := u.BusinessTokens
	if u.MaintenanceTokens > math.MaxInt64-total {
		return 0, false
	}
	total += u.MaintenanceTokens
	if u.RiskReviewTokens > math.MaxInt64-total {
		return 0, false
	}
	total += u.RiskReviewTokens
	reserved, ok := s.riskReservedLocked(agentID)
	if !ok {
		return 0, false
	}
	maintenanceReserved, ok := s.maintenanceReservedLocked(agentID)
	if !ok || maintenanceReserved > math.MaxInt64-total {
		return 0, false
	}
	total += maintenanceReserved
	if reserved > math.MaxInt64-total {
		return 0, false
	}
	return total + reserved, true
}

func DigestRiskArgs(args []byte) string { h := sha256.Sum256(args); return hex.EncodeToString(h[:]) }

func (s *Store) BeginRiskReview(agentID, operationID, requestID, fingerprint string, estimated int64, now time.Time) (RiskReviewReservation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agentID, operationID, requestID, fingerprint = strings.TrimSpace(agentID), strings.TrimSpace(operationID), strings.TrimSpace(requestID), strings.TrimSpace(fingerprint)
	if agentID == "" || operationID == "" || requestID == "" || fingerprint == "" || estimated < 0 || now.IsZero() {
		return RiskReviewReservation{}, fmt.Errorf("invalid risk review reservation")
	}
	if old, ok := s.data.RiskReviewReceipts[operationID]; ok {
		if old.AgentID != agentID || old.RequestID != requestID || old.Fingerprint != fingerprint || old.EstimatedTokens != estimated {
			return RiskReviewReservation{}, fmt.Errorf("%w: risk review receipt differs", ErrConflict)
		}
		return RiskReviewReservation{Receipt: old}, nil
	}
	for _, old := range s.data.RiskReviewReceipts {
		if old.AgentID == agentID && old.RequestID == requestID {
			if old.Fingerprint != fingerprint || old.EstimatedTokens != estimated {
				return RiskReviewReservation{}, fmt.Errorf("%w: risk request differs", ErrConflict)
			}
			return RiskReviewReservation{Receipt: old}, nil
		}
	}
	p, ok := s.data.Profiles[agentID]
	if !ok || !p.Enabled {
		return RiskReviewReservation{}, ErrNotRunnable
	}
	u := s.data.Usage[agentID]
	if u.RiskReviewUnknown {
		return RiskReviewReservation{}, ErrUsageUnknown
	}
	for _, r := range s.data.RiskReviewReceipts {
		if r.AgentID == agentID && (r.Status == "pending" || r.Unknown) {
			return RiskReviewReservation{}, fmt.Errorf("%w: risk review unresolved", ErrConflict)
		}
	}
	used, ok := s.totalUsageLocked(agentID, u)
	if !ok || estimated > math.MaxInt64-used {
		return RiskReviewReservation{}, fmt.Errorf("risk review usage overflow")
	}
	if p.TotalTokenBudget > 0 && (used > p.TotalTokenBudget || estimated > p.TotalTokenBudget-used) {
		return RiskReviewReservation{}, fmt.Errorf("total token budget exhausted")
	}
	now = now.UTC()
	r := RiskReviewReceipt{AgentID: agentID, OperationID: operationID, RequestID: requestID, Fingerprint: fingerprint, EstimatedTokens: estimated, Status: "pending", CreatedAt: now, UpdatedAt: now}
	s.data.RiskReviewReceipts[operationID] = r
	if err := s.saveLocked(); err != nil {
		delete(s.data.RiskReviewReceipts, operationID)
		return RiskReviewReservation{}, err
	}
	return RiskReviewReservation{Receipt: r, Claimed: true}, nil
}

func (s *Store) SettleRiskReview(agentID, operationID string, used int64, unknown bool, now time.Time) (AgentUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.data.RiskReviewReceipts[strings.TrimSpace(operationID)]
	if !ok || r.AgentID != strings.TrimSpace(agentID) {
		return AgentUsage{}, ErrNotFound
	}
	if used < 0 {
		return AgentUsage{}, fmt.Errorf("invalid risk review settlement")
	}
	if r.Status == "settled" {
		if r.UsedTokens != used || r.Unknown != unknown {
			return AgentUsage{}, ErrConflict
		}
		return s.data.Usage[r.AgentID], nil
	}
	u, old := s.data.Usage[r.AgentID], s.data.Usage[r.AgentID]
	oldReceipt := r
	charged := used
	if charged > math.MaxInt64-u.RiskReviewTokens {
		return AgentUsage{}, fmt.Errorf("usage overflow")
	}
	u.AgentID, u.RiskReviewTokens, u.UpdatedAt = r.AgentID, u.RiskReviewTokens+charged, now.UTC()
	u.RiskReviewUnknown = unknown
	r.Status, r.UsedTokens, r.Unknown, r.UpdatedAt = "settled", used, unknown, now.UTC()
	operationID = strings.TrimSpace(operationID)
	s.data.Usage[r.AgentID], s.data.RiskReviewReceipts[operationID] = u, r
	if err := s.saveLocked(); err != nil {
		s.data.Usage[r.AgentID] = old
		s.data.RiskReviewReceipts[operationID] = oldReceipt
		return AgentUsage{}, err
	}
	return u, nil
}

// SettleRiskReviewWithObservation commits usage and its digest-only audit in
// one goals-store snapshot, so neither can succeed without the other.
func (s *Store) SettleRiskReviewWithObservation(agentID, operationID string, used int64, unknown bool, observation RiskObservationRecord, now time.Time) (AgentUsage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agentID, operationID = strings.TrimSpace(agentID), strings.TrimSpace(operationID)
	r, ok := s.data.RiskReviewReceipts[operationID]
	if !ok || r.AgentID != agentID || used < 0 || observation.AgentID != agentID || observation.OperationID != operationID || observation.RequestID != r.RequestID {
		return AgentUsage{}, ErrNotFound
	}
	key := operationID + ":" + r.RequestID
	oldObs, obsExists := s.data.RiskObservations[key]
	if obsExists && oldObs.ArgsDigest != observation.ArgsDigest {
		return AgentUsage{}, ErrConflict
	}
	if r.Status == "settled" {
		if r.UsedTokens != used || r.Unknown != unknown {
			return AgentUsage{}, ErrConflict
		}
		return s.data.Usage[agentID], nil
	}
	u, oldUsage, oldReceipt := s.data.Usage[agentID], s.data.Usage[agentID], r
	charged := used
	if charged > math.MaxInt64-u.RiskReviewTokens {
		return AgentUsage{}, fmt.Errorf("usage overflow")
	}
	u.AgentID, u.RiskReviewTokens, u.UpdatedAt, u.RiskReviewUnknown = agentID, u.RiskReviewTokens+charged, now.UTC(), unknown
	r.Status, r.UsedTokens, r.Unknown, r.UpdatedAt = "settled", used, unknown, now.UTC()
	observation.CreatedAt = observation.CreatedAt.UTC()
	if len(observation.Reason) > 2048 {
		observation.Reason = observation.Reason[:2048]
	}
	if len(observation.Recommendation) > 2048 {
		observation.Recommendation = observation.Recommendation[:2048]
	}
	s.data.Usage[agentID], s.data.RiskReviewReceipts[operationID], s.data.RiskObservations[key] = u, r, observation
	if err := s.saveLocked(); err != nil {
		s.data.Usage[agentID], s.data.RiskReviewReceipts[operationID] = oldUsage, oldReceipt
		if obsExists {
			s.data.RiskObservations[key] = oldObs
		} else {
			delete(s.data.RiskObservations, key)
		}
		return AgentUsage{}, err
	}
	return u, nil
}

func (s *Store) SaveRiskObservation(r RiskObservationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(r.AgentID) == "" || strings.TrimSpace(r.OperationID) == "" || strings.TrimSpace(r.RequestID) == "" {
		return fmt.Errorf("invalid risk observation")
	}
	key := r.OperationID + ":" + r.RequestID
	receipt, ok := s.data.RiskReviewReceipts[r.OperationID]
	if !ok || receipt.AgentID != r.AgentID || receipt.RequestID != r.RequestID {
		return ErrNotFound
	}
	if existing, exists := s.data.RiskObservations[key]; exists && existing.ArgsDigest != r.ArgsDigest {
		return ErrConflict
	}
	if len(r.Reason) > 2048 {
		r.Reason = r.Reason[:2048]
	}
	if len(r.Recommendation) > 2048 {
		r.Recommendation = r.Recommendation[:2048]
	}
	r.CreatedAt = r.CreatedAt.UTC()
	old, existed := s.data.RiskObservations[key]
	s.data.RiskObservations[key] = r
	if err := s.saveLocked(); err != nil {
		if existed {
			s.data.RiskObservations[key] = old
		} else {
			delete(s.data.RiskObservations, key)
		}
		return err
	}
	return nil
}
