package turn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	clihitl "github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func (o *Orchestrator) processToolCalls(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	calls []llm.ToolCall,
) (*PendingHITL, string, error) {
	if o.toolBudgetCheck != nil {
		allowed, reason := o.toolBudgetCheck(sessionID)
		if !allowed {
			if strings.TrimSpace(reason) == "" {
				reason = "turn_budget"
			}
			o.appendMissingToolResponses(sessionID, history, calls,
				"ERROR: turn budget exhausted",
				map[string]any{"budget_exhausted": true, "budget_reason": reason})
			return nil, "", fmt.Errorf("%w: %s", ErrBudgetExhausted, reason)
		}
	}
	var autoCalls []llm.ToolCall
	var approvalCalls []pendingApprovalCall
	var userInfo *llm.ToolCall
	var memoryConflicts []PendingHITLItem

	for i, tc := range calls {
		o.publishToolCall(sessionID, tc, false, i)
		o.recordToolCall(sessionID, tc.Function.Name)

		if childagent.IsTemporaryAgentTool(tc.Function.Name) {
			if o.isChildSession {
				msg := "rejected: child_forbidden"
				o.publishToolResult(sessionID, tc, msg, true, nil)
				o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
				continue
			}
			if o.childMgr == nil || !o.childMgr.Enabled() {
				output := "ERROR: child agents disabled"
				o.publishToolResult(sessionID, tc, output, true, nil)
				o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
				continue
			}
			_, cleanedArgs := tools.ParseRunInBackground(tc.Function.Arguments)
			output, err := o.childMgr.HandleParentTool(ctx, sessionID, tc.Function.Name, cleanedArgs)
			if err != nil {
				return nil, "", err
			}
			o.publishToolResult(sessionID, tc, output, strings.HasPrefix(output, "ERROR:"), nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
			continue
		}

		if tools.IsAskUserInformation(tc.Function.Name) {
			if o.isChildSession {
				msg := "rejected: ask_user_forbidden_for_child"
				o.publishToolResult(sessionID, tc, msg, true, nil)
				o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
				continue
			}
			if userInfo == nil {
				cp := tc
				userInfo = &cp
			}
			continue
		}
		if tools.IsRemember(tc.Function.Name) {
			conflictItem, err := o.executeRememberTool(ctx, sessionID, history, tc)
			if err != nil {
				return nil, "", err
			}
			if conflictItem != nil {
				memoryConflicts = append(memoryConflicts, *conflictItem)
			}
			continue
		}
		if tools.IsSkillTool(tc.Function.Name) {
			if err := o.executeSkillTool(sessionID, history, tc); err != nil {
				return nil, "", err
			}
			continue
		}
		decision := o.decideToolBeforeEach(ctx, sessionID, history, tc)
		switch decision.Action {
		case policy.ActionDeny:
			msg := hooks.ToolDenyMessage(decision)
			o.publishToolResult(sessionID, tc, msg, true, nil)
			o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, msg))
		case policy.ActionRequireApproval:
			item := pendingApprovalCall{tc: tc}
			if decision.ApprovalSubtype == hooks.ApprovalSubtypeDuplicateToolCall && decision.DuplicateMeta != nil {
				meta := *decision.DuplicateMeta
				item.duplicateMeta = &meta
			}
			approvalCalls = append(approvalCalls, item)
		default:
			autoCalls = append(autoCalls, tc)
		}
	}

	if err := o.executeAutoBatch(ctx, sessionID, history, autoCalls, nil); err != nil {
		return nil, "", err
	}

	var pendingItems []PendingHITLItem
	if userInfo != nil {
		pendingItems = append(pendingItems, PendingHITLItem{ToolCall: *userInfo})
	}
	pendingItems = append(pendingItems, memoryConflicts...)
	for _, item := range approvalCalls {
		pendingItem := PendingHITLItem{ToolCall: item.tc}
		if item.duplicateMeta != nil {
			meta := *item.duplicateMeta
			pendingItem.DuplicateMeta = &meta
		}
		pendingItems = append(pendingItems, pendingItem)
	}
	if len(pendingItems) == 0 {
		return nil, "", nil
	}
	message, sseItems := buildHITLRequiredPayload(pendingItems)
	// Complete the pause hook before publishing the resumable event. Clients
	// may resume immediately after observing hitl_required; publishing first
	// would let the resume path mutate the shared history while this turn is
	// still applying hook effects to it.
	o.runHITLBeforePausePhase(ctx, sessionID, history, "awaiting_hitl")
	o.publishHITLRequired(sessionID, newShortID("hitl-"), message, sseItems)
	return pendingFromItems(pendingItems), "awaiting_hitl", nil
}

