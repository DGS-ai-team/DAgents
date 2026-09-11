package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// ExecuteCondition is the public Manager seam for scheduler condition checks.
// It executes only after the normal Agent gate and lifecycle fence; ASK is
// persisted as the ordinary HITL approval and returned as awaiting.
func (m *Manager) ExecuteCondition(ctx context.Context, req triggers.ConditionRequest) (triggers.ConditionResult, error) {
	if m == nil || strings.TrimSpace(req.AgentID) == "" || strings.TrimSpace(req.SessionID) == "" {
		return triggers.ConditionResult{}, fmt.Errorf("condition identity is required")
	}
	leaseCtx, release, acquired, err := m.TryAcquireMaintenanceContext(ctx, req.AgentID)
	if err != nil {
		return triggers.ConditionResult{}, err
	}
	if !acquired {
		return triggers.ConditionResult{}, fmt.Errorf("agent is busy")
	}
	defer release()
	r := m.getRuntime(req.SessionID)
	if r == nil || r.agentID != req.AgentID || r.orch == nil {
		return triggers.ConditionResult{}, fmt.Errorf("condition session unavailable")
	}
	r.conditionCompletion = m.conditionCompletion
	if err := r.lifecycleBeginInputTurn(turn.TurnSourceSideEffect); err != nil {
		return triggers.ConditionResult{}, err
	}
	var prepared turn.ConditionResult
	var callID, executionID string
	outcome, history := r.runTurnStepWithSideEffects(leaseCtx, false, func(stepCtx context.Context, _ *[]llm.Message) turn.StepOutcome {
		var call llm.ToolCall
		prepared, call, err = r.orch.PrepareConditionWithDelivery(stepCtx, r.session.ID, req.TriggerID, req.DeliveryID, req.Command)
		if err != nil {
			return turn.StepOutcome{ConditionHandled: true, Err: err}
		}
		if r.conditionValidator != nil {
			if err := r.conditionValidator(stepCtx, turn.ConditionApprovalMetadata{TriggerID: req.TriggerID, DeliveryID: req.DeliveryID, AgentID: req.AgentID, SessionID: req.SessionID, TriggerRevision: req.Revision, Occurrence: req.Occurrence}); err != nil {
				return turn.StepOutcome{ConditionHandled: true, Err: err}
			}
		}
		callID, executionID = call.ID, call.ID+"-execution"
		state := r.turnCoordinator.Snapshot()
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{Type: turn.CommandAssistantReceived, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, HasTools: true, ToolBatchID: state.StepID + "-condition-batch", AssistantMessageID: callID + "-assistant", At: time.Now().UTC(), Reason: "condition"}); err != nil {
			return turn.StepOutcome{ConditionHandled: true, Err: err}
		}
		if _, err := r.lifecycleDispatchErr(turn.TurnCommand{Type: turn.CommandToolCallRecorded, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, ToolCallID: callID, ToolExecutionID: executionID, ToolName: call.Function.Name, Arguments: json.RawMessage(call.Function.Arguments), At: time.Now().UTC()}); err != nil {
			return turn.StepOutcome{ConditionHandled: true, Err: err}
		}
		if prepared.Action == policy.ActionAuto {
			state = r.turnCoordinator.Snapshot()
			if _, err := r.lifecycleDispatchErr(turn.TurnCommand{Type: turn.CommandToolExecutionStarted, SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID, Generation: state.Generation, ToolCallID: callID, ToolExecutionID: executionID, At: time.Now().UTC()}); err != nil {
				return turn.StepOutcome{ConditionHandled: true, Err: err}
			}
			prepared, err = r.orch.ExecutePreparedCondition(stepCtx, r.session.ID, call)
		}
		return turn.StepOutcome{ConditionHandled: true, ConditionMatched: prepared.Matched, ConditionToolCallID: callID, ConditionExecutionID: executionID, ConditionResult: prepared.ResultContent, Err: err}
	})
	if prepared.Action == policy.ActionRequireApproval && outcome.Err == nil {
		if err := r.lifecycleCancel(); err != nil {
			return triggers.ConditionResult{}, fmt.Errorf("cancel condition preparation: %w", err)
		}
		if err := r.requestConditionApproval(ConditionApprovalRequest{Metadata: turn.ConditionApprovalMetadata{TriggerID: req.TriggerID, DeliveryID: req.DeliveryID, AgentID: req.AgentID, SessionID: req.SessionID, TriggerRevision: req.Revision, Occurrence: req.Occurrence}, Command: req.Command}); err != nil {
			return triggers.ConditionResult{}, err
		}
		return triggers.ConditionResult{Status: triggers.ConditionAwaitingApproval}, nil
	}
	if err := r.lifecycleAfterCondition(outcome, history, time.Now().UTC()); err != nil {
		return triggers.ConditionResult{}, err
	}
	r.finishTurnIdle(outcome)
	r.persist(context.Background())
	if outcome.Err != nil {
		return triggers.ConditionResult{}, outcome.Err
	}
	if outcome.ConditionMatched {
		return triggers.ConditionResult{Status: triggers.ConditionMatched}, nil
	}
	return triggers.ConditionResult{Status: triggers.ConditionNotMatched}, nil
}
