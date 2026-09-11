package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// ConditionResult is the policy outcome of one Agent-owned condition check.
// ApprovalRequired is deliberately returned to the caller: a scheduler must
// persist an actionable recovery/approval state rather than treating ASK as
// allow or as a silent false.
type ConditionResult struct {
	Matched        bool
	Action         policy.Action
	ApprovalReason string
	ResultContent  string
	Rejected       bool
}

// PrepareCondition performs the normal tool policy/hooks decision without
// opening the shell. The returned call is the exact call that must be fenced
// before ExecutePreparedCondition is invoked.
func (o *Orchestrator) PrepareCondition(ctx context.Context, sessionID, triggerID, command string) (ConditionResult, llm.ToolCall, error) {
	return o.prepareCondition(ctx, sessionID, triggerID, triggerID, command)
}

// PrepareConditionWithDelivery uses the durable delivery identity for the
// tool call, so repeated activations of one trigger cannot collide.
func (o *Orchestrator) PrepareConditionWithDelivery(ctx context.Context, sessionID, triggerID, deliveryID, command string) (ConditionResult, llm.ToolCall, error) {
	return o.prepareCondition(ctx, sessionID, triggerID, deliveryID, command)
}

func (o *Orchestrator) prepareCondition(ctx context.Context, sessionID, triggerID, deliveryID, command string) (ConditionResult, llm.ToolCall, error) {
	if o == nil || o.tools == nil {
		return ConditionResult{}, llm.ToolCall{}, fmt.Errorf("condition executor unavailable")
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(triggerID) == "" {
		return ConditionResult{}, llm.ToolCall{}, fmt.Errorf("condition identity is required")
	}
	execution, ok := ExecutionContextFromContext(ctx)
	if !ok || !execution.Valid() || execution.SessionID != sessionID {
		return ConditionResult{}, llm.ToolCall{}, fmt.Errorf("condition execution context is invalid")
	}
	args, err := json.Marshal(map[string]string{"command": strings.TrimSpace(command)})
	if err != nil {
		return ConditionResult{}, llm.ToolCall{}, fmt.Errorf("marshal condition arguments: %w", err)
	}
	callIdentity := strings.TrimSpace(deliveryID)
	if callIdentity == "" {
		callIdentity = strings.TrimSpace(triggerID)
	}
	tc := llm.ToolCall{ID: "condition-" + callIdentity, Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: string(args)}}
	decision := o.decideToolBeforeEach(ctx, sessionID, &[]llm.Message{}, tc)
	return ConditionResult{Action: decision.Action, ApprovalReason: decision.ApprovalReason}, tc, nil
}

// ExecutePreparedCondition runs the already policy-checked call. Callers
// must persist the execution-start fact before invoking this method.
func (o *Orchestrator) ExecutePreparedCondition(ctx context.Context, sessionID string, tc llm.ToolCall) (ConditionResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	content, rejected, _, err := o.invokeTool(callCtx, sessionID, tc, nil)
	if err != nil {
		return ConditionResult{ResultContent: content}, err
	}
	if rejected {
		return ConditionResult{ResultContent: content}, fmt.Errorf("condition execution rejected")
	}
	matched, err := parseConditionResult(content)
	return ConditionResult{Matched: matched, Action: policy.ActionAuto, ResultContent: content}, err
}

// BuildConditionApprovalPending creates the normal execute_tool-shaped HITL
// item while retaining trigger identity outside the UI payload.
func BuildConditionApprovalPending(req ConditionApprovalMetadata, command string) *PendingHITL {
	return &PendingHITL{Items: []PendingHITLItem{{
		ToolCall: llm.ToolCall{ID: "condition-" + strings.TrimSpace(req.DeliveryID), Type: "function", Function: llm.ToolCallFunction{
			Name: "bash_run", Arguments: mustJSONConditionArgs(command),
		}},
		ConditionApproval: &req,
	}}}
}

func mustJSONConditionArgs(command string) string {
	b, _ := json.Marshal(map[string]string{"command": strings.TrimSpace(command)})
	return string(b)
}

// ExecuteCondition evaluates and, when allowed, runs a condition through the
// same policy/hooks boundary as a model-issued bash_run. It is intentionally a
// narrow Agent-bound seam; triggers never receive a Registry or execute a
// process directly.
func (o *Orchestrator) ExecuteCondition(ctx context.Context, sessionID, triggerID, command string) (ConditionResult, error) {
	result, tc, err := o.PrepareCondition(ctx, sessionID, triggerID, command)
	if err != nil {
		return result, err
	}
	if result.Action == policy.ActionDeny || result.Action == policy.ActionRequireApproval {
		return result, nil
	}
	if result.Action != policy.ActionAuto {
		return result, fmt.Errorf("condition policy action is not executable: %s", result.Action)
	}
	executed, err := o.ExecutePreparedCondition(ctx, sessionID, tc)
	executed.Action = result.Action
	executed.ApprovalReason = result.ApprovalReason
	return executed, err
}