func (o *Orchestrator) decideToolBeforeEach(ctx context.Context, sessionID string, history *[]llm.Message, tc llm.ToolCall) hooks.ToolBeforeEachResult {
	if o.executionGuard != nil {
		return o.executionGuard.Check(ctx, sessionID, history, tc)
	}
	return o.evaluateToolBeforeEach(ctx, sessionID, history, tc)
}

func (o *Orchestrator) evaluateToolBeforeEach(ctx context.Context, sessionID string, history *[]llm.Message, tc llm.ToolCall) hooks.ToolBeforeEachResult {
	var decision hooks.ToolBeforeEachResult
	if o.toolHooks == nil {
		action := o.policy.DecideTool(tc.Function.Name, parseJSONArgs(tc.Function.Arguments))
		mode := policy.ModeRule
		if o.policy != nil {
			mode = o.policy.ToolApprovalMode(tc.Function.Name)
		}
		decision = hooks.ToolBeforeEachResult{Action: action, ToolMode: mode}
	} else {
		hc := hooks.BuildToolBeforeEachContext(hooks.ToolBeforeEachInput{
			SessionID:    sessionID,
			ToolName:     tc.Function.Name,
			ToolArgs:     parseJSONArgs(tc.Function.Arguments),
			RawArguments: tc.Function.Arguments,
		})
		out, err := o.runPhase(ctx, hooks.PhaseToolBeforeEach, hc, sessionID, history, "")
		if err != nil {
			return hooks.DefaultToolBeforeEachResult()
		}
		decision = hooks.ToolBeforeEachDecisionFrom(out)
	}
	return o.mergeToolPreflightDecision(ctx, tc, decision)
}

func (o *Orchestrator) mergeToolPreflightDecision(ctx context.Context, tc llm.ToolCall, decision hooks.ToolBeforeEachResult) hooks.ToolBeforeEachResult {
	preflight, ok := o.tools.(tools.ToolPreflight)
	if !ok {
		return decision
	}
	live, applicable := preflight.PreflightTool(ctx, tc.Function.Name, parseJSONArgs(tc.Function.Arguments))
	if !applicable {
		return decision
	}
	if live.Action == policy.ActionDeny {
		decision.Action = policy.ActionDeny
		if strings.TrimSpace(live.ApprovalReason) != "" {
			decision.ApprovalReason = live.ApprovalReason
		}
		return decision
	}
	if live.Action == policy.ActionRequireApproval && decision.Action != policy.ActionDeny {
		decision.Action = policy.ActionRequireApproval
		if strings.TrimSpace(decision.ApprovalReason) == "" {
			decision.ApprovalReason = live.ApprovalReason
		}
	}
	return decision
}

func (o *Orchestrator) recordToolExecutionSuccess(tc llm.ToolCall, content string, rejected bool) {
	if rejected || o.toolExecLog == nil {
		return
	}
	fp := hooks.ToolArgsFingerprint(tc.Function.Name, tc.Function.Arguments)
	o.toolExecLog.RecordSuccess(tc.Function.Name, fp, tc.ID, content)
}

