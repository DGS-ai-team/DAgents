package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

type MaintenanceUsageStore interface {
	BeginMaintenance(string, string, string, int64, time.Time) (goals.MaintenanceReservation, error)
	SettleMaintenance(string, string, int64, bool, time.Time) (goals.AgentUsage, error)
	SaveMaintenanceResult(string, string, json.RawMessage, int64, int64, bool) error
}

type maintenanceEvidenceSaver interface {
	SaveMaintenanceResultWithEvidence(string, string, json.RawMessage, json.RawMessage, int64, int64, bool) error
}

type maintenanceReceiptReader interface {
	GetMaintenanceReceipt(string) (goals.MaintenanceReceipt, bool)
}

type maintenanceReceiptLister interface {
	ListMaintenanceReceipts(string) []goals.MaintenanceReceipt
}

type MaintenanceUsageExtractor interface {
	ExtractWithUsage(context.Context, ExtractionInput) ([]Candidate, *llm.Usage, error)
}

type MaintenanceRunner struct {
	Source    DurableMaintenanceSource
	Extractor MaintenanceUsageExtractor
	Memory    *LocalService
	Usage     MaintenanceUsageStore
}

const (
	maintenanceEvidenceMaxBytes        = 16000
	maintenanceEvidenceMaxMessageBytes = 4000
)

func truncateEvidenceUTF8(value string, maxBytes int) (string, bool) {
	if maxBytes < 0 {
		maxBytes = 0
	}
	if maxBytes >= len(value) && utf8.ValidString(value) {
		return value, false
	}
	if maxBytes == 0 {
		return "", len(value) != 0
	}
	bytes := []byte(value)
	if maxBytes > len(bytes) {
		maxBytes = len(bytes)
	}
	for maxBytes > 0 && !utf8.Valid(bytes[:maxBytes]) {
		maxBytes--
	}
	return string(bytes[:maxBytes]), maxBytes < len(bytes)
}

func boundEvidenceMessages(messages []ExtractionMessage) ([]ExtractionMessage, bool) {
	bounded := make([]ExtractionMessage, 0, minInt(len(messages), maintenanceEvidenceMaxMessages))
	used := 0
	truncated := false
	for i, message := range messages {
		if i >= maintenanceEvidenceMaxMessages || used >= maintenanceEvidenceMaxBytes {
			truncated = true
			break
		}
		remaining := maintenanceEvidenceMaxBytes - used
		content, cut := truncateEvidenceUTF8(message.Content, minInt(maintenanceEvidenceMaxMessageBytes, remaining))
		// Evidence is a bounded text record. Tool calls and metadata can contain
		// arbitrarily large provider payloads, so omit them rather than copying
		// unbounded data or retaining aliases into the source snapshot.
		metadataOmitted := message.Name != "" || message.ToolCallID != "" || len(message.ToolCalls) != 0
		role := evidenceRole(message.Role)
		roleOmitted := role != message.Role
		message.Name = ""
		message.ToolCallID = ""
		message.ToolCalls = nil
		message.Role = role
		message.Content = content
		bounded = append(bounded, message)
		used += len(content)
		truncated = truncated || cut || metadataOmitted || roleOmitted
		if len(content) == 0 && len(message.Content) == 0 && len(messages[i].Content) > 0 {
			truncated = true
		}
	}
	return bounded, truncated
}

