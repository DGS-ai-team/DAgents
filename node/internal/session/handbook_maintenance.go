package session

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/handbookfs"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

type HandbookMaintenanceResult struct {
	Usage      turn.TurnUsage
	Changed    bool
	Unknown    bool
	UsageKnown bool
}

// HandbookTurnBinding is invoked after the durable lifecycle has assigned the
// real turn ID and before any model request is made.
type HandbookTurnBinding func(sessionID, turnID string) error

type handbookNoopFact struct {
	Version    int    `json:"version"`
	ReceiptID  string `json:"receipt_id"`
	SessionID  string `json:"session_id"`
	TurnID     string `json:"turn_id"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Path       string `json:"path"`
	Digest     string `json:"digest"`
}

func (r *runtime) recordHandbookNoop(ctx context.Context, noop tools.HandbookNoop) error {
	if r == nil || r.store == nil {
		return fmt.Errorf("handbook no-op audit store unavailable")
	}
	provenance, ok := handbookfs.ProvenanceFromContext(ctx)
	if !ok || provenance.MaintenanceReceiptID == "" || provenance.SessionID != r.session.ID || provenance.TurnID == "" {
		return fmt.Errorf("handbook no-op provenance is missing or mismatched")
	}
	if noop.ToolCallID == "" || noop.ToolName == "" || noop.Path == "" || noop.Digest == "" {
		return fmt.Errorf("handbook no-op identity is incomplete")
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.TurnID != provenance.TurnID || state.StepID == "" {
		return fmt.Errorf("handbook no-op requires an active turn step")
	}
	fact := handbookNoopFact{Version: 1, ReceiptID: provenance.MaintenanceReceiptID, SessionID: r.session.ID, TurnID: state.TurnID, ToolCallID: noop.ToolCallID, ToolName: noop.ToolName, Path: noop.Path, Digest: noop.Digest}
	payload, err := json.Marshal(fact)
	if err != nil {
		return fmt.Errorf("marshal handbook no-op audit: %w", err)
	}
	_, err = r.lifecycleDispatchErr(turn.TurnCommand{
		Type: turn.CommandExternalFactRecorded, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID,
		Generation: state.Generation, CommandID: "handbook-noop:" + state.TurnID + ":" + noop.ToolCallID,
		ExternalFactID:   "handbook-noop:" + noop.ToolCallID,
		ExternalFactKind: "handbook.noop", ToolCallID: noop.ToolCallID, ToolName: noop.ToolName,
		ResultContent: string(payload), Payload: payload, At: time.Now().UTC(), Reason: "handbook_noop_recorded",
	})
	return err
}

// RunHandbookMaintenance executes one real Agent turn while the caller holds
// the maintenance gate. It deliberately does not acquire a gate itself.
func (m *Manager) RunHandbookMaintenance(ctx context.Context, sessionID, prompt string, budget turn.TurnBudget) (HandbookMaintenanceResult, error) {
	return m.RunHandbookMaintenanceWithBinding(ctx, sessionID, prompt, budget, nil)
}

func (m *Manager) RunHandbookMaintenanceWithBinding(ctx context.Context, sessionID, prompt string, budget turn.TurnBudget, bind HandbookTurnBinding) (HandbookMaintenanceResult, error) {
	if m == nil || ctx == nil {
		return HandbookMaintenanceResult{Unknown: true}, fmt.Errorf("maintenance executor unavailable")
	}
	if budget.MaxSteps <= 0 || budget.MaxTotalTokens <= 0 {
		return HandbookMaintenanceResult{}, fmt.Errorf("handbook maintenance budget is insufficient")
	}
	m.mu.RLock()
	r := m.sessions[sessionID]
	var g *agentExecutionGate
	if r != nil {
		g = m.maintenanceGates[r.agentID]
	}
	m.mu.RUnlock()
	if r == nil || r.orch == nil {
		return HandbookMaintenanceResult{Unknown: true}, fmt.Errorf("session runtime unavailable")
	}
	if !r.autoAgent {
		return HandbookMaintenanceResult{}, fmt.Errorf("handbook maintenance requires Auto Agent")
	}
	if g == nil || !g.owns(ctx) {
		return HandbookMaintenanceResult{}, fmt.Errorf("maintenance gate is not held")
	}
	mutationBefore := uint64(0)
	if reg := r.orch.ToolRegistry(); reg != nil {
		mutationBefore = reg.HandbookMutationCount()
	}
	r.lifecycleMu.Lock()
	previousBudget := r.turnBudget
	r.turnBudget = budget
	r.lifecycleMu.Unlock()
	defer func() { r.lifecycleMu.Lock(); r.turnBudget = previousBudget; r.lifecycleMu.Unlock() }()
	user := llm.UserMessage(prompt, llm.UserNameHuman)
	// lifecycleBeginInputTurn persists the pending input as the durable turn
	// start. Handbook turns bypass the normal input queue, so publish the same
	// pending message explicitly before opening the lifecycle.
	r.mu.Lock()
	r.pendingInputMessage = &user
	r.mu.Unlock()
	beginErr := r.lifecycleBeginInputTurn(turn.TurnSourceSideEffect)
	r.mu.Lock()
	r.pendingInputMessage = nil
	r.mu.Unlock()
	if beginErr != nil {
		return HandbookMaintenanceResult{Unknown: true}, beginErr
	}
	if bind != nil {
		turnID := r.turnCoordinator.Snapshot().TurnID
		if turnID == "" {
			bindErr := fmt.Errorf("handbook turn ID unavailable")
			if cancelErr := r.lifecycleCancel(); cancelErr != nil {
				bindErr = fmt.Errorf("%w; lifecycle cancel: %v", bindErr, cancelErr)
			}
			return HandbookMaintenanceResult{UsageKnown: true}, bindErr
		}
		if bindErr := bind(r.session.ID, turnID); bindErr != nil {
			if cancelErr := r.lifecycleCancel(); cancelErr != nil {
				bindErr = fmt.Errorf("%w; lifecycle cancel: %v", bindErr, cancelErr)
			}
			return HandbookMaintenanceResult{UsageKnown: true}, bindErr
		}
		if provenance, ok := handbookfs.ProvenanceFromContext(ctx); ok {
			provenance.SessionID = r.session.ID
			provenance.TurnID = turnID
			ctx = handbookfs.WithProvenance(ctx, provenance)
		}
	}
	historyStart := r.lifecycleHistoryLength()
	maintCtx := tools.WithHandbookMaintenance(ctx)
	maintCtx = tools.WithHandbookNoopRecorder(maintCtx, func(noopCtx context.Context, noop tools.HandbookNoop) error {
		return r.recordHandbookNoop(noopCtx, noop)
	})
	outcome, history := r.runTurnStepWithSideEffects(maintCtx, false, func(stepCtx context.Context, h *[]llm.Message) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(stepCtx, r.session.ID, h, user)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil && outcome.Err == nil {
		outcome.Err = err
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(maintCtx, 0, outcome)
	r.finishTurnIdle(outcome)
	state := r.turnCoordinator.Snapshot()
	changed := false
	if reg := r.orch.ToolRegistry(); reg != nil {
		changed = reg.HandbookMutationCount() > mutationBefore
	}
	// Mutation accounting is authoritative. A successful write/search_replace
	// can still be a no-op (or an error encoded in a tool result).
	usageKnown := state.ModelUsageKnown
	result := HandbookMaintenanceResult{Usage: state.Usage, Changed: changed, Unknown: !usageKnown, UsageKnown: usageKnown}
	if outcome.Pending != nil || state.StepStatus == turn.StepStatusWaitingInteraction {
		return result, fmt.Errorf("handbook maintenance requires interaction and did not complete")
	}
	if outcome.Err != nil {
		return result, outcome.Err
	}
	return result, nil
}