func (o *Orchestrator) executeSkillTool(sessionID string, history *[]llm.Message, tc llm.ToolCall) error {
	catalog := o.skillAccess.Catalog
	if catalog == nil || !catalog.Enabled() {
		output := "ERROR: skills 功能已禁用"
		o.publishToolResult(sessionID, tc, output, true, nil)
		o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
		return nil
	}
	loaded := []skills.LoadedSkill{}
	if o.skillAccess.Get != nil {
		loaded = o.skillAccess.Get()
	}
	var payload map[string]any
	_, cleanedArgs := tools.ParseRunInBackground(tc.Function.Arguments)
	_ = json.Unmarshal([]byte(cleanedArgs), &payload)
	var output string
	switch tc.Function.Name {
	case "load_skills":
		names := stringSliceField(payload, "skill_names")
		loaded = catalog.SetLoadedSkills(names)
		body, _ := json.Marshal(map[string]any{
			"action":        "set_loaded_skills",
			"loaded_skills": loaded,
		})
		output = string(body)
	case "unload_skills":
		names := stringSliceField(payload, "skill_names")
		loaded = catalog.UnloadSkills(loaded, names)
		body, _ := json.Marshal(map[string]any{
			"action":        "unload_skills",
			"loaded_skills": loaded,
		})
		output = string(body)
	case "clear_skills":
		loaded = nil
		body, _ := json.Marshal(map[string]any{
			"action":        "clear_skills",
			"loaded_skills": []skills.LoadedSkill{},
		})
		output = string(body)
	default:
		output = "ERROR: unknown skill tool"
	}
	if o.skillAccess.Set != nil {
		o.skillAccess.Set(loaded)
	}
	rejected := strings.HasPrefix(output, "ERROR:")
	o.publishToolResult(sessionID, tc, output, rejected, nil)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(tc.ID, tc.Function.Name, output))
	return nil
}

func stringSliceField(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch t := raw.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return t
	default:
		return nil
	}
}

// executeAutoBatch 并行执行一批免审批工具（对齐 Python gather）。
// 每个工具完成后立刻推送 tool_result SSE，便于 UI 反映并行进度；
// Wait 后按原始 tool_calls 顺序写入 history（不重复推送 SSE）。
func (o *Orchestrator) executeAutoBatch(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	autoCalls []llm.ToolCall,
	plan *clihitl.ApprovalPlan,
) error {
	if len(autoCalls) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(autoCalls) == 1 {
		return o.executeTool(ctx, sessionID, history, autoCalls[0], plan)
	}
	type batchItem struct {
		tc           llm.ToolCall
		rejected     bool
		extra        map[string]any
		forClient    string
		forHistory   string
		spillPath    string
		lifecycleErr error
	}
	results := make([]batchItem, len(autoCalls))
	var wg sync.WaitGroup
	// split/publish 串行化：AfterEach hook 与 hub seq 需避免并发重入。
	var publishMu sync.Mutex
	for i := range autoCalls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tc := autoCalls[idx]
			var lifecycleErr error
			content := ""
			rejected := false
			var extra map[string]any
			if err := o.emitToolExecutionStarted(ctx, sessionID, tc); err != nil {
				lifecycleErr = fmt.Errorf("record tool execution start: %w", err)
				content = "ERROR: " + lifecycleErr.Error()
				rejected = true
			} else {
				content, rejected, extra, lifecycleErr = o.invokeToolWithRetries(ctx, sessionID, tc, plan)
				finishErr := o.emitToolExecutionFinished(ctx, sessionID, tc, rejected || strings.HasPrefix(strings.TrimSpace(content), "ERROR:"))
				if lifecycleErr == nil {
					lifecycleErr = finishErr
				}
			}
			publishMu.Lock()
			forClient, forHistory, spillPath := o.splitToolResult(sessionID, tc, content)
			o.publishToolResult(sessionID, tc, forClient, rejected, extra)
			publishMu.Unlock()
			results[idx] = batchItem{
				tc:           tc,
				rejected:     rejected,
				extra:        extra,
				forClient:    forClient,
				forHistory:   forHistory,
				spillPath:    spillPath,
				lifecycleErr: lifecycleErr,
			}
		}(i)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	var lifecycleErr error
	for _, item := range results {
		o.persistToolResult(sessionID, history, item.tc, item.forClient, item.forHistory, item.spillPath, item.rejected)
		if lifecycleErr == nil && item.lifecycleErr != nil {
			lifecycleErr = item.lifecycleErr
		}
	}
	if lifecycleErr != nil {
		return lifecycleErr
	}
	return nil
}

