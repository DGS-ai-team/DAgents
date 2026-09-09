package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

type MaintenanceUsageStore interface {
	BeginMaintenance(string, string, string, int64, time.Time) (goals.MaintenanceReservation, error)
	SettleMaintenance(string, string, int64, bool, time.Time) (goals.AgentUsage, error)
	SaveMaintenanceResult(string, string, json.RawMessage, int64, int64, bool) error
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

// RunOnce processes the earliest completed durable snapshot after cursor.
// Empty or unreadable input never spends LLM tokens; unreadable input leaves
// the cursor unchanged so recovery cannot skip a gap.
func (r *MaintenanceRunner) RunOnce(ctx context.Context, agentID string, cursor MaintenanceCursor) (nextSeq int64, retErr error) {
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
	if err != nil || !changed {
		if err != nil {
			return cursor.Sequence, err
		}
		return cursor.Sequence, nil
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
	if saveErr := r.Usage.SaveMaintenanceResult(agentID, receiptID, raw, seq, used, unknown); saveErr != nil {
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
