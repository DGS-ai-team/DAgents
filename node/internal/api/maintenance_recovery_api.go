package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
)

type maintenanceRecoveryRequest struct {
	LocalDate        string `json:"local_date"`
	ScheduleRevision int64  `json:"schedule_revision"`
	Token            string `json:"token"`
}

func (s *Server) recoveryCoordinates(r *http.Request, id string) (string, int64, error) {
	query := r.URL.Query()
	dateRaw, hasDate := query["local_date"]
	revRaw, hasRevision := query["schedule_revision"]
	if hasDate != hasRevision {
		return "", 0, fmt.Errorf("local_date and schedule_revision must be supplied together")
	}
	date := strings.TrimSpace(r.URL.Query().Get("local_date"))
	revision := int64(0)
	if hasRevision {
		var parseErr error
		revision, parseErr = strconv.ParseInt(strings.TrimSpace(r.URL.Query().Get("schedule_revision")), 10, 64)
		if parseErr != nil || revision <= 0 || len(revRaw) == 0 || len(dateRaw) == 0 || date == "" {
			return "", 0, fmt.Errorf("invalid recovery coordinates")
		}
	}
	if !hasDate && !hasRevision {
		status, err := s.goalStore.MaintenanceScheduleStatus(id, time.Now().UTC())
		if err != nil || status.Last == nil {
			return "", 0, fmt.Errorf("recovery coordinates are required")
		}
		if date == "" {
			date = status.Last.LocalDate
		}
		if revision <= 0 {
			revision = status.Last.ScheduleRevision
		}
	}
	return date, revision, nil
}

func (s *Server) handleGetMaintenanceRecovery(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	date, revision, err := s.recoveryCoordinates(r, id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_recovery_coordinates", err.Error(), nil)
		return
	}
	snapshot, err := s.goalStore.GetMaintenanceRecoverySnapshot(id, date, revision)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "recovery_not_found", err.Error(), nil)
		return
	}
	type receiptSummary struct {
		ID      string `json:"id"`
		Stage   string `json:"stage"`
		Status  string `json:"status"`
		Known   bool   `json:"known"`
		Session string `json:"session_id,omitempty"`
	}
	receipts := make([]receiptSummary, 0, len(snapshot.Receipts))
	for _, entry := range snapshot.Receipts {
		receipts = append(receipts, receiptSummary{ID: entry.ReceiptID, Stage: entry.Receipt.PhaseState, Status: entry.Receipt.Status, Known: !entry.Receipt.Unknown, Session: entry.Receipt.SessionID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"local_date": date, "schedule_revision": revision, "stage": snapshot.Occurrence.Status, "known": !snapshot.Usage.Unknown && snapshot.Usage.UnknownTokens == 0, "used_tokens": snapshot.Usage.MaintenanceTokens, "blocked_reason": snapshot.Occurrence.Error, "token": snapshot.Token, "receipts": receipts})
}

func (s *Server) handlePostMaintenanceRecovery(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	if s.goalStore == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "goals_unavailable", "goal store unavailable", nil)
		return
	}
	var in maintenanceRecoveryRequest
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.LocalDate) == "" || in.ScheduleRevision <= 0 || strings.TrimSpace(in.Token) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_recovery_request", "local_date, schedule_revision and token are required", nil)
		return
	}
	snapshot, err := s.goalStore.GetMaintenanceRecoverySnapshot(id, in.LocalDate, in.ScheduleRevision)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "recovery_not_found", err.Error(), nil)
		return
	}
	if snapshot.Token != strings.TrimSpace(in.Token) {
		writeAPIError(w, http.StatusConflict, "recovery_token_stale", "recovery snapshot has changed", nil)
		return
	}
	for _, entry := range snapshot.Receipts {
		if entry.Receipt.ParentReceiptID != "" || entry.Receipt.Status != "settled" {
			continue
		}
		if entry.Receipt.Unknown || len(entry.Receipt.CandidateJSON) == 0 || len(entry.Receipt.EvidenceJSON) == 0 {
			writeAPIError(w, http.StatusConflict, "recovery_evidence_invalid", "memory receipt is not recoverable", map[string]any{"receipt_id": entry.ReceiptID})
			return
		}
	}
	rec, err := s.agents.Get(r.Context(), id)
	if err != nil || rec == nil {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent not found", nil)
		return
	}
	if s.sessions == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "recovery_unavailable", "session manager unavailable", nil)
		return
	}
	leaseCtx, release, acquired, err := s.sessions.TryAcquireMaintenanceContext(r.Context(), id)
	if err != nil || !acquired {
		writeAPIError(w, http.StatusConflict, "agent_busy", "agent has pending chat work", nil)
		return
	}
	defer release()
	recoveryCtx, cancel := context.WithTimeout(leaseCtx, 30*time.Second)
	defer cancel()
	ms, err := s.openAgentMemoryService(id, rec)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "recovery_memory_unavailable", err.Error(), nil)
		return
	}
	defer ms.Close()
	cursor, err := ms.GetMaintenanceCursor(recoveryCtx)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "recovery_memory_unavailable", err.Error(), nil)
		return
	}
	for _, entry := range snapshot.Receipts {
		if entry.Receipt.ParentReceiptID != "" {
			continue
		}
		var candidates []memory.Candidate
		if json.Unmarshal(entry.Receipt.CandidateJSON, &candidates) != nil {
			writeAPIError(w, http.StatusConflict, "recovery_memory_mismatch", "memory candidates are invalid", map[string]any{"receipt_id": entry.ReceiptID})
			return
		}
		rawCandidates, marshalErr := json.Marshal(candidates)
		digest := sha256.Sum256(rawCandidates)
		candidateFingerprint := hex.EncodeToString(digest[:])
		op, opErr := ms.GetMaintenanceOperation(recoveryCtx, entry.ReceiptID)
		if opErr != nil || op.Status != "applied" || op.AgentID != id || op.Scope != memory.ScopeAgent || op.NextCursor != entry.Receipt.NextCursor || op.SourceFingerprint != entry.Receipt.Fingerprint || op.CandidateFingerprint != candidateFingerprint || cursor.Sequence < entry.Receipt.NextCursor || marshalErr != nil {
			writeAPIError(w, http.StatusConflict, "recovery_memory_mismatch", "memory operation does not match receipt", map[string]any{"receipt_id": entry.ReceiptID})
			return
		}
	}
	if err := recoveryCtx.Err(); err != nil {
		writeAPIError(w, http.StatusConflict, "recovery_cancelled", err.Error(), nil)
		return
	}
	if _, err := s.goalStore.ResumeMaintenanceOccurrence(id, in.LocalDate, in.ScheduleRevision, in.Token, time.Now().UTC()); err != nil {
		writeAPIError(w, http.StatusConflict, "recovery_rejected", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "stage": goals.MaintenanceOccurrencePending, "local_date": in.LocalDate, "schedule_revision": in.ScheduleRevision})
}