// ExecuteConditionApproval consumes one ordinary approval plan for a pending
// condition. The caller owns durable interaction CAS/one-shot consumption;
// this method only performs the live policy recheck and execution edge.
func (o *Orchestrator) ExecuteConditionApproval(ctx context.Context, sessionID string, pending *PendingHITL, resumeValue map[string]any) (ConditionResult, error) {
	if o == nil || o.tools == nil {
		return ConditionResult{}, fmt.Errorf("condition executor unavailable")
	}
	if pending == nil || len(pending.Items) != 1 || pending.Items[0].ConditionApproval == nil {
		return ConditionResult{}, fmt.Errorf("condition approval is missing")
	}
	item := pending.Items[0]
	meta := item.ConditionApproval
	if strings.TrimSpace(sessionID) == "" || meta.AgentID == "" || meta.AgentID != o.agentID {
		return ConditionResult{}, fmt.Errorf("condition approval identity is invalid")
	}
	execution, ok := ExecutionContextFromContext(ctx)
	if !ok || !execution.Valid() || execution.SessionID != sessionID {
		return ConditionResult{}, fmt.Errorf("condition approval execution context is invalid")
	}
	if meta.ArgsDigest != "" && meta.ArgsDigest != Digest(item.ToolCall.Function.Arguments) {
		return ConditionResult{}, fmt.Errorf("condition approval arguments changed")
	}
	plan, err := hitl.ParseApprovalResume(resumeValue, []string{item.ToolCall.ID})
	if err != nil {
		return ConditionResult{Action: policy.ActionRequireApproval, ApprovalReason: err.Error()}, nil
	}
	if _, approved := plan.Approved[item.ToolCall.ID]; !approved {
		return ConditionResult{Action: policy.ActionRequireApproval, ApprovalReason: "user rejected condition", Rejected: true}, nil
	}
	history := make([]llm.Message, 0, 1)
	decision := o.decideToolBeforeEach(ctx, sessionID, &history, item.ToolCall)
	if decision.Action == policy.ActionDeny {
		return ConditionResult{Action: decision.Action, ApprovalReason: decision.ApprovalReason}, nil
	}
	if decision.Action != policy.ActionAuto && decision.Action != policy.ActionRequireApproval {
		return ConditionResult{Action: decision.Action, ApprovalReason: decision.ApprovalReason}, fmt.Errorf("condition approval policy action is not executable: %s", decision.Action)
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	content, rejected, _, execErr := o.invokeTool(callCtx, sessionID, item.ToolCall, &plan)
	if execErr != nil {
		return ConditionResult{Action: decision.Action, ResultContent: content}, execErr
	}
	if rejected {
		return ConditionResult{Action: decision.Action, ResultContent: content}, fmt.Errorf("condition execution rejected")
	}
	matched, parseErr := parseConditionResult(content)
	return ConditionResult{Matched: matched, Action: decision.Action, ApprovalReason: decision.ApprovalReason, ResultContent: content}, parseErr
}

func parseConditionResult(content string) (bool, error) {
	header := strings.SplitN(content, "--- STDOUT ---", 2)[0]
	if !strings.Contains(header, "[BASH_RESULT]") {
		return false, fmt.Errorf("condition result missing bash header")
	}
	status := ""
	exitCode := ""
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "status=") {
			status = strings.TrimSpace(strings.TrimPrefix(line, "status="))
		}
		if strings.HasPrefix(line, "exit_code=") {
			exitCode = strings.TrimSpace(strings.TrimPrefix(line, "exit_code="))
		}
	}
	switch status {
	case "TIMED_OUT", "CANCELLED":
		return false, fmt.Errorf("condition execution %s", strings.ToLower(status))
	case "SUCCEEDED", "FAILED":
	default:
		return false, fmt.Errorf("condition result has unknown status")
	}
	if exitCode == "" {
		return false, fmt.Errorf("condition result missing exit code")
	}
	code, err := strconv.Atoi(exitCode)
	if err != nil {
		return false, fmt.Errorf("condition result has invalid exit code: %w", err)
	}
	if status == "SUCCEEDED" && code == 0 {
		return true, nil
	}
	if status == "FAILED" && code != 0 {
		return false, nil
	}
	return false, fmt.Errorf("condition result status and exit code disagree")
}