func (o *Orchestrator) commitToolResult(
	sessionID string,
	history *[]llm.Message,
	tc llm.ToolCall,
	content string,
	rejected bool,
	extra map[string]any,
) {
	forClient, forHistory, spillPath := o.splitToolResult(sessionID, tc, content)
	o.publishToolResult(sessionID, tc, forClient, rejected, extra)
	o.persistToolResult(sessionID, history, tc, forClient, forHistory, spillPath, rejected)
}

// persistToolResult 将已推送（或即将仅落盘）的工具结果写入 metrics / history。
func (o *Orchestrator) persistToolResult(
	sessionID string,
	history *[]llm.Message,
	tc llm.ToolCall,
	forClient, forHistory, spillPath string,
	rejected bool,
) {
	o.recordToolResult(sessionID, tc.Function.Name, tc.Function.Arguments, forHistory, spillPath, rejected)
	o.recordToolExecutionSuccess(tc, forClient, rejected)
	o.appendHistory(sessionID, history, llm.ToolResultMessage(
		tc.ID,
		tc.Function.Name,
		forHistory,
	))
	if !rejected {
		o.maybeAppendToolVisionUserMessage(sessionID, history, tc)
	}
}

func (o *Orchestrator) invokeTool(ctx context.Context, sessionID string, tc llm.ToolCall, plan *clihitl.ApprovalPlan) (content string, rejected bool, extra map[string]any, execErr error) {
	runInBackground, cleanedArgs := tools.ParseRunInBackground(tc.Function.Arguments)
	if tools.IsBackgroundJobTool(tc.Function.Name) {
		runInBackground = false
	}
	toolCtx := tools.WithToolCallID(tools.WithSession(ctx, sessionID), tc.ID)
	if plan != nil && plan.IsApproved(tc.ID) {
		toolCtx = tools.WithApprovalID(toolCtx, tc.ID)
	}
	if target := resolveTriggerSessionTarget(tc, plan); target != "" {
		toolCtx = tools.WithTriggerSessionTarget(toolCtx, target)
	}

	var output string
	if runInBackground {
		output, execErr = o.tools.StartBackground(toolCtx, sessionID, tc.Function.Name, tc.ID, cleanedArgs)
	} else {
		output, execErr = o.tools.Execute(toolCtx, tc.Function.Name, cleanedArgs)
		extra = mergeToolResultExtra(o.tools.TakeBashCompressStatsForCall(tc.ID), o.tools.TakeToolResultMediaForCall(tc.ID))
	}
	if execErr != nil {
		return execErr.Error(), true, nil, execErr
	}
	return output, false, extra, nil
}

// invokeToolWithRetries retries only an executor-approved, read-like tool
// after a transient execution error. The original ToolCall ID is retained and
// the lifecycle sink records a ToolExecutionRetrying fact for each retry; a
// generic mutation such as bash is therefore never replayed automatically.
func (o *Orchestrator) invokeToolWithRetries(ctx context.Context, sessionID string, tc llm.ToolCall, plan *clihitl.ApprovalPlan) (string, bool, map[string]any, error) {
	content, rejected, extra, execErr := o.invokeTool(ctx, sessionID, tc, plan)
	for retry := 0; execErr != nil && o.canRetryTool(sessionID, tc.Function.Name, execErr, retry); retry++ {
		if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type:            CommandToolExecutionRetrying,
			ToolCallID:      tc.ID,
			ToolExecutionID: tc.ID + "-execution",
			ToolName:        tc.Function.Name,
			ErrorKind:       toolErrorKind(execErr),
			Reason:          "transient_tool_error",
			At:              time.Now().UTC(),
		}); err != nil {
			return content, rejected, extra, fmt.Errorf("record tool retry: %w", err)
		}
		content, rejected, extra, execErr = o.invokeTool(ctx, sessionID, tc, plan)
	}
	return content, rejected, extra, nil
}

func (o *Orchestrator) canRetryTool(sessionID, toolName string, execErr error, retries int) bool {
	if o == nil || execErr == nil || retries >= o.toolRetryLimit || !isTransientToolError(execErr) {
		return false
	}
	policy, ok := o.tools.(tools.ToolRetryPolicy)
	if !ok || !policy.ToolRetryAllowed(toolName) {
		return false
	}
	if o.toolRetryCheck != nil {
		allowed, _ := o.toolRetryCheck(sessionID)
		if !allowed {
			return false
		}
	}
	return true
}

