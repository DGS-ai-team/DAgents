// Package turn 实现 turn 编排、工具循环、分阶段 HITL 与状态机。
package turn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/hitl"
	historypkg "github.com/DGS-ai-team/DAgents/node/internal/history"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// State 表示 session 内 turn 生命周期阶段。
type State string

const (
	StateIdle           State = "idle"
	StateModelStreaming State = "model_streaming"
	StateAwaitingTool   State = "awaiting_tool"
)

// StateSetter 在 turn 阶段切换时回调（session 写入 turn 状态）。
type StateSetter func(State)

// SkillAccess 为 orchestrator 读写 session loaded_skills 的回调。
type SkillAccess struct {
	Catalog *skills.Catalog
	Get     func() []skills.LoadedSkill
	Set     func([]skills.LoadedSkill)
}

// Orchestrator 驱动 LLM + 工具循环并通过 Hub 推送 SSE。
type Orchestrator struct {
	llm          llm.Client
	hub          stream.Publisher
	agentID      string
	fsRoot       string
	tools        tools.Executor
	policy       *policy.Engine
	skillAccess  SkillAccess
	maxToolLoops int
	promptCtx    *promptcontext.Reader
	journal      *historypkg.Journal
	logger       *slog.Logger

	childMgr       *childagent.Manager
	isChildSession bool

	enqueueToolResult func(sessionID string) error
}

// SetChildAgentTools 注入子 Agent 工具处理器；isChild 为 true 时禁止调用管理工具。
func (o *Orchestrator) SetChildAgentTools(m *childagent.Manager, isChild bool) {
	o.childMgr = m
	o.isChildSession = isChild
}

// SetToolResultEnqueuer 注入 tool_result 入队回调；生产 session 必须设置以对齐 Python 队列语义。
func (o *Orchestrator) SetToolResultEnqueuer(fn func(sessionID string) error) {
	o.enqueueToolResult = fn
}

// RunMessageTurn 执行 human_message 回合；测试无 enqueuer 时内联多步，生产应使用 RunHumanMessageTurn 单步 + 队列。
func (o *Orchestrator) RunMessageTurn(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	userText string,
	setState StateSetter,
	toolLoopCount int,
) (*PendingHITL, int, error) {
	if setState == nil {
		setState = func(State) {}
	}
	o.appendHistory(sessionID, history, llm.Message{Role: "user", Content: userText})
	o.logger.Info("turn human message start", "session_id", sessionID, "content_len", len(userText))
	return o.runUntilQueueOrDone(ctx, sessionID, history, setState, 0)
}

// RunHumanMessageTurn 追加 user 消息后执行单步模型回合（human_message）。
func (o *Orchestrator) RunHumanMessageTurn(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	userText string,
	setState StateSetter,
) StepOutcome {
	if setState == nil {
		setState = func(State) {}
	}
	o.appendHistory(sessionID, history, llm.Message{Role: "user", Content: userText})
	o.logger.Info("turn human message start", "session_id", sessionID, "content_len", len(userText))
	return o.runOneStep(ctx, sessionID, history, setState, 0)
}

// RunToolMessageTurn 在 history 已含 tool 结果后执行单步模型回合（tool_message，不追加 user）。
func (o *Orchestrator) RunToolMessageTurn(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	setState StateSetter,
	toolLoopCount int,
) StepOutcome {
	if setState == nil {
		setState = func(State) {}
	}
	if strings.TrimSpace(RuntimeToolMessageContent) == "" {
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("missing tool_message content")}
	}
	o.logger.Info("turn tool message start", "session_id", sessionID, "loop", toolLoopCount)
	return o.runOneStep(ctx, sessionID, history, setState, toolLoopCount)
}

// HandleAsyncToolResult 将异步工具完成写回 history 并按尾部形态决定是否继续 tool_message 回合。
func (o *Orchestrator) HandleAsyncToolResult(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	input AsyncToolResultInput,
	setState StateSetter,
	toolLoopCount int,
) StepOutcome {
	if setState == nil {
		setState = func(State) {}
	}
	built := buildAsyncToolMessages(input)
	tail := classifyToolResultTail(*history)
	switch tail {
	case tailTool:
		o.appendHistory(sessionID, history, built.AssistantMessage)
		o.appendHistory(sessionID, history, built.ToolMessage)
	case tailAssistantWithToolCalls:
		insertAt := len(*history) - 1
		o.insertHistory(sessionID, history, insertAt, built.AssistantMessage)
		o.insertHistory(sessionID, history, insertAt+1, built.ToolMessage)
	case tailAssistantWithoutToolCalls:
		o.appendHistory(sessionID, history, built.UserMessage)
		o.appendHistory(sessionID, history, built.AssistantMessage)
		o.appendHistory(sessionID, history, built.ToolMessage)
	default:
		o.appendHistory(sessionID, history, built.AssistantMessage)
		o.appendHistory(sessionID, history, built.ToolMessage)
	}
	o.publishAsyncToolCallbackSSE(sessionID, built)
	if !shouldContinueAfterAsyncTool(tail) {
		return StepOutcome{LoopCount: toolLoopCount}
	}
	return o.RunToolMessageTurn(ctx, sessionID, history, setState, toolLoopCount)
}

