package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type maintenanceReconcileRequest struct {
	LocalDate        string `json:"local_date"`
	ScheduleRevision int64  `json:"schedule_revision"`
	Token            string `json:"token"`
	ReceiptID        string `json:"receipt_id"`
}

// handlePostMaintenanceReconcile settles a durable handbook child only after
// replaying its persisted turn, filesystem history, and memory parent. It does
// not resume the occurrence or issue another model request.
func (s *Server) handlePostMaintenanceReconcile(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("agent_id"))
	if !s.autoAgent(id, r, w) {
		return
	}
	var in maintenanceReconcileRequest
	if err := decodeJSON(r, &in); err != nil || strings.TrimSpace(in.LocalDate) == "" || in.ScheduleRevision <= 0 || strings.TrimSpace(in.Token) == "" || strings.TrimSpace(in.ReceiptID) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_reconcile_request", "local_date, schedule_revision, token and receipt_id are required", nil)
		return
	}
	in.LocalDate, in.Token, in.ReceiptID = strings.TrimSpace(in.LocalDate), strings.TrimSpace(in.Token), strings.TrimSpace(in.ReceiptID)
	if s.goalStore == nil || s.agents == nil || s.store == nil || s.sessions == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "reconcile_unavailable", "maintenance reconciliation unavailable", nil)
		return
	}
	snapshot, err := s.goalStore.GetMaintenanceRecoverySnapshot(id, strings.TrimSpace(in.LocalDate), in.ScheduleRevision)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "recovery_not_found", err.Error(), nil)
		return
	}
	child, parent, parentID, ok := maintenanceChildForReconcile(snapshot, id, in.ReceiptID)
	if !ok {
		writeAPIError(w, http.StatusConflict, "reconcile_receipt_invalid", "maintenance handbook receipt is invalid", nil)
		return
	}
	// Exact settled retries are durable and must remain idempotent even if the
	// old handbook root or journal was subsequently removed.
	if child.Status == "settled" && !child.Unknown && child.PhaseState == goals.MaintenancePhaseComplete && parent.Status == "settled" && !parent.Unknown && parent.PhaseState == goals.MaintenancePhaseComplete && child.ReconciliationToken == in.Token {
		writeJSON(w, http.StatusOK, map[string]any{"status": "reconciled", "stage": goals.MaintenancePhaseComplete, "local_date": strings.TrimSpace(in.LocalDate), "schedule_revision": in.ScheduleRevision, "receipt_id": strings.TrimSpace(in.ReceiptID), "used_tokens": child.UsedTokens})
		return
	}
	if snapshot.Token != strings.TrimSpace(in.Token) {
		writeAPIError(w, http.StatusConflict, "recovery_token_stale", "recovery snapshot has changed", nil)
		return
	}

	rec, err := s.agents.Get(r.Context(), id)
	if err != nil || rec == nil {
		s.writeAgentNotFound(w, id)
		return
	}
	leaseCtx, release, acquired, err := s.sessions.TryAcquireMaintenanceContext(r.Context(), id)
	if err != nil || !acquired {
		writeAPIError(w, http.StatusConflict, "agent_busy", "agent has pending work", nil)
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(leaseCtx, 30*time.Second)
	defer cancel()

	root := strings.TrimSpace(child.HandbookRoot)
	if root == "" || !filepath.IsAbs(root) {
		writeAPIError(w, http.StatusConflict, "handbook_unavailable", "handbook root is missing or not absolute", nil)
		return
	}
	handbook, err := handbookfs.OpenReadOnly(root)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "handbook_unavailable", err.Error(), nil)
		return
	}
	prov := handbookfs.Provenance{MaintenanceReceiptID: in.ReceiptID, SessionID: child.SessionID, TurnID: child.TurnID}
	entries, handbookDigest, err := handbook.ReadSourceSnapshot(ctx, prov)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "handbook_evidence_invalid", err.Error(), nil)
		return
	}
	events, err := s.store.ListTurnEventsForTurnBounded(ctx, child.SessionID, child.TurnID, 4096, 4*1024*1024)
	if err != nil {
		writeAPIError(w, http.StatusConflict, "turn_evidence_invalid", err.Error(), nil)
		return
	}
	reconciled, err := turn.ReconcileMaintenanceTurn(events, id, child.SessionID, child.TurnID)
	if err != nil || !reconciled.Completed || !reconciled.UsageKnown || reconciled.Usage.TotalTokens < 0 {
		if err == nil {
			err = fmt.Errorf("turn is not completed with known usage")
		}
		writeAPIError(w, http.StatusConflict, "turn_not_reconcilable", err.Error(), nil)
		return
	}
	if len(entries) == 0 && maintenanceTurnContainsHandbookMutation(events) {
		writeAPIError(w, http.StatusConflict, "handbook_evidence_invalid", "missing mutation evidence in handbook history", nil)
		return
	}
	ms, err := s.openAgentMemoryService(id, rec)
	if err != nil || ms == nil {
		writeAPIError(w, http.StatusConflict, "memory_unavailable", fmt.Sprint(err), nil)
		return
	}
	defer ms.Close()
	if err := verifyMaintenanceParent(ctx, ms, parentID, parent, id); err != nil {
		writeAPIError(w, http.StatusConflict, "memory_evidence_invalid", err.Error(), nil)
		return
	}
	eventsJSON, _ := json.Marshal(events)
	eventsSum := sha256.Sum256(eventsJSON)
	evidence, _ := json.Marshal(map[string]any{"turn_id": child.TurnID, "session_id": child.SessionID, "events_digest": hex.EncodeToString(eventsSum[:]), "handbook_digest": handbookDigest, "file_count": len(entries), "usage_known": true})
	if err := ctx.Err(); err != nil {
		writeAPIError(w, http.StatusConflict, "reconcile_cancelled", err.Error(), nil)
		return
	}
	result, err := s.goalStore.ReconcileHandbookCompletion(id, in.LocalDate, in.ScheduleRevision, in.Token, in.ReceiptID, child.SessionID, child.TurnID, int64(reconciled.Usage.TotalTokens), evidence, time.Now().UTC())
	if err != nil {
		writeAPIError(w, http.StatusConflict, "reconcile_rejected", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "reconciled", "stage": goals.MaintenancePhaseComplete, "local_date": in.LocalDate, "schedule_revision": in.ScheduleRevision, "receipt_id": in.ReceiptID, "used_tokens": result.UsedTokens})
}

