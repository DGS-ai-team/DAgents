package turn

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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
}

// ExecuteCondition evaluates and, when allowed, runs a condition through the
// same policy/hooks boundary as a model-issued bash_run. It is intentionally a
// narrow Agent-bound seam; triggers never receive a Registry or execute a
// process directly.
func (o *Orchestrator) ExecuteCondition(ctx context.Context, sessionID, triggerID, command string) (ConditionResult, error) {
	if o == nil || o.tools == nil {
		return ConditionResult{}, fmt.Errorf("condition executor unavailable")
	}
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(triggerID) == "" {
		return ConditionResult{}, fmt.Errorf("condition identity is required")
	}
	execution, ok := ExecutionContextFromContext(ctx)
	if !ok || !execution.Valid() || execution.SessionID != sessionID {
		return ConditionResult{}, fmt.Errorf("condition execution context is invalid")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return ConditionResult{Matched: true, Action: policy.ActionAuto}, nil
	}
	argBytes, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		return ConditionResult{}, fmt.Errorf("marshal condition arguments: %w", err)
	}
	args := string(argBytes)
	tc := llm.ToolCall{ID: "condition-" + strings.TrimSpace(triggerID), Type: "function", Function: llm.ToolCallFunction{Name: "bash_run", Arguments: args}}
	history := make([]llm.Message, 0, 1)
	decision := o.decideToolBeforeEach(ctx, sessionID, &history, tc)
	result := ConditionResult{Action: decision.Action, ApprovalReason: decision.ApprovalReason}
	if decision.Action == policy.ActionDeny || decision.Action == policy.ActionRequireApproval {
		return result, nil
	}
	if decision.Action != policy.ActionAuto {
		return result, fmt.Errorf("condition policy action is not executable: %s", decision.Action)
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	content, rejected, _, err := o.invokeTool(callCtx, sessionID, tc, nil)
	if err != nil {
		return result, err
	}
	if rejected {
		return result, fmt.Errorf("condition execution rejected")
	}
	matched, parseErr := parseConditionResult(content)
	if parseErr != nil {
		return result, parseErr
	}
	result.Matched = matched
	return result, nil
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
