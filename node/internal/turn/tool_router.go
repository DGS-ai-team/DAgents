package turn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

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
	var autoCalls []llm.ToolCall
	var approvalCalls []pendingApprovalCall
	var userInfo *llm.ToolCall

	for i, tc := range calls {
		o.publishToolCallSSE(sessionID, tc, false, i)
		o.recordToolCall(sessionID, tc.Function.Name)

		if childagent.IsTemporaryAgentTool(tc.Function.Name) {
			if o.isChildSession {
				o.appendDeniedTool(sessionID, history, tc, "child_forbidden")
				continue
			}
			if o.childMgr == nil || !o.childMgr.Enabled() {
				output := "ERROR: child agents disabled"
				o.publishToolResult(sessionID, tc, output, true, nil)
				o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: output})
				continue
			}
			_, cleanedArgs := tools.ParseRunInBackground(tc.Function.Arguments)
			output, err := o.childMgr.HandleParentTool(ctx, sessionID, tc.Function.Name, cleanedArgs)
			if err != nil {
				return nil, "", err
			}
			o.publishToolResult(sessionID, tc, output, strings.HasPrefix(output, "ERROR:"), nil)
			o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: output})
			continue
		}

		if tools.IsAskUserInformation(tc.Function.Name) {
			if o.isChildSession {
				o.appendDeniedTool(sessionID, history, tc, "ask_user_forbidden_for_child")
				continue
			}
			if userInfo == nil {
				cp := tc
				userInfo = &cp
			}
			continue
		}
		if tools.IsSkillTool(tc.Function.Name) {
			if err := o.executeSkillTool(sessionID, history, tc); err != nil {
				return nil, "", err
			}
			continue
		}
		decision := o.decideToolBeforeEach(ctx, sessionID, tc)
		switch decision.Action {
		case policy.ActionDeny:
			o.appendDeniedTool(sessionID, history, tc, "policy_denied")
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

	if userInfo != nil {
		question, uiArgs := buildUserInformationPayload(*userInfo)
		o.hub.Publish(sessionID, o.agentID, "user_information_required", map[string]any{
			"content":               question,
			"user_information_args": uiArgs,
			"display_type":          "normal_text",
		})
		return &PendingHITL{Kind: HITLUserInformation, UserInfo: userInfo}, "awaiting_user_information", nil
	}

	if len(approvalCalls) > 0 {
		approvalID := newShortID("appr-")
		executionID := newShortID("exec-")
		toolItems := make([]map[string]any, 0, len(approvalCalls))
		hasDuplicate := false
		for _, item := range approvalCalls {
			if item.duplicateMeta != nil {
				hasDuplicate = true
			}
			toolItems = append(toolItems, buildApprovalToolItem(item.tc, item.duplicateMeta))
		}
		message := "检测到工具调用，等待用户确认后继续执行。"
		if hasDuplicate {
			message = "检测到工具调用；部分为短窗口内与上次完全相同的重复调用，请确认后再执行。"
		}
		pendingTools := make([]llm.ToolCall, 0, len(approvalCalls))
		for _, item := range approvalCalls {
			pendingTools = append(pendingTools, item.tc)
		}
		o.hub.Publish(sessionID, o.agentID, "approval_required", map[string]any{
			"approval_type": "execute_tool",
			"approval_id":   approvalID,
			"execution_id":  executionID,
			"message":       message,
			"approval_args": map[string]any{"tool_calls": toolItems},
			"display_type":  "normal_text",
		})
		return &PendingHITL{Kind: HITLApproval, ToolCalls: pendingTools}, "awaiting_tool_approval", nil
	}
	return nil, "", nil
}

func (o *Orchestrator) decideToolBeforeEach(ctx context.Context, sessionID string, tc llm.ToolCall) hooks.ToolBeforeEachResult {
	if o.toolHooks == nil {
		action := o.policy.DecideTool(tc.Function.Name, parseJSONArgs(tc.Function.Arguments))
		mode := policy.ModeRule
		if o.policy != nil {
			mode = o.policy.ToolApprovalMode(tc.Function.Name)
		}
		return hooks.ToolBeforeEachResult{Action: action, ToolMode: mode}
	}
	hc := hooks.BuildToolBeforeEachContext(hooks.ToolBeforeEachInput{
		SessionID:    sessionID,
		ToolName:     tc.Function.Name,
		ToolArgs:     parseJSONArgs(tc.Function.Arguments),
		RawArguments: tc.Function.Arguments,
	})
	out, err := o.toolHooks.RunPhase(ctx, hooks.PhaseToolBeforeEach, hc)
	if err != nil {
		return hooks.DefaultToolBeforeEachResult()
	}
	return hooks.ToolBeforeEachDecisionFrom(out)
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
		o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: output})
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
	o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: output})
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