func (o *Orchestrator) publishAsyncToolCallbackSSE(sessionID string, built asyncToolMessages) {
	o.hub.Publish(sessionID, o.agentID, "tool_call", map[string]any{
		"assistant_content": "",
		"tool_calls": []map[string]any{{
			"id":   built.ToolCallID,
			"name": "tool_callback",
			"arguments": map[string]any{
				"job_id": built.AssistantMessage.ToolCalls[0].Function.Arguments,
			},
			"raw_arguments": built.AssistantMessage.ToolCalls[0].Function.Arguments,
		}},
		"display_type": "normal_text",
	})
	o.hub.Publish(sessionID, o.agentID, "tool_result", map[string]any{
		"tool_call_id": built.ToolCallID,
		"tool_name":    built.ToolName,
		"content":      built.ToolMessage.Content,
		"partial":      false,
		"async_status": built.Status,
		"display_type": "normal_text",
	})
}

// ContinueAfterResume 在 Client 提交 resume 后写入 tool 结果并调度 tool_result 续跑。
func (o *Orchestrator) ContinueAfterResume(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	resumeValue map[string]any,
	pending *PendingHITL,
	setState StateSetter,
	toolLoopCount int,
) StepOutcome {
	if pending == nil {
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("no pending hitl")}
	}
	if setState == nil {
		setState = func(State) {}
	}
	o.logger.Info("turn resume", "session_id", sessionID, "hitl_kind", pending.Kind)
	switch pending.Kind {
	case HITLUserInformation:
		if pending.UserInfo == nil {
			return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("missing user_information tool call")}
		}
		tc := *pending.UserInfo
		content, err := hitl.ParseUserInformationResume(resumeValue, tc.ID)
		if err != nil {
			content = err.Error()
		}
		o.publishToolResult(sessionID, tc, content, false, nil)
		o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: content})
	case HITLApproval:
		ids := make([]string, 0, len(pending.ToolCalls))
		for _, tc := range pending.ToolCalls {
			ids = append(ids, tc.ID)
		}
		plan, err := hitl.ParseApprovalResume(resumeValue, ids)
		if err != nil {
			for _, tc := range pending.ToolCalls {
				o.appendDeniedTool(sessionID, history, tc, err.Error())
			}
		} else {
			var approved []llm.ToolCall
			for _, tc := range pending.ToolCalls {
				if plan.IsApproved(tc.ID) {
					approved = append(approved, tc)
				} else {
					o.appendDeniedTool(sessionID, history, tc, "user_rejected")
				}
			}
			if err := o.executeAutoBatch(ctx, sessionID, history, approved); err != nil {
				return StepOutcome{LoopCount: toolLoopCount, Err: err}
			}
		}
	default:
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("unknown pending hitl kind")}
	}
	return StepOutcome{LoopCount: toolLoopCount, ScheduleToolResult: true}
}

func NewOrchestrator(
	agentID, fsRoot string,
	hub stream.Publisher,
	client llm.Client,
	toolExec tools.Executor,
	policyEngine *policy.Engine,
	skillAccess SkillAccess,
	maxToolLoops int,
	promptCtx *promptcontext.Reader,
	journal *historypkg.Journal,
	logger *slog.Logger,
) *Orchestrator {
	if policyEngine == nil {
		policyEngine, _ = policy.LoadFile("")
	}
	if maxToolLoops <= 0 {
		maxToolLoops = DefaultMaxToolLoops()
	}
	return &Orchestrator{
		agentID:      agentID,
		fsRoot:       fsRoot,
		hub:          hub,
		llm:          client,
		tools:        toolExec,
		policy:       policyEngine,
		skillAccess:  skillAccess,
		maxToolLoops: maxToolLoops,
		promptCtx:    promptCtx,
		journal:      journal,
		logger:       logx.OrDefault(logger),
	}
}