func isTransientToolError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{"timeout", "timed out", "temporar", "unavailable", "connection reset", "broken pipe", "eof", "transport"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func toolErrorKind(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "transient_tool_error"
}

func (o *Orchestrator) executeTool(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	tc llm.ToolCall,
	plan *clihitl.ApprovalPlan,
) error {
	o.recordToolCall(sessionID, tc.Function.Name)
	if err := o.emitToolExecutionStarted(ctx, sessionID, tc); err != nil {
		content := "ERROR: " + err.Error()
		o.commitToolResult(sessionID, history, tc, content, true, nil)
		return fmt.Errorf("record tool execution start: %w", err)
	}
	content, rejected, extra, lifecycleErr := o.invokeToolWithRetries(ctx, sessionID, tc, plan)
	finishErr := o.emitToolExecutionFinished(ctx, sessionID, tc, rejected || strings.HasPrefix(strings.TrimSpace(content), "ERROR:"))
	o.commitToolResult(sessionID, history, tc, content, rejected, extra)
	if lifecycleErr != nil {
		return lifecycleErr
	}
	return finishErr
}

func (o *Orchestrator) emitToolExecutionStarted(ctx context.Context, sessionID string, tc llm.ToolCall) error {
	if strings.TrimSpace(tc.ID) == "" {
		return nil
	}
	return o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
		Type:            CommandToolExecutionStarted,
		ToolCallID:      tc.ID,
		ToolExecutionID: tc.ID + "-execution",
		ToolName:        tc.Function.Name,
		At:              time.Now().UTC(),
		Reason:          "tool_execution_started",
	})
}

func (o *Orchestrator) emitToolExecutionFinished(ctx context.Context, sessionID string, tc llm.ToolCall, failed bool) error {
	if strings.TrimSpace(tc.ID) == "" {
		return nil
	}
	commandType := CommandToolExecutionCompleted
	status := ToolExecutionStatusSucceeded
	errorKind := ""
	if failed {
		commandType = CommandToolExecutionFailed
		status = ToolExecutionStatusFailed
		errorKind = "tool_execution_error"
	}
	return o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
		Type:            commandType,
		ToolCallID:      tc.ID,
		ToolExecutionID: tc.ID + "-execution",
		ExecutionStatus: status,
		ErrorKind:       errorKind,
		At:              time.Now().UTC(),
		Reason:          "tool_execution_finished",
	})
}

func resolveTriggerSessionTarget(tc llm.ToolCall, plan *clihitl.ApprovalPlan) string {
	name := strings.ToLower(strings.TrimSpace(tc.Function.Name))
	if name != "trigger_create" {
		return ""
	}
	if plan == nil {
		return clihitl.TriggerSessionSame
	}
	target := plan.TriggerSessionTarget(tc.ID)
	if target != "" {
		return target
	}
	if plan.IsApproved(tc.ID) {
		return clihitl.TriggerSessionSame
	}
	return ""
}

func buildUserInformationPayload(tc llm.ToolCall) (question string, uiArgs map[string]any) {
	parsed := parseJSONArgs(tc.Function.Arguments)
	question = strings.TrimSpace(fmt.Sprint(parsed["question"]))
	if question == "" {
		question = "请补充信息"
	}
	uiArgs = map[string]any{
		"tool_call_id": tc.ID,
		"tool_name":    tc.Function.Name,
		"question":     question,
		"required":     true,
	}
	if opts, ok := parsed["options"]; ok {
		uiArgs["options"] = opts
	}
	if v, ok := parsed["allow_multiple"]; ok {
		uiArgs["allow_multiple"] = v
	}
	if v, ok := parsed["placeholder"]; ok {
		uiArgs["placeholder"] = v
	}
	if v, ok := parsed["required"]; ok {
		uiArgs["required"] = v
	}
	return question, uiArgs
}

func parseJSONArgs(argsJSON string) map[string]any {
	var m map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &m)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func newShortID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func mergeToolResultExtra(parts ...map[string]any) map[string]any {
	var out map[string]any
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		if out == nil {
			out = make(map[string]any, len(part))
		}
		for k, v := range part {
			out[k] = v
		}
	}
	return out
}