func evidenceRole(role string) string {
	switch role {
	case "system", "user", "assistant", "tool":
		return role
	default:
		return "unknown"
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MaintenanceEvidence is the bounded durable input selected for one memory
// maintenance operation. It is suitable for passing to a later handbook phase.
type MaintenanceEvidence struct {
	AgentID           string
	CursorBefore      int64
	CursorAfter       int64
	SessionID         string
	SourceFingerprint string
	Messages          []ExtractionMessage
	HasIncrement      bool
	Truncated         bool
}

const maintenanceEvidenceMaxMessages = 8

// RunOnceWithEvidence preserves RunOnce compatibility while exposing the
// exact durable snapshot selected before processing. It does not invoke the
// extractor a second time.
func (r *MaintenanceRunner) RunOnceWithEvidence(ctx context.Context, agentID string, cursor MaintenanceCursor) (int64, MaintenanceEvidence, error) {
	evidence := MaintenanceEvidence{AgentID: agentID, CursorBefore: cursor.Sequence}
	next, err := r.runOnce(ctx, agentID, cursor, &evidence)
	return next, evidence, err
}

// RunOnce processes the earliest completed durable snapshot after cursor.
// Empty or unreadable input never spends LLM tokens; unreadable input leaves
// the cursor unchanged so recovery cannot skip a gap.
func (r *MaintenanceRunner) RunOnce(ctx context.Context, agentID string, cursor MaintenanceCursor) (int64, error) {
	return r.runOnce(ctx, agentID, cursor, nil)
}

func (r *MaintenanceRunner) runOnce(ctx context.Context, agentID string, cursor MaintenanceCursor, evidence *MaintenanceEvidence) (nextSeq int64, retErr error) {
	if r == nil || r.Source == nil || r.Memory == nil || r.Usage == nil {
		return cursor.Sequence, fmt.Errorf("maintenance runner is incomplete")
	}
	if cursor.Sequence < 0 {
		return cursor.Sequence, fmt.Errorf("invalid maintenance cursor")
	}
	if err := r.reconcilePending(ctx, agentID); err != nil {
		return cursor.Sequence, err
	}
	input, seq, changed, err := ReadDurableMaintenanceInput(ctx, r.Source, agentID, cursor)
	selectedEvidence := MaintenanceEvidence{AgentID: agentID, CursorBefore: cursor.Sequence, CursorAfter: seq, SessionID: input.SessionID, SourceFingerprint: input.SourceFingerprint, HasIncrement: changed && len(input.Messages) > 0}
	selectedEvidence.Messages, selectedEvidence.Truncated = boundEvidenceMessages(input.Messages)
	if evidence != nil {
		*evidence = selectedEvidence
	}
	if err != nil || !changed {
		if err != nil {
			return cursor.Sequence, err
		}
		return cursor.Sequence, nil
	}
	if input.SkipOnly {
		op := MaintenanceOperation{OperationID: fmt.Sprintf("maintenance-skip-%s-%d", agentID, seq), AgentID: agentID, Scope: ScopeAgent, SourceFingerprint: fmt.Sprintf("skip:%s:%d", agentID, seq), ExpectedCursor: cursor.Sequence, NextCursor: seq}
		if _, applyErr := r.Memory.ApplyMaintenanceOperation(ctx, op, nil, MaintenanceCursor{AgentID: agentID, Scope: ScopeAgent, Sequence: seq, SourceFingerprint: op.SourceFingerprint}); applyErr != nil {
			return cursor.Sequence, applyErr
		}
		return seq, nil
	}
	if len(input.Messages) == 0 {
		return cursor.Sequence, nil
	}
	sum := sha256.Sum256([]byte(agentID + ":" + input.SourceFingerprint + fmt.Sprintf(":%d", seq)))
	receiptID := "maintenance-" + hex.EncodeToString(sum[:])
	if reader, ok := r.Usage.(maintenanceReceiptReader); ok {
		if receipt, exists := reader.GetMaintenanceReceipt(receiptID); exists && (receipt.Status == "settled" || (receipt.Status == "pending" && len(receipt.CandidateJSON) > 0)) {
			op, opErr := r.Memory.agent.GetMaintenanceOperation(ctx, receiptID)
			cur, curErr := r.Memory.agent.GetMaintenanceCursor(ctx, agentID)
			if opErr == nil && curErr == nil && op.Status == "applied" && cur.Sequence >= seq {
				if receipt.Status == "pending" {
					if _, settleErr := r.Usage.SettleMaintenance(agentID, receiptID, receipt.UsedTokens, receipt.Unknown, time.Now().UTC()); settleErr != nil {
						return cursor.Sequence, settleErr
					}
				}
				return seq, nil
			}
			if len(receipt.CandidateJSON) > 0 && receipt.NextCursor == seq {
				var saved []Candidate
				if json.Unmarshal(receipt.CandidateJSON, &saved) == nil {
					_, applyErr := r.Memory.ApplyMaintenanceOperation(ctx, MaintenanceOperation{OperationID: receiptID, AgentID: agentID, Scope: ScopeAgent, SourceFingerprint: input.SourceFingerprint, ExpectedCursor: cursor.Sequence, NextCursor: seq}, saved, MaintenanceCursor{AgentID: agentID, Scope: ScopeAgent, Sequence: seq, SourceFingerprint: input.SourceFingerprint})
					if applyErr != nil {
						return cursor.Sequence, applyErr
					}
					if _, settleErr := r.Usage.SettleMaintenance(agentID, receiptID, receipt.UsedTokens, receipt.Unknown, time.Now().UTC()); settleErr != nil {
						return cursor.Sequence, settleErr
					}
					return seq, nil
				}
			}
			return cursor.Sequence, fmt.Errorf("maintenance receipt %s has no recoverable memory commit", receiptID)
		}
	}
	reservation, err := r.Usage.BeginMaintenance(agentID, receiptID, input.SourceFingerprint, int64(tokens.EstimateInt(renderExtractionInput(input, 0))), time.Now().UTC())
	if err != nil {
		return cursor.Sequence, err
	}
	// A pending receipt owned by another runner is not evidence that its LLM
	// call completed. Do not guess its usage or run the call a second time.
	if !reservation.Claimed {
		return cursor.Sequence, fmt.Errorf("recovery_required: maintenance receipt %s is pending without a recoverable result", receiptID)
	}
	used, unknown := int64(0), false
	defer func() {
		if _, settleErr := r.Usage.SettleMaintenance(agentID, receiptID, used, unknown, time.Now().UTC()); settleErr != nil {
			nextSeq = cursor.Sequence
			retErr = errors.Join(retErr, settleErr)
		}
	}()
	if r.Extractor == nil {
		unknown = true
		return cursor.Sequence, fmt.Errorf("maintenance extractor is unavailable")
	}
	candidates, usage, extractErr := r.Extractor.ExtractWithUsage(ctx, input)
	if usage != nil {
		used = int64(usage.TotalTokens)
	} else {
		unknown = true
	}
	if extractErr != nil {
		return cursor.Sequence, extractErr
	}
	candidates = normalizeCandidates(input, candidates)
	raw, marshalErr := json.Marshal(candidates)
	if marshalErr != nil {
		return cursor.Sequence, marshalErr
	}
	evidenceRaw, marshalErr := json.Marshal(selectedEvidence)
	if marshalErr != nil {
		return cursor.Sequence, marshalErr
	}
	var saveErr error
	if saver, ok := r.Usage.(maintenanceEvidenceSaver); ok {
		saveErr = saver.SaveMaintenanceResultWithEvidence(agentID, receiptID, raw, evidenceRaw, seq, used, unknown)
	} else {
		saveErr = r.Usage.SaveMaintenanceResult(agentID, receiptID, raw, seq, used, unknown)
	}
	if saveErr != nil {
		// The extractor usage is still known even though the result could not be
		// written. Keep the real charge; the unrecoverable receipt blocks a
		// duplicate extraction until reconciliation repairs the result.
		return cursor.Sequence, saveErr
	}
	op := MaintenanceOperation{OperationID: receiptID, AgentID: agentID, Scope: ScopeAgent, SourceFingerprint: input.SourceFingerprint, ExpectedCursor: cursor.Sequence, NextCursor: seq}
	// Apply computes and verifies the candidate fingerprint inside its atomic
	// memory transaction; no cursor is advanced before candidate validation.
	_, err = r.Memory.ApplyMaintenanceOperation(ctx, op, candidates, MaintenanceCursor{AgentID: agentID, Scope: ScopeAgent, Sequence: seq, SourceFingerprint: input.SourceFingerprint})
	if err != nil {
		return cursor.Sequence, err
	}
	return seq, nil
}

// reconcilePending closes the durable gap after memory commit succeeded but
// receipt settlement failed. CandidateJSON is the evidence that the LLM work
// already happened, so recovery never invokes the extractor again.
func (r *MaintenanceRunner) reconcilePending(ctx context.Context, agentID string) error {
	lister, ok := r.Usage.(maintenanceReceiptLister)
	if !ok {
		return nil
	}
	for _, receipt := range lister.ListMaintenanceReceipts(agentID) {
		if receipt.Status != "pending" || len(receipt.CandidateJSON) == 0 {
			continue
		}
		var candidates []Candidate
		if err := json.Unmarshal(receipt.CandidateJSON, &candidates); err != nil {
			return fmt.Errorf("recovery_required: maintenance receipt %s candidates are invalid: %w", receipt.Fingerprint, err)
		}
		cur, err := r.Memory.agent.GetMaintenanceCursor(ctx, agentID)
		if err != nil {
			return err
		}
		op := MaintenanceOperation{OperationID: receiptIDForReceipt(receipt), AgentID: agentID, Scope: ScopeAgent, SourceFingerprint: receipt.Fingerprint, ExpectedCursor: cur.Sequence, NextCursor: receipt.NextCursor}
		if existing, getErr := r.Memory.agent.GetMaintenanceOperation(ctx, op.OperationID); getErr == nil {
			op.ExpectedCursor = existing.ExpectedCursor
			op.NextCursor = existing.NextCursor
		}
		if _, err := r.Memory.ApplyMaintenanceOperation(ctx, op, candidates, MaintenanceCursor{AgentID: agentID, Scope: ScopeAgent, Sequence: op.NextCursor, SourceFingerprint: receipt.Fingerprint}); err != nil {
			return err
		}
		if _, err := r.Usage.SettleMaintenance(agentID, op.OperationID, receipt.UsedTokens, receipt.Unknown, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}

func receiptIDForReceipt(receipt goals.MaintenanceReceipt) string {
	sum := sha256.Sum256([]byte(receipt.AgentID + ":" + receipt.Fingerprint + fmt.Sprintf(":%d", receipt.NextCursor)))
	return "maintenance-" + hex.EncodeToString(sum[:])
}