// InterruptPending 在用户插入新 message 时打断 pending tool calls。
func (o *Orchestrator) InterruptPending(sessionID string, history *[]llm.Message, pending *PendingHITL) {
	if pending == nil {
		return
	}
	extra := map[string]any{"interrupted_by_user_message": true}
	for _, tc := range pending.AllToolCalls() {
		o.publishToolResult(sessionID, tc, ToolUserInterruptedMessage, false, extra)
		o.appendHistory(sessionID, history, llm.Message{
			Role:       "tool",
			ToolCallID: tc.ID,
			Content:    ToolUserInterruptedMessage,
		})
	}
}

func (o *Orchestrator) runUntilQueueOrDone(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	setState StateSetter,
	toolLoopCount int,
) (*PendingHITL, int, error) {
	for {
		outcome := o.runOneStep(ctx, sessionID, history, setState, toolLoopCount)
		if outcome.Err != nil {
			return outcome.Pending, outcome.LoopCount, outcome.Err
		}
		if outcome.Pending != nil {
			return outcome.Pending, outcome.LoopCount, nil
		}
		if outcome.ScheduleToolResult {
			if o.enqueueToolResult != nil {
				if err := o.enqueueToolResult(sessionID); err != nil {
					return nil, outcome.LoopCount, err
				}
				return nil, outcome.LoopCount, nil
			}
			toolLoopCount = outcome.LoopCount
			continue
		}
		return nil, outcome.LoopCount, nil
	}
}

func (o *Orchestrator) runOneStep(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	setState StateSetter,
	toolLoopCount int,
) StepOutcome {
	finishReason := "stop"
	var streamErr error
	toolLoopCount++
	if toolLoopCount > o.maxToolLoops {
		o.hub.Publish(sessionID, o.agentID, "error", map[string]any{
			"message": fmt.Sprintf("工具调用轮次超过上限：%d", o.maxToolLoops),
		})
		o.hub.Publish(sessionID, o.agentID, "done", map[string]any{"finish_reason": "error"})
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("tool loop limit exceeded")}
	}

	toolDefs := o.tools.Definitions()
	systemPrompt := o.buildSystemPrompt(sessionID)
	setState(StateModelStreaming)
	result, err := o.llm.StreamChat(ctx, llm.ChatRequest{
		SystemPrompt: systemPrompt,
		Messages:     *history,
		Tools:        toolDefs,
	}, llm.StreamHandler{
		OnDelta: func(delta string) {
			o.hub.Publish(sessionID, o.agentID, "assistant", map[string]any{
				"content":      delta,
				"display_type": "delta",
			})
		},
		OnReasoningDelta: func(delta string) {
			o.hub.Publish(sessionID, o.agentID, "reasoning", map[string]any{
				"content":      delta,
				"display_type": "reasoning",
			})
		},
		OnUsage: func(u llm.Usage) {
			o.hub.Publish(sessionID, o.agentID, "usage", map[string]any{
				"prompt_tokens":     u.PromptTokens,
				"completion_tokens": u.CompletionTokens,
				"total_tokens":      u.TotalTokens,
			})
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			finishReason = "cancelled"
			streamErr = err
			o.logger.Info("turn llm cancelled", "session_id", sessionID, "loop", toolLoopCount)
		} else {
			o.hub.Publish(sessionID, o.agentID, "error", map[string]any{"message": err.Error()})
			finishReason = "error"
			streamErr = err
			o.logger.Error("turn llm failed", "session_id", sessionID, "loop", toolLoopCount, "error", err)
		}
		o.hub.Publish(sessionID, o.agentID, "done", map[string]any{"finish_reason": finishReason})
		return StepOutcome{LoopCount: toolLoopCount, Err: streamErr}
	}

	assistant := llm.Message{Role: "assistant", Content: result.Content}
	if strings.TrimSpace(result.ReasoningContent) != "" {
		assistant.ReasoningContent = result.ReasoningContent
	}
	if len(result.ToolCalls) > 0 {
		assistant.ToolCalls = result.ToolCalls
	}
	o.appendHistory(sessionID, history, assistant)

	if len(result.ToolCalls) == 0 {
		o.hub.Publish(sessionID, o.agentID, "done", map[string]any{"finish_reason": finishReason})
		o.logger.Info("turn done", "session_id", sessionID, "finish_reason", finishReason, "loop", toolLoopCount)
		return StepOutcome{LoopCount: toolLoopCount}
	}

	setState(StateAwaitingTool)
	pending, pauseReason, procErr := o.processToolCalls(ctx, sessionID, history, result.ToolCalls)
	if procErr != nil {
		if errors.Is(procErr, context.Canceled) {
			finishReason = "cancelled"
		} else {
			finishReason = "error"
			o.hub.Publish(sessionID, o.agentID, "error", map[string]any{"message": procErr.Error()})
		}
		o.hub.Publish(sessionID, o.agentID, "done", map[string]any{"finish_reason": finishReason})
		return StepOutcome{LoopCount: toolLoopCount, Err: procErr}
	}
	if pending != nil {
		o.hub.Publish(sessionID, o.agentID, "done", map[string]any{"finish_reason": pauseReason})
		o.logger.Info("turn paused", "session_id", sessionID, "finish_reason", pauseReason, "loop", toolLoopCount)
		return StepOutcome{Pending: pending, LoopCount: toolLoopCount}
	}
	if o.enqueueToolResult != nil {
		if err := o.enqueueToolResult(sessionID); err != nil {
			return StepOutcome{LoopCount: toolLoopCount, Err: err}
		}
		return StepOutcome{LoopCount: toolLoopCount}
	}
	return StepOutcome{LoopCount: toolLoopCount, ScheduleToolResult: true}
}

