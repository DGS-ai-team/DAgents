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
	pending.Normalize()
	pendingCount := len(pending.Items)
	if pendingCount > 0 {
		pendingToolCallID = pending.Items[0].ToolCall.ID
	}
	o.logger.Info("turn resume",
		"session_id", sessionID,
		"pending_items", pendingCount,
		"resume_tool_call_id", resumeToolCallID,
		"pending_tool_call_id", pendingToolCallID,
		"resume_value_kind", hitl.ResumeValueKind(resumeValue),
		"resume_value", resumeValue,
	)
	switch hitl.ResumeValueKind(resumeValue) {
	case "user_information":
		return o.continueAfterUserInformationResume(ctx, sessionID, history, resumeValue, pending, toolLoopCount)
	case "approval":
		return o.continueAfterApprovalResume(ctx, sessionID, history, resumeValue, pending, toolLoopCount)
	default:
		return StepOutcome{LoopCount: toolLoopCount, Err: fmt.Errorf("unsupported resume type")}
	}
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
	orch := &Orchestrator{
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
	registerSystemPromptBuildHook(orch)
	return orch
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
		o.publishError(sessionID, fmt.Sprintf("工具调用轮次超过上限：%d", o.maxToolLoops))
		o.publishDone(sessionID, "error")
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
			o.publishAssistant(sessionID, delta)
		},
		OnReasoningDelta: func(delta string) {
			o.publishReasoning(sessionID, delta)
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
				o.publishToolCall(sessionID, tc, true, i)
			}
		},
		OnUsage: func(u llm.Usage) {
			o.publishUsage(sessionID, toolLoopCount, u)
		},
	})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			finishReason = "cancelled"
			streamErr = err
			o.logger.Info("turn llm cancelled", "session_id", sessionID, "loop", toolLoopCount)
			o.persistCancelledStream(sessionID, history, result)
		} else {
			o.publishError(sessionID, err.Error())
			finishReason = "error"
			streamErr = err
			o.logger.Error("turn llm failed", "session_id", sessionID, "loop", toolLoopCount, "error", err)
		}
		if finishReason == "cancelled" {
			o.publishUsageIfAccumulated(sessionID, toolLoopCount)
		}
		o.publishDone(sessionID, finishReason)
		return StepOutcome{LoopCount: toolLoopCount, Err: streamErr}
	}

	result, hookErr := o.runLLMAfterCallPhase(ctx, sessionID, result)
	if hookErr != nil {
		msg := hookErr.Error()
		if isLLMAfterCallAbort(hookErr) {
			o.logger.Warn("llm.after_call aborted turn", "session_id", sessionID, "error", hookErr)
		} else {
			o.logger.Warn("llm.after_call hook failed", "session_id", sessionID, "error", hookErr)
		}
		o.publishError(sessionID, msg)
		o.publishDone(sessionID, "error")
		return StepOutcome{LoopCount: toolLoopCount, Err: hookErr}
	}

	assistant := assistantMessageFromResult(result)
	o.appendHistory(sessionID, history, assistant)

	if len(result.ToolCalls) == 0 {
		o.publishDone(sessionID, finishReason)
		o.logger.Info("turn done", "session_id", sessionID, "finish_reason", finishReason, "loop", toolLoopCount)
		return StepOutcome{LoopCount: toolLoopCount}
	}

	setState(StateAwaitingTool)
	pending, pauseReason, procErr := o.processToolCalls(ctx, sessionID, history, result.ToolCalls)
	if procErr != nil {
		if errors.Is(procErr, context.Canceled) {
			finishReason = "cancelled"
			o.appendMissingToolResponses(sessionID, history, result.ToolCalls, ToolStreamInterruptedMessage, map[string]any{"interrupted_by_stream_cancel": true})
			o.publishUsageIfAccumulated(sessionID, toolLoopCount)
		} else {
			finishReason = "error"
			o.publishError(sessionID, procErr.Error())
		}
		o.publishDone(sessionID, finishReason)
		return StepOutcome{LoopCount: toolLoopCount, Err: procErr}
	}
	if pending != nil {
		o.publishDone(sessionID, pauseReason)
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

// resetTurnUsage 新 user 消息 turn 开始时清零 token 累计，避免上轮用量带入 SSE usage。
func (o *Orchestrator) resetTurnUsage(sessionID string) {
	if o == nil {
		return
	}
	o.turnUsageMu.Lock()
	delete(o.turnUsage, sessionID)
	o.turnUsageMu.Unlock()
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
	if o.toolHooks == nil {
		return o.composeSystemPrompt(sessionID)
	}
	hc := hooks.BuildPromptBuildContext(sessionID, o.agentID, "")
	out, err := o.toolHooks.RunPhase(context.Background(), hooks.PhasePromptBuild, hc)
	if err != nil {
		return o.composeSystemPrompt(sessionID)
	}
	prompt := hooks.SystemPromptFrom(out, "")
	if prompt == "" {
		return o.composeSystemPrompt(sessionID)
	}
	return prompt
}

func (o *Orchestrator) runTurnDonePhase(sessionID, finishReason string) {
	if o.toolHooks == nil {
		return
	}
	hc := hooks.BuildTurnDoneContext(sessionID, o.agentID, finishReason)
	_, _ = o.toolHooks.RunPhase(context.Background(), hooks.PhaseTurnDone, hc)
}

func (o *Orchestrator) composeSystemPrompt(sessionID string) string {
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