func maintenanceTurnContainsHandbookMutation(events []turn.TurnEventEnvelope) bool {
	for _, event := range events {
		var payload map[string]json.RawMessage
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		var toolName string
		if json.Unmarshal(payload["tool_name"], &toolName) != nil {
			continue
		}
		if toolName == "write_file" || toolName == "search_replace" {
			return true
		}
	}
	return false
}

func maintenanceChildForReconcile(snapshot goals.MaintenanceRecoverySnapshot, id, receiptID string) (goals.MaintenanceReceipt, goals.MaintenanceReceipt, string, bool) {
	var child, parent goals.MaintenanceReceipt
	for _, e := range snapshot.Receipts {
		if e.ReceiptID == strings.TrimSpace(receiptID) {
			child = e.Receipt
		}
	}
	if child.AgentID != id || child.ParentReceiptID == "" || child.OccurrenceLocalDate != snapshot.Occurrence.LocalDate || child.OccurrenceScheduleRevision != snapshot.Occurrence.ScheduleRevision {
		return goals.MaintenanceReceipt{}, goals.MaintenanceReceipt{}, "", false
	}
	parentID := ""
	for _, e := range snapshot.Receipts {
		if e.ReceiptID == child.ParentReceiptID {
			parent = e.Receipt
			parentID = e.ReceiptID
		}
	}
	if parent.AgentID != id || parent.HandbookReceiptID != receiptID || parent.ParentReceiptID != "" {
		return goals.MaintenanceReceipt{}, goals.MaintenanceReceipt{}, "", false
	}
	return child, parent, parentID, true
}

func verifyMaintenanceParent(ctx context.Context, ms *memory.LocalService, parentID string, parent goals.MaintenanceReceipt, agentID string) error {
	if parent.Status != "settled" || parent.Unknown || len(parent.CandidateJSON) == 0 || len(parent.EvidenceJSON) == 0 || parent.NextCursor <= 0 {
		return fmt.Errorf("memory parent is not settled and known")
	}
	var candidates []memory.Candidate
	if err := json.Unmarshal(parent.CandidateJSON, &candidates); err != nil {
		return fmt.Errorf("invalid memory candidates: %w", err)
	}
	raw, _ := json.Marshal(candidates)
	sum := sha256.Sum256(raw)
	op, err := ms.GetMaintenanceOperation(ctx, parentID)
	cursor, cursorErr := ms.GetMaintenanceCursor(ctx)
	if err != nil || cursorErr != nil || op.Status != "applied" || op.AgentID != agentID || op.Scope != memory.ScopeAgent || op.NextCursor != parent.NextCursor || cursor.Sequence < parent.NextCursor || op.SourceFingerprint != parent.Fingerprint || op.CandidateFingerprint != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("memory parent operation does not match receipt")
	}
	return nil
}