func (o *Orchestrator) insertHistory(sessionID string, history *[]llm.Message, index int, message llm.Message) {
	if o.journal != nil {
		o.journal.InsertMessage(sessionID, history, index, message)
	} else {
		normalized := historypkg.NormalizeMessageForContext(*history, message, o.logger)
		if index < 0 {
			index = 0
		}
		if index > len(*history) {
			index = len(*history)
		}
		out := append([]llm.Message(nil), (*history)[:index]...)
		out = append(out, normalized)
		out = append(out, (*history)[index:]...)
		*history = out
	}
}

func (o *Orchestrator) buildSystemPrompt(sessionID string) string {
	var loaded []skills.LoadedSkill
	if o.skillAccess.Get != nil {
		loaded = o.skillAccess.Get()
	}
	return BuildSystemPrompt(SystemPromptInput{
		AgentID:   o.agentID,
		FSRoot:    o.fsRoot,
		SessionID: sessionID,
		Catalog:   o.skillAccess.Catalog,
		Loaded:    loaded,
		PromptCtx: o.promptCtx,
	})
}

func (o *Orchestrator) processToolCalls(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	calls []llm.ToolCall,
) (*PendingHITL, string, error) {
	var autoCalls, approvalCalls []llm.ToolCall
	var userInfo *llm.ToolCall

	for _, tc := range calls {
		o.publishToolCall(sessionID, tc)

		if childagent.IsChildAgentTool(tc.Function.Name) {
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
		switch o.policy.DecideTool(tc.Function.Name, parseJSONArgs(tc.Function.Arguments)) {
		case policy.ActionDeny:
			o.appendDeniedTool(sessionID, history, tc, "policy_denied")
		case policy.ActionRequireApproval:
			approvalCalls = append(approvalCalls, tc)
		default:
			autoCalls = append(autoCalls, tc)
		}
	}

	if err := o.executeAutoBatch(ctx, sessionID, history, autoCalls); err != nil {
		return nil, "", err
	}

	if userInfo != nil {
		question, uiArgs := buildUserInformationPayload(*userInfo)
		o.hub.Publish(sessionID, o.agentID, "user_information_required", map[string]any{
			"content":                 question,
			"user_information_args": uiArgs,
			"display_type":          "normal_text",
		})
		return &PendingHITL{Kind: HITLUserInformation, UserInfo: userInfo}, "awaiting_user_information", nil
	}

	if len(approvalCalls) > 0 {
		approvalID := newShortID("appr-")
		executionID := newShortID("exec-")
		toolItems := make([]map[string]any, 0, len(approvalCalls))
		for _, tc := range approvalCalls {
			toolItems = append(toolItems, map[string]any{
				"id":            tc.ID,
				"name":          tc.Function.Name,
				"arguments":     parseJSONArgs(tc.Function.Arguments),
				"raw_arguments": tc.Function.Arguments,
			})
		}
		o.hub.Publish(sessionID, o.agentID, "approval_required", map[string]any{
			"approval_type": "execute_tool",
			"approval_id":   approvalID,
			"execution_id":  executionID,
			"message":       "检测到工具调用，等待用户确认后继续执行。",
			"approval_args": map[string]any{"tool_calls": toolItems},
			"display_type":  "normal_text",
		})
		return &PendingHITL{Kind: HITLApproval, ToolCalls: append([]llm.ToolCall(nil), approvalCalls...)}, "awaiting_tool_approval", nil
	}
	return nil, "", nil
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
			"action":           "set_loaded_skills",
			"loaded_skills":    loaded,
			"available_skills": catalog.ListMetadata(),
		})
		output = string(body)
	case "unload_skills":
		names := stringSliceField(payload, "skill_names")
		loaded = catalog.UnloadSkills(loaded, names)
		body, _ := json.Marshal(map[string]any{
			"action":           "unload_skills",
			"loaded_skills":    loaded,
			"available_skills": catalog.ListMetadata(),
		})
		output = string(body)
	case "clear_skills":
		loaded = nil
		body, _ := json.Marshal(map[string]any{
			"action":           "clear_skills",
			"loaded_skills":    []skills.LoadedSkill{},
			"available_skills": catalog.ListMetadata(),
		})
		output = string(body)
	default:
		output = "ERROR: unknown skill tool"
	}
	if o.skillAccess.Set != nil {
		o.skillAccess.Set(loaded)
	}
	o.publishToolResult(sessionID, tc, output, strings.HasPrefix(output, "ERROR:"), nil)
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
) error {
	if len(autoCalls) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(autoCalls) == 1 {
		return o.executeTool(ctx, sessionID, history, autoCalls[0])
	}
	type batchItem struct {
		tc       llm.ToolCall
		content  string
		rejected bool
	}
	results := make([]batchItem, len(autoCalls))
	var wg sync.WaitGroup
	for i := range autoCalls {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tc := autoCalls[idx]
			content, rejected := o.invokeTool(ctx, sessionID, tc)
			results[idx] = batchItem{tc: tc, content: content, rejected: rejected}
		}(i)
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	// 并行执行但按模型 tool_calls 顺序落 history，保证 OpenAI 协议一致。
	for _, item := range results {
		o.publishToolResult(sessionID, item.tc, item.content, item.rejected, nil)
		o.appendHistory(sessionID, history, llm.Message{
			Role:       "tool",
			ToolCallID: item.tc.ID,
			Content:    item.content,
		})
	}
	return nil
}