// executeAutoBatch 并行执行一批免审批工具，按原始 tool_calls 顺序写回 history（对齐 Python gather）。
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
		tc       llm.ToolCall
		content  string
		rejected bool
		extra    map[string]any
	}
	results := make([]batchItem, len(autoCalls))
	var wg sync.WaitGroup
	for i := range autoCalls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tc := autoCalls[idx]
			content, rejected, extra := o.invokeTool(ctx, sessionID, tc, plan)
			results[idx] = batchItem{tc: tc, content: content, rejected: rejected, extra: extra}
		}(i)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, item := range results {
		forClient, forHistory, spillPath := o.splitToolResult(sessionID, item.tc, item.content)
		o.publishToolResult(sessionID, item.tc, forClient, item.rejected, item.extra)
		o.recordToolResult(sessionID, item.tc.Function.Name, item.tc.Function.Arguments, forHistory, spillPath, item.rejected)
		o.recordToolExecutionSuccess(item.tc, forClient, item.rejected)
		o.appendHistory(sessionID, history, llm.Message{
			Role:       "tool",
			ToolCallID: item.tc.ID,
			Content:    forHistory,
		})
	}
	return nil
}

func (o *Orchestrator) invokeTool(ctx context.Context, sessionID string, tc llm.ToolCall, plan *clihitl.ApprovalPlan) (content string, rejected bool, extra map[string]any) {
	runInBackground, cleanedArgs := tools.ParseRunInBackground(tc.Function.Arguments)
	if tools.IsBackgroundJobTool(tc.Function.Name) {
		runInBackground = false
	}
	toolCtx := tools.WithToolCallID(tools.WithSession(ctx, sessionID), tc.ID)
	if target := resolveTriggerSessionTarget(tc, plan); target != "" {
		toolCtx = tools.WithTriggerSessionTarget(toolCtx, target)
	}

	var output string
	var execErr error
	if runInBackground {
		output, execErr = o.tools.StartBackground(toolCtx, sessionID, tc.Function.Name, tc.ID, cleanedArgs)
	} else {
		output, execErr = o.tools.Execute(toolCtx, tc.Function.Name, cleanedArgs)
		extra = o.tools.TakeBashCompressStatsForCall(tc.ID)
	}
	if execErr != nil {
		return execErr.Error(), true, nil
	}
	return output, false, extra
}

func (o *Orchestrator) executeTool(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	tc llm.ToolCall,
	plan *clihitl.ApprovalPlan,
) error {
	o.recordToolCall(sessionID, tc.Function.Name)
	content, rejected, extra := o.invokeTool(ctx, sessionID, tc, plan)
	forClient, forHistory, spillPath := o.splitToolResult(sessionID, tc, content)
	o.publishToolResult(sessionID, tc, forClient, rejected, extra)
	o.recordToolResult(sessionID, tc.Function.Name, tc.Function.Arguments, forHistory, spillPath, rejected)
	o.recordToolExecutionSuccess(tc, forClient, rejected)
	o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: forHistory})
	return nil
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

func (o *Orchestrator) appendDeniedTool(sessionID string, history *[]llm.Message, tc llm.ToolCall, reason string) {
	msg := "rejected: " + reason
	o.publishToolResult(sessionID, tc, msg, true, nil)
	o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: msg})
}

func (o *Orchestrator) publishToolCallPartial(sessionID string, tc llm.ToolCall, toolIndex int) {
	o.publishToolCallSSE(sessionID, tc, true, toolIndex)
}

func (o *Orchestrator) publishToolCallSSE(sessionID string, tc llm.ToolCall, partial bool, toolIndex int) {
	if partial {
		o.logger.Debug("tool call partial",
			"session_id", sessionID,
			"tool_name", tc.Function.Name,
			"tool_index", toolIndex,
		)
	} else {
		o.logger.Info("tool call",
			"session_id", sessionID,
			"tool_name", tc.Function.Name,
			"tool_call_id", tc.ID,
		)
	}
	payload := map[string]any{
		"partial": partial,
		"tool_calls": []map[string]any{{
			"id":   tc.ID,
			"type": tc.Type,
			"function": map[string]any{
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			},
		}},
	}
	if toolIndex >= 0 {
		payload["tool_index"] = toolIndex
	}
	o.hub.Publish(sessionID, o.agentID, "tool_call", payload)
}

func (o *Orchestrator) publishToolResult(sessionID string, tc llm.ToolCall, content string, rejected bool, extra map[string]any) {
	payload := map[string]any{
		"tool_call_id": tc.ID,
		"tool_name":    tc.Function.Name,
		"content":      content,
		"rejected":     rejected,
	}
	for k, v := range extra {
		payload[k] = v
	}
	o.hub.Publish(sessionID, o.agentID, "tool_result", payload)
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
