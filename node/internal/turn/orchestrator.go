// Package turn 实现 turn 编排、工具循环、分阶段 HITL 与状态机。
package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	historypkg "github.com/DGS-ai-team/DAgents/node/internal/history"
	"github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
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
	toolHooks    *hooks.Registry
	toolExecLog  *hooks.ToolExecutionLog
	skillAccess  SkillAccess
	maxToolLoops int
	promptCtx    *promptcontext.Reader
	journal      *historypkg.Journal
	logger       *slog.Logger

	childMgr       *childagent.Manager
	isChildSession bool

	turnUsageMu sync.Mutex
	turnUsage   map[string]llm.Usage

	ctxMetrics *contextMetricsStore

	enqueueToolResult   func(sessionID string) error
	systemPromptBuilder SystemPromptBuilder
}

// SetSystemPromptBuilder 注入 system prompt 构造器；nil 时使用默认 BuildSystemPrompt。
func (o *Orchestrator) SetSystemPromptBuilder(fn SystemPromptBuilder) {
	o.systemPromptBuilder = fn
}

// SetChildAgentManager 注入临时 Agent 管理器（仅父 session 调用）。
func (o *Orchestrator) SetChildAgentManager(m *childagent.Manager) {
	o.childMgr = m
}

// SetChildSession 标记当前 orchestrator 运行在子 session（禁止管理类工具与 ask_user）。
func (o *Orchestrator) SetChildSession(isChild bool) {
	o.isChildSession = isChild
}

// SetToolResultEnqueuer 注入 tool_result 入队回调；生产 session 必须设置以对齐 Python 队列语义。
func (o *Orchestrator) SetToolResultEnqueuer(fn func(sessionID string) error) {
	o.enqueueToolResult = fn
}

// SetPolicy 热更新策略引擎（policy API 写盘后调用）。
func (o *Orchestrator) SetPolicy(engine *policy.Engine) {
	if engine == nil {
		engine, _ = policy.LoadFile("")
	}
	o.policy = engine
	if o.toolHooks != nil {
		o.toolHooks.SetPolicyEngine(engine)
	}
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
	o.appendHistory(sessionID, history, llm.UserMessage(userText, llm.UserNameHuman))
	o.resetTurnUsage(sessionID)
	o.resetContextMetrics(sessionID)
	o.logger.Info("turn human message start", "session_id", sessionID, "content_len", len(userText))
	return o.runUntilQueueOrDone(ctx, sessionID, history, setState, 0)
}

// RunHumanMessageTurn 追加 user 消息后执行单步模型回合（human_message）。
func (o *Orchestrator) RunHumanMessageTurn(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	userText string,
	userName string,
	setState StateSetter,
) StepOutcome {
	if setState == nil {
		setState = func(State) {}
	}
	o.appendHistory(sessionID, history, llm.UserMessage(userText, llm.NormalizeUserMessageName(userName)))
	o.resetTurnUsage(sessionID)
	o.resetContextMetrics(sessionID)
	o.logger.Info("turn human message start",
		"session_id", sessionID,
		"content_len", len(userText),
		"user_name", llm.NormalizeUserMessageName(userName),
	)
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
	built := o.buildAsyncToolMessages(sessionID, input)
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
	o.hub.Publish(sessionID, o.agentID, "tool_result", asyncToolResultSSEPayload(built))
}