func (o *Orchestrator) invokeTool(ctx context.Context, sessionID string, tc llm.ToolCall) (content string, rejected bool) {
	runInBackground, cleanedArgs := tools.ParseRunInBackground(tc.Function.Arguments)
	if tools.IsBackgroundJobTool(tc.Function.Name) {
		runInBackground = false
	}
	toolCtx := tools.WithSession(ctx, sessionID)

	var output string
	var execErr error
	if runInBackground {
		output, execErr = o.tools.StartBackground(toolCtx, sessionID, tc.Function.Name, tc.ID, cleanedArgs)
	} else {
		output, execErr = o.tools.Execute(toolCtx, tc.Function.Name, cleanedArgs)
	}
	if execErr != nil {
		return execErr.Error(), true
	}
	return output, false
}

func (o *Orchestrator) executeTool(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	tc llm.ToolCall,
) error {
	content, rejected := o.invokeTool(ctx, sessionID, tc)
	o.publishToolResult(sessionID, tc, content, rejected, nil)
	o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: content})
	return nil
}

func (o *Orchestrator) appendDeniedTool(sessionID string, history *[]llm.Message, tc llm.ToolCall, reason string) {
	msg := "rejected: " + reason
	o.publishToolResult(sessionID, tc, msg, true, nil)
	o.appendHistory(sessionID, history, llm.Message{Role: "tool", ToolCallID: tc.ID, Content: msg})
}

func (o *Orchestrator) appendHistory(sessionID string, history *[]llm.Message, message llm.Message) {
	if o.journal != nil {
		o.journal.AppendMessage(sessionID, history, message)
	} else {
		normalized := historypkg.NormalizeMessageForContext(*history, message, o.logger)
		*history = append(*history, normalized)
	}
}

func (o *Orchestrator) publishToolCall(sessionID string, tc llm.ToolCall) {
	o.logger.Info("tool call",
		"session_id", sessionID,
		"tool_name", tc.Function.Name,
		"tool_call_id", tc.ID,
	)
	o.hub.Publish(sessionID, o.agentID, "tool_call", map[string]any{
		"tool_calls": []map[string]any{{
			"id":   tc.ID,
			"type": tc.Type,
			"function": map[string]any{
				"name":      tc.Function.Name,
				"arguments": tc.Function.Arguments,
			},
		}},
	})
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