func asyncToolResultSSEPayload(built asyncToolMessages) map[string]any {
	payload := map[string]any{
		"tool_call_id": built.ToolCallID,
		"tool_name":    built.ToolName,
		"content":      built.ForClientContent,
		"partial":      false,
		"async_status": built.Status,
		"display_type": "normal_text",
	}
	if built.OutputCompressSavedPct > 0 {
		payload["output_compress_saved_pct"] = built.OutputCompressSavedPct
		payload["output_compress_raw_runes"] = built.OutputCompressRawRunes
		payload["output_compress_out_runes"] = built.OutputCompressOutRunes
	}
	return payload
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
	resumeToolCallID := strings.TrimSpace(fmt.Sprint(resumeValue["tool_call_id"]))
	pendingToolCallID := ""
	if pending.Kind == HITLUserInformation && pending.UserInfo != nil {
		pendingToolCallID = pending.UserInfo.ID
	} else if pending.Kind == HITLApproval && len(pending.ToolCalls) > 0 {
		pendingToolCallID = pending.ToolCalls[0].ID
	}
	o.logger.Info("turn resume",
		"session_id", sessionID,
		"hitl_kind", pending.Kind,
		"resume_tool_call_id", resumeToolCallID,
		"pending_tool_call_id", pendingToolCallID,
		"resume_value", resumeValue,
	)
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
			if err := o.executeAutoBatch(ctx, sessionID, history, approved, &plan); err != nil {
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
	hookCfg hooks.RuntimeConfig,
	logger *slog.Logger,
) *Orchestrator {
	if policyEngine == nil {
		policyEngine, _ = policy.LoadFile("")
	}
	toolExecLog := &hooks.ToolExecutionLog{}
	agentFileTrust := hooks.NewAgentFileTrust()
	hookCfg = hooks.RuntimeConfigOrDefault(hookCfg)
	if strings.TrimSpace(hookCfg.ToolResult.FSRoot) == "" {
		hookCfg.ToolResult.FSRoot = fsRoot
	}
	toolHooks := hooks.NewRegistry(policyEngine, hookCfg)
	toolHooks.SetToolExecutionLog(toolExecLog)
	toolHooks.SetAgentFileTrust(agentFileTrust)
	if reg, ok := toolExec.(*tools.Registry); ok {
		toolHooks.SetPathStater(reg)
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
		toolHooks:    toolHooks,
		toolExecLog:  toolExecLog,
		skillAccess:  skillAccess,
		maxToolLoops: maxToolLoops,
		promptCtx:    promptCtx,
		journal:      journal,
		logger:       logx.OrDefault(logger),
		ctxMetrics:   newContextMetricsStore(),
	}
}

// InterruptPending 在用户插入新 message 时打断 pending tool calls。
func (o *Orchestrator) InterruptPending(sessionID string, history *[]llm.Message, pending *PendingHITL) {
	if pending == nil {
		return
	}
	o.insertMissingToolResponsesAfterAssistant(
		sessionID,
		history,
		pending.AllToolCalls(),
		ToolUserInterruptedMessage,
		map[string]any{"interrupted_by_user_message": true},
	)
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
	o.RepairUnrespondedToolCalls(sessionID, history)
	finishReason := "stop"
	var streamErr error
	toolLoopCount++
	o.recordToolLoop(sessionID, toolLoopCount)
	if toolLoopCount > o.maxToolLoops {
		o.hub.Publish(sessionID, o.agentID, "error", map[string]any{
			"message": fmt.Sprintf("工具调用轮次超过上限：%d", o.maxToolLoops),
		})
		o.publishTurnIdleDone(sessionID, "error")
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("tool loop limit exceeded")}
	}

	toolDefs := o.ToolDefinitions()
	systemPrompt := o.buildSystemPrompt(sessionID)
	setState(StateModelStreaming)
	publishedToolPartial := make(map[int]string)
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
		OnToolCallDelta: func(calls []llm.ToolCall) {
			for i, tc := range calls {
				if strings.TrimSpace(tc.Function.Name) == "" {
					continue
				}
				fp := tc.ID + "\x1e" + tc.Function.Name + "\x1e" + tc.Function.Arguments
				if publishedToolPartial[i] == fp {
					continue
				}
				publishedToolPartial[i] = fp
				o.publishToolCallPartial(sessionID, tc, i)
			}
		},
		OnUsage: func(u llm.Usage) {
			o.accumulateAndPublishUsage(sessionID, toolLoopCount, u)
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			finishReason = "cancelled"
			streamErr = err
			o.logger.Info("turn llm cancelled", "session_id", sessionID, "loop", toolLoopCount)
			o.persistCancelledStream(sessionID, history, result)
		} else {
			o.hub.Publish(sessionID, o.agentID, "error", map[string]any{"message": err.Error()})
			finishReason = "error"
			streamErr = err
			o.logger.Error("turn llm failed", "session_id", sessionID, "loop", toolLoopCount, "error", err)
		}
		if finishReason == "cancelled" {
			o.publishAccumulatedUsageIfAny(sessionID, toolLoopCount)
		}
		o.publishTurnIdleDone(sessionID, finishReason)
		return StepOutcome{LoopCount: toolLoopCount, Err: streamErr}
	}

	assistant := assistantMessageFromResult(result)
	o.appendHistory(sessionID, history, assistant)

	if len(result.ToolCalls) == 0 {
		o.publishTurnIdleDone(sessionID, finishReason)
		o.logger.Info("turn done", "session_id", sessionID, "finish_reason", finishReason, "loop", toolLoopCount)
		return StepOutcome{LoopCount: toolLoopCount}
	}

	setState(StateAwaitingTool)
	pending, pauseReason, procErr := o.processToolCalls(ctx, sessionID, history, result.ToolCalls)
	if procErr != nil {
		if errors.Is(procErr, context.Canceled) {
			finishReason = "cancelled"
			o.appendMissingToolResponses(sessionID, history, result.ToolCalls, ToolStreamInterruptedMessage, map[string]any{"interrupted_by_stream_cancel": true})
			o.publishAccumulatedUsageIfAny(sessionID, toolLoopCount)
		} else {
			finishReason = "error"
			o.hub.Publish(sessionID, o.agentID, "error", map[string]any{"message": procErr.Error()})
		}
		o.publishTurnIdleDone(sessionID, finishReason)
		return StepOutcome{LoopCount: toolLoopCount, Err: procErr}
	}
	if pending != nil {
		o.publishTurnIdleDone(sessionID, pauseReason)
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

// publishTurnIdleDone 推送语义 B 的 done：编排器暂停，轮到用户交互（非段落换行）。
//
// 逻辑：
// 1. 始终带 finish_reason；
// 2. HITL 暂停：turn_complete=false + awaiting；
// 3. stop/error/cancelled：turn_complete=true，awaiting 为空。
func (o *Orchestrator) publishTurnIdleDone(sessionID, finishReason string) {
	payload := map[string]any{"finish_reason": finishReason}
	switch finishReason {
	case "awaiting_user_information":
		payload["turn_complete"] = false
		payload["awaiting"] = "user_information"
	case "awaiting_tool_approval":
		payload["turn_complete"] = false
		payload["awaiting"] = "tool_approval"
	default:
		payload["turn_complete"] = true
		payload["awaiting"] = nil
	}
	if m := o.contextMetrics(sessionID); m != nil {
		payload["tool_context_metrics"] = m.snapshot()
	}
	o.publishTurnContextMetrics(sessionID, finishReason)
	o.hub.Publish(sessionID, o.agentID, "done", payload)
}

func (o *Orchestrator) resetTurnUsage(sessionID string) {
	if o == nil {
		return
	}
	o.turnUsageMu.Lock()
	delete(o.turnUsage, sessionID)
	o.turnUsageMu.Unlock()
}

func (o *Orchestrator) accumulateAndPublishUsage(sessionID string, llmStep int, u llm.Usage) {
	if o == nil {
		return
	}
	o.turnUsageMu.Lock()
	if o.turnUsage == nil {
		o.turnUsage = make(map[string]llm.Usage)
	}
	acc := o.turnUsage[sessionID]
	acc.AccumulateFrom(u)
	o.turnUsage[sessionID] = acc
	payload := llm.UsageSSEEvent(llmStep, u, acc)
	o.turnUsageMu.Unlock()
	o.hub.Publish(sessionID, o.agentID, "usage", payload)
}

// publishAccumulatedUsageIfAny 在 turn 取消时补发已累计 usage，避免客户端 strip 丢失末次快照。
func (o *Orchestrator) publishAccumulatedUsageIfAny(sessionID string, llmStep int) {
	if o == nil || o.hub == nil {
		return
	}
	o.turnUsageMu.Lock()
	acc, ok := o.turnUsage[sessionID]
	o.turnUsageMu.Unlock()
	if !ok {
		return
	}
	norm := acc
	norm.Normalize()
	if norm.PromptTokens <= 0 && norm.CompletionTokens <= 0 {
		return
	}
	payload := llm.UsageSSEEvent(llmStep, llm.Usage{}, acc)
	o.hub.Publish(sessionID, o.agentID, "usage", payload)
}

// SystemPromptForSession 返回当前 session 下一步 LLM 调用将使用的 system prompt。
func (o *Orchestrator) SystemPromptForSession(sessionID string) string {
	return o.buildSystemPrompt(sessionID)
}

// ToolDefinitions 返回与 runOneStep 相同的 tools 列表（侧车压缩前缀对齐用）。
func (o *Orchestrator) ToolDefinitions() []tools.ToolDef {
	if o == nil || o.tools == nil {
		return nil
	}
	return o.tools.Definitions()
}

func (o *Orchestrator) buildSystemPrompt(sessionID string) string {
	var loaded []skills.LoadedSkill
	if o.skillAccess.Get != nil {
		loaded = o.skillAccess.Get()
	}
	in := SystemPromptInput{
		AgentID:   o.agentID,
		FSRoot:    o.fsRoot,
		SessionID: sessionID,
		Catalog:   o.skillAccess.Catalog,
		Loaded:    loaded,
		PromptCtx: o.promptCtx,
	}
	if o.systemPromptBuilder != nil {
		return o.systemPromptBuilder(in)
	}
	return BuildSystemPrompt(in)
}
