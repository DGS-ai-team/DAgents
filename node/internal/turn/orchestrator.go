// Package turn 实现 turn 编排、工具循环、分阶段 HITL 与状态机。
package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	historypkg "github.com/DGS-ai-team/DAgents/node/internal/history"
	"github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/media"
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

// SkillAccess 为 orchestrator 读写 session loaded_skills 的回调。
type SkillAccess struct {
	Catalog *skills.Catalog
	Get     func() []skills.LoadedSkill
	Set     func([]skills.LoadedSkill)
}

// LifecycleCommandSink accepts one durable Turn/Step fact. Returning an
// error lets execution boundaries stop before a side effect is opened when
// the session-owned lifecycle projection cannot accept the fact.
type LifecycleCommandSink func(sessionID string, command TurnCommand) error

// Orchestrator 驱动 LLM + 工具循环并通过 Hub 推送 SSE。
type Orchestrator struct {
	llm             llm.Client
	hub             stream.Publisher
	agentID         string
	fsRoot          string
	tools           tools.Executor
	policy          *policy.Engine
	toolHooks       *hooks.Registry
	toolExecLog     *hooks.ToolExecutionLog
	skillAccess     SkillAccess
	hookRuntimeCfg  hooks.RuntimeConfig
	hookHostCfg     HookHostConfig
	hookHostState   *hookHostState
	maxToolLoops    int
	modelRetryLimit int
	toolRetryLimit  int
	promptCtx       *promptcontext.Reader
	longTermStore   LongTermStore
	journal         *historypkg.Journal
	logger          *slog.Logger

	childMgr       *childagent.Manager
	isChildSession bool

	turnUsageMu sync.Mutex
	turnUsage   map[string]llm.Usage
	summaryMu   sync.Mutex
	summaryNext map[string]bool

	ctxMetrics *contextMetricsStore

	modelSnapshots  *modelContextSnapshotStore
	runtimeRevision int64
	runtimeDigest   string
	executionGuard  ExecutionGuard

	enqueueToolResult   func(ctx context.Context, sessionID string) error
	systemPromptBuilder SystemPromptBuilder
	lifecycleMetadata   func(sessionID string) map[string]any
	lifecycleCommand    LifecycleCommandSink
	toolBudgetCheck     func(sessionID string) (bool, string)
	toolRetryCheck      func(sessionID string) (bool, string)
	modelRetryCheck     func(sessionID string) (bool, string)

	multimodalEnabled bool
	mediaReg          *media.Registry
}

// SetHookHostConfig 注入 Host 路径与配额配置。
func (o *Orchestrator) SetHookHostConfig(cfg HookHostConfig) {
	if o == nil {
		return
	}
	o.hookHostCfg = cfg.normalized()
	if o.hookHostState != nil {
		o.hookHostState.mu.Lock()
		o.hookHostState.fsRoot = o.fsRoot
		o.hookHostState.mu.Unlock()
	}
}

// SetSystemPromptBuilder 注入 system prompt 构造器；nil 时使用默认 BuildSystemPrompt。
func (o *Orchestrator) SetSystemPromptBuilder(fn SystemPromptBuilder) {
	o.systemPromptBuilder = fn
}

// SetRuntimeIdentity attaches diagnostics to each Turn snapshot. These values
// are never inserted into the model-visible prompt.
func (o *Orchestrator) SetRuntimeIdentity(revision int64, digest string) {
	if o == nil {
		return
	}
	o.runtimeRevision = revision
	o.runtimeDigest = strings.TrimSpace(digest)
}

// SetModelRetryLimit controls bounded retries for transient provider failures.
// A retry stays inside the current Step and is recorded as a new ModelAttempt;
// partial streamed output is never retried because replaying it would duplicate
// user-visible text.
func (o *Orchestrator) SetModelRetryLimit(limit int) {
	if o == nil {
		return
	}
	if limit < 0 {
		limit = 0
	}
	o.modelRetryLimit = limit
}

// SetExecutionGuard replaces the latest-state execution check. The default
// guard delegates to the existing policy/hooks path; tool providers still do
// their own channel and credential validation at the actual execution edge.
func (o *Orchestrator) SetExecutionGuard(guard ExecutionGuard) {
	if o == nil {
		return
	}
	o.executionGuard = guard
}

// ModelContextSnapshot returns the active Turn snapshot, if any.
func (o *Orchestrator) ModelContextSnapshot(sessionID string) *ModelContextSnapshot {
	if o == nil || o.modelSnapshots == nil {
		return nil
	}
	return o.modelSnapshots.get(sessionID)
}

// RestoreModelContextSnapshot hydrates the frozen model inputs for an active
// Turn after a Node restart. It is intentionally separate from normal setters
// so callers cannot accidentally replace a live Turn snapshot mid-step.
func (o *Orchestrator) RestoreModelContextSnapshot(sessionID string, snapshot *ModelContextSnapshot) {
	if o == nil || snapshot == nil {
		return
	}
	o.setModelContextSnapshot(sessionID, snapshot)
}

// SetMultimodalEnabled 控制 read_image 后的 vision user 消息注入。
func (o *Orchestrator) SetMultimodalEnabled(enabled bool) {
	if o == nil {
		return
	}
	o.multimodalEnabled = enabled
}

// SetMediaRegistry 注入 session media registry（用户图 LLM 展开，F-M5）。
func (o *Orchestrator) SetMediaRegistry(reg *media.Registry) {
	if o == nil {
		return
	}
	o.mediaReg = reg
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
func (o *Orchestrator) SetToolResultEnqueuer(fn func(ctx context.Context, sessionID string) error) {
	o.enqueueToolResult = fn
}

// SetLifecycleMetadataProvider attaches Turn/Step projection metadata to
// outward SSE events without making the Orchestrator own SessionRuntime.
func (o *Orchestrator) SetLifecycleMetadataProvider(fn func(sessionID string) map[string]any) {
	if o == nil {
		return
	}
	o.lifecycleMetadata = fn
}

// SetLifecycleCommandSink observes durable Turn/Step boundaries from the
// actual model execution path. The SessionRuntime owns the coordinator; the
// Orchestrator only emits facts and never mutates lifecycle state directly.
func (o *Orchestrator) SetLifecycleCommandSink(fn LifecycleCommandSink) {
	if o == nil {
		return
	}
	o.lifecycleCommand = fn
}

// SetToolBudgetCheck attaches the runtime-owned preflight for a ToolBatch.
// The callback runs after ToolCall facts are recorded but before any tool side
// effect starts, preserving the durable proposed-before-executed invariant.
func (o *Orchestrator) SetToolBudgetCheck(fn func(sessionID string) (bool, string)) {
	if o == nil {
		return
	}
	o.toolBudgetCheck = fn
}

// SetToolRetryLimit bounds automatic retries for one ToolExecution. A retry
// keeps the original ToolCall ID and is only attempted when the executor's
// ToolRetryPolicy explicitly marks the tool as side-effect safe.
func (o *Orchestrator) SetToolRetryLimit(limit int) {
	if o == nil {
		return
	}
	if limit < 0 {
		limit = 0
	}
	o.toolRetryLimit = limit
}

// SetToolRetryCheck attaches the runtime-owned lifecycle budget preflight for
// a retry edge. It is separate from the initial tool-call check because a
// retry consumes a different budget dimension.
func (o *Orchestrator) SetToolRetryCheck(fn func(sessionID string) (bool, string)) {
	if o == nil {
		return
	}
	o.toolRetryCheck = fn
}

// SetModelRetryCheck applies Turn-level token/time/cost limits before another
// provider attempt is opened inside the current Step.
func (o *Orchestrator) SetModelRetryCheck(fn func(sessionID string) (bool, string)) {
	if o == nil {
		return
	}
	o.modelRetryCheck = fn
}

func (o *Orchestrator) emitLifecycleCommand(ctx context.Context, sessionID string, command TurnCommand) error {
	if o == nil || o.lifecycleCommand == nil {
		return nil
	}
	if command.SessionID == "" {
		command.SessionID = sessionID
	}
	if command.At.IsZero() {
		command.At = time.Now().UTC()
	}
	if command.TurnID == "" || command.Generation == 0 {
		execution, _ := ExecutionContextFromContext(ctx)
		if command.TurnID == "" {
			command.TurnID = execution.TurnID
		}
		if command.Generation == 0 {
			command.Generation = execution.Generation
		}
		if command.StepID == "" {
			command.StepID = execution.StepID
		}
	}
	return o.lifecycleCommand(sessionID, command)
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
	userMsg llm.Message,
) StepOutcome {
	if userMsg.Role == "" {
		userMsg.Role = "user"
	}
	o.clearModelContextSnapshot(sessionID)
	o.appendHistory(sessionID, history, userMsg)
	summary := llm.MessageTextSummary(userMsg)
	o.runMessageEnqueuedPhase(ctx, sessionID, history, summary, map[string]any{
		"source":      userMsg.Name,
		"has_images":  llm.MessageHasImages(userMsg),
		"content_len": len(summary),
	})
	o.resetTurnUsage(sessionID)
	o.resetContextMetrics(sessionID)
	o.resetHookHostLLMQuota()
	o.logger.Info("turn human message start",
		"session_id", sessionID,
		"content_len", len(summary),
		"has_images", llm.MessageHasImages(userMsg),
		"user_name", llm.NormalizeUserMessageName(userMsg.Name),
	)
	return o.runOneStep(ctx, sessionID, history)
}

// RunToolMessageTurn 在 history 已含 tool 结果后执行单步模型回合（tool_message，不追加 user）。
func (o *Orchestrator) RunToolMessageTurn(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
) StepOutcome {
	if strings.TrimSpace(RuntimeToolMessageContent) == "" {
		return StepOutcome{StepIndex: StepIndexFromContext(ctx), Err: fmt.Errorf("missing tool_message content")}
	}
	stepIndex := StepIndexFromContext(ctx)
	o.logger.Info("turn tool message start", "session_id", sessionID, "step_index", stepIndex)
	return o.runOneStep(ctx, sessionID, history)
}

// ContinueAfterResume 在 Client 提交 resume 后写入 tool 结果并调度 tool_result 续跑。
func (o *Orchestrator) ContinueAfterResume(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
	resumeValue map[string]any,
	pending *PendingHITL,
) StepOutcome {
	stepIndex := StepIndexFromContext(ctx)
	if pending == nil {
		return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("no pending hitl")}
	}
	resumeKind := strings.TrimSpace(fmt.Sprint(resumeValue["type"]))
	o.runHITLAfterResumePhase(ctx, sessionID, history, resumeKind)
	resumeToolCallID := strings.TrimSpace(fmt.Sprint(resumeValue["tool_call_id"]))
	pendingToolCallID := ""
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
		return o.continueAfterUserInformationResume(ctx, sessionID, history, resumeValue, pending, stepIndex)
	case "memory_conflict":
		return o.continueAfterMemoryConflictResume(ctx, sessionID, history, resumeValue, pending, stepIndex)
	case "approval":
		return o.continueAfterApprovalResume(ctx, sessionID, history, resumeValue, pending, stepIndex)
	default:
		return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("unsupported resume type")}
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
		agentID:         agentID,
		fsRoot:          fsRoot,
		hub:             hub,
		llm:             client,
		tools:           toolExec,
		policy:          policyEngine,
		toolHooks:       toolHooks,
		toolExecLog:     toolExecLog,
		skillAccess:     skillAccess,
		hookRuntimeCfg:  hookCfg,
		maxToolLoops:    maxToolLoops,
		modelRetryLimit: 2,
		toolRetryLimit:  1,
		promptCtx:       promptCtx,
		journal:         journal,
		logger:          logx.OrDefault(logger),
		ctxMetrics:      newContextMetricsStore(),
		modelSnapshots:  newModelContextSnapshotStore(),
		summaryNext:     make(map[string]bool),
	}
	orch.executionGuard = executionGuardFunc(orch.evaluateToolBeforeEach)
	registerSystemPromptBuildHook(orch)
	return orch
}

// SetNextStepFinalSummary marks the next model request as the reserved
// no-tools summary step. The flag is consumed exactly once at Step start.
func (o *Orchestrator) SetNextStepFinalSummary(sessionID string) {
	if o == nil {
		return
	}
	o.summaryMu.Lock()
	if o.summaryNext == nil {
		o.summaryNext = make(map[string]bool)
	}
	o.summaryNext[sessionID] = true
	o.summaryMu.Unlock()
}

func (o *Orchestrator) consumeNextStepFinalSummary(sessionID string) bool {
	if o == nil {
		return false
	}
	o.summaryMu.Lock()
	defer o.summaryMu.Unlock()
	marked := o.summaryNext[sessionID]
	delete(o.summaryNext, sessionID)
	return marked
}

// InterruptPending 在用户插入新 message 时打断 pending tool calls。
func (o *Orchestrator) InterruptPending(sessionID string, history *[]llm.Message, pending *PendingHITL) {
	o.InterruptPendingWithReason(
		sessionID,
		history,
		pending,
		ToolUserInterruptedMessage,
		map[string]any{"interrupted_by_user_message": true},
	)
}

// InterruptPendingWithReason 以自定义文案/元数据打断 pending tool calls。
func (o *Orchestrator) InterruptPendingWithReason(
	sessionID string,
	history *[]llm.Message,
	pending *PendingHITL,
	message string,
	meta map[string]any,
) {
	if pending == nil {
		return
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = ToolUserInterruptedMessage
	}
	if meta == nil {
		meta = map[string]any{"interrupted_by_user_message": true}
	}
	o.insertMissingToolResponsesAfterAssistant(
		sessionID,
		history,
		pending.AllToolCalls(),
		msg,
		meta,
	)
}

func (o *Orchestrator) runOneStep(
	ctx context.Context,
	sessionID string,
	history *[]llm.Message,
) StepOutcome {
	stepIndex := StepIndexFromContext(ctx)
	o.RepairUnrespondedToolCalls(sessionID, history)
	o.runTurnBeforeStepPhase(ctx, sessionID, history, "model_step", stepIndex)
	finalSummary := o.consumeNextStepFinalSummary(sessionID)
	finishReason := "stop"
	var streamErr error
	o.recordToolLoop(sessionID, stepIndex)
	// 超过 maxToolLoops 后不再硬失败：本步禁用 tools，若模型仍发起 tool_calls 则写入 soft tool_result，
	// 让模型给出结论并询问用户；下一条 human Turn 会重新从 Step 1 开始。
	overToolBudget := stepIndex > o.maxToolLoops || finalSummary

	var toolDefs []tools.ToolDef
	var systemPrompt string
	var msgs []llm.Message
	var hookErr error
	snapshot := o.ModelContextSnapshot(sessionID)
	if snapshot != nil {
		// A hard tool-loop budget is an execution safeguard, not a new runtime
		// configuration. Keep the prompt snapshot but suppress tools for this
		// final model request.
		systemPrompt = snapshot.SystemPrompt
		toolDefs = append([]tools.ToolDef(nil), snapshot.ToolDefinitions...)
		if overToolBudget {
			toolDefs = nil
		}
		msgs = append([]llm.Message(nil), (*history)...)
	} else {
		toolDefs = o.ToolDefinitions()
		if overToolBudget {
			toolDefs = nil
		}
		systemPrompt = o.buildSystemPrompt(sessionID)
		msgs, systemPrompt, hookErr = o.runLLMBeforeCallPhase(ctx, sessionID, history, systemPrompt)
		if hookErr != nil {
			o.runTurnErrorPhase(ctx, sessionID, history, hookErr)
			o.publishError(sessionID, hookErr.Error())
			o.publishDone(sessionID, "error")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: hookErr}
		}
		snapshot = NewModelContextSnapshot(systemPrompt, toolDefs, o.runtimeRevision, o.runtimeDigest)
		o.setModelContextSnapshot(sessionID, snapshot)
	}
	*history = msgs
	llmMessages := media.ExpandMessagesForLLM(*history, o.mediaReg)
	requestAt := time.Now().UTC()
	if snapshot != nil {
		if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type:            CommandTurnSnapshotCreated,
			At:              requestAt,
			RuntimeRevision: snapshot.RuntimeRevision,
			RuntimeDigest:   snapshot.RuntimeDigest,
			PromptDigest:    snapshot.PromptDigest,
			ToolDigest:      snapshot.ToolDigest,
			ContextSnapshot: snapshot.Clone(),
			Reason:          "model_context_snapshot_created",
		}); err != nil {
			o.runTurnErrorPhase(ctx, sessionID, history, err)
			o.publishError(sessionID, err.Error())
			o.publishDone(sessionID, "error")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("record turn snapshot: %w", err)}
		}
	}
	requestDigest := Digest(struct {
		SystemPrompt string
		Messages     []llm.Message
		Tools        []tools.ToolDef
	}{systemPrompt, llmMessages, toolDefs})
	result, err := o.runModelRequest(ctx, sessionID, systemPrompt, llmMessages, toolDefs, requestDigest, stepIndex)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
				Type:      CommandModelRequestFailed,
				At:        time.Now().UTC(),
				ErrorKind: modelErrorKind(err),
				Reason:    err.Error(),
			})
		}
		if errors.Is(err, context.Canceled) {
			finishReason = "cancelled"
			streamErr = err
			o.runTurnCancelPhase(ctx, sessionID, history, "llm_stream_cancelled")
			o.logger.Info("turn llm cancelled", "session_id", sessionID, "step_index", stepIndex)
			o.persistCancelledStream(sessionID, history, result)
		} else {
			o.runTurnErrorPhase(ctx, sessionID, history, err)
			o.publishError(sessionID, err.Error())
			finishReason = "error"
			streamErr = err
			o.logger.Error("turn llm failed", "session_id", sessionID, "step_index", stepIndex, "error", err)
		}
		if finishReason == "cancelled" {
			o.publishUsageIfAccumulated(sessionID, stepIndex)
		}
		o.publishDone(sessionID, finishReason)
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: streamErr}
	}
	if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
		Type:   CommandModelResponseCompleted,
		At:     time.Now().UTC(),
		Reason: "model_response_completed",
	}); err != nil {
		o.runTurnErrorPhase(ctx, sessionID, history, err)
		o.publishError(sessionID, err.Error())
		o.publishDone(sessionID, "error")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("record model response: %w", err)}
	}

	result, hookErr = o.runLLMAfterCallPhase(ctx, sessionID, result)
	if hookErr != nil {
		o.runTurnErrorPhase(ctx, sessionID, history, hookErr)
		msg := hookErr.Error()
		if isLLMAfterCallAbort(hookErr) {
			o.logger.Warn("llm.after_call aborted turn", "session_id", sessionID, "error", hookErr)
		} else {
			o.logger.Warn("llm.after_call hook failed", "session_id", sessionID, "error", hookErr)
		}
		o.publishError(sessionID, msg)
		o.publishDone(sessionID, "error")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: hookErr}
	}

	assistant := assistantMessageFromResult(result)
	o.appendHistory(sessionID, history, assistant)
	if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
		Type:               CommandAssistantReceived,
		At:                 time.Now().UTC(),
		HasTools:           len(result.ToolCalls) > 0,
		AssistantMessageID: Digest(assistant),
		Reason:             "assistant_message_recorded",
	}); err != nil {
		o.runTurnErrorPhase(ctx, sessionID, history, err)
		o.publishError(sessionID, err.Error())
		o.publishDone(sessionID, "error")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("record assistant message: %w", err)}
	}
	for _, toolCall := range result.ToolCalls {
		if strings.TrimSpace(toolCall.ID) == "" {
			continue
		}
		if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type:       CommandToolCallRecorded,
			At:         time.Now().UTC(),
			ToolCallID: toolCall.ID,
			ToolName:   toolCall.Function.Name,
			Arguments:  []byte(toolCall.Function.Arguments),
			Reason:     "tool_call_recorded_before_execution",
		}); err != nil {
			o.runTurnErrorPhase(ctx, sessionID, history, err)
			o.publishError(sessionID, err.Error())
			o.publishDone(sessionID, "error")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("record tool call: %w", err)}
		}
	}

	if len(result.ToolCalls) == 0 {
		o.publishDone(sessionID, finishReason)
		o.logger.Info("turn done", "session_id", sessionID, "finish_reason", finishReason, "step_index", stepIndex)
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex}
	}

	if stepIndex > o.maxToolLoops {
		o.appendMissingToolResponses(
			sessionID,
			history,
			result.ToolCalls,
			ToolLoopLimitExceededMessage,
			map[string]any{"tool_loop_limit_exceeded": true, "max_tool_loops": o.maxToolLoops},
		)
		o.logger.Info(
			"tool loop soft limit",
			"session_id", sessionID,
			"step_index", stepIndex,
			"max_tool_loops", o.maxToolLoops,
			"tool_calls", len(result.ToolCalls),
		)
		// 已超额一步仍反复 tool_calls 时收束，避免 soft-reject 死循环。
		if stepIndex > o.maxToolLoops+1 {
			o.publishDone(sessionID, finishReason)
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex}
		}
		if o.enqueueToolResult != nil {
			if err := o.enqueueToolResult(ctx, sessionID); err != nil {
				o.clearModelContextSnapshot(sessionID)
				return StepOutcome{StepIndex: stepIndex, Err: err}
			}
			return StepOutcome{StepIndex: stepIndex}
		}
		return StepOutcome{StepIndex: stepIndex, ScheduleToolResult: true}
	}

	pending, pauseReason, procErr := o.processToolCalls(ctx, sessionID, history, result.ToolCalls)
	if procErr != nil {
		if errors.Is(procErr, context.Canceled) {
			finishReason = "cancelled"
			o.runTurnCancelPhase(ctx, sessionID, history, "tool_processing_cancelled")
			o.appendMissingToolResponses(sessionID, history, result.ToolCalls, ToolStreamInterruptedMessage, map[string]any{"interrupted_by_stream_cancel": true})
			o.publishUsageIfAccumulated(sessionID, stepIndex)
		} else {
			o.runTurnErrorPhase(ctx, sessionID, history, procErr)
			finishReason = "error"
			o.publishError(sessionID, procErr.Error())
		}
		o.publishDone(sessionID, finishReason)
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: procErr}
	}
	if pending != nil {
		o.publishDone(sessionID, pauseReason)
		o.logger.Info("turn paused", "session_id", sessionID, "finish_reason", pauseReason, "step_index", stepIndex)
		return StepOutcome{Pending: pending, StepIndex: stepIndex}
	}
	if o.enqueueToolResult != nil {
		if err := o.enqueueToolResult(ctx, sessionID); err != nil {
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: err}
		}
		return StepOutcome{StepIndex: stepIndex}
	}
	return StepOutcome{StepIndex: stepIndex, ScheduleToolResult: true}
}

func (o *Orchestrator) setModelContextSnapshot(sessionID string, snapshot *ModelContextSnapshot) {
	if o == nil {
		return
	}
	if o.modelSnapshots == nil {
		o.modelSnapshots = newModelContextSnapshotStore()
	}
	o.modelSnapshots.set(sessionID, snapshot)
}

func waitModelRetry(ctx context.Context, attempt int) error {
	// Keep retries bounded and cancellation-aware. The small exponential delay
	// avoids hammering a provider during a transient outage without adding a
	// visible delay to normal successful Steps.
	delay := 100 * time.Millisecond * time.Duration(1<<(attempt-1))
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func isTransientModelError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"timeout", "temporar", "try again", "rate limit", "too many requests",
		"429", "502", "503", "504", "connection reset", "connection refused",
		"broken pipe", "eof", "service unavailable",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func modelErrorKind(err error) string {
	if err == nil {
		return ""
	}
	if isTransientModelError(err) {
		return "transient_provider_error"
	}
	return "provider_error"
}

func (o *Orchestrator) clearModelContextSnapshot(sessionID string) {
	if o == nil || o.modelSnapshots == nil {
		return
	}
	o.modelSnapshots.clear(sessionID)
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
	if snapshot := o.ModelContextSnapshot(sessionID); snapshot != nil {
		return snapshot.SystemPrompt
	}
	return o.buildSystemPrompt(sessionID)
}

// ToolDefinitions 返回与 runOneStep 相同的 tools 列表（侧车压缩前缀对齐用）。
func (o *Orchestrator) ToolDefinitions() []tools.ToolDef {
	if o == nil || o.tools == nil {
		return nil
	}
	return o.tools.Definitions()
}

// ToolDefinitionsForSession returns the active Turn schema when a Turn
// snapshot exists. Compression and diagnostics should use this form so a
// skill/MCP change during a running Turn cannot make the sidecar diverge from
// the main model request.
func (o *Orchestrator) ToolDefinitionsForSession(sessionID string) []tools.ToolDef {
	if snapshot := o.ModelContextSnapshot(sessionID); snapshot != nil {
		return cloneToolDefinitions(snapshot.ToolDefinitions)
	}
	return o.ToolDefinitions()
}

// ToolRegistry 在 Executor 为 *tools.Registry 时返回，供 UI 同步控制 bash。
func (o *Orchestrator) ToolRegistry() *tools.Registry {
	if o == nil || o.tools == nil {
		return nil
	}
	if reg, ok := o.tools.(*tools.Registry); ok {
		return reg
	}
	return nil
}

func (o *Orchestrator) buildSystemPrompt(sessionID string) string {
	if o.toolHooks == nil {
		return o.composeSystemPrompt(sessionID)
	}
	hc := hooks.BuildPromptBuildContext(sessionID, o.agentID, "")
	out, err := o.runPhase(context.Background(), hooks.PhasePromptBuild, hc, sessionID, nil, "")
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
	_, _ = o.runPhase(context.Background(), hooks.PhaseTurnDone, hc, sessionID, nil, finishReason)
}

// ReloadLongTermMemory 从持久化存储重新加载长期记忆并注入 prompt（清空上下文 / 首条交互 / 压缩完成后调用）。
func (o *Orchestrator) ReloadLongTermMemory(ctx context.Context) {
	if o == nil {
		return
	}
	if o.longTermStore == nil {
		if o.promptCtx != nil {
			o.promptCtx.UpdateLongTerm("")
		}
		return
	}
	snap, err := o.longTermStore.ReadLongTerm(ctx)
	if err != nil {
		if o.logger != nil {
			o.logger.Warn("reload long-term memory failed", "agent_id", o.agentID, "error", err)
		}
		return
	}
	if o.promptCtx != nil {
		o.promptCtx.UpdateLongTerm(FormatLongTermEntries(snap.Entries))
	}
}

func (o *Orchestrator) composeSystemPrompt(sessionID string) string {
	var loaded []skills.LoadedSkill
	if o.skillAccess.Get != nil {
		loaded = o.skillAccess.Get()
	}
	in := SystemPromptInput{
		AgentID:               o.agentID,
		FSRoot:                o.fsRoot,
		SessionID:             sessionID,
		Catalog:               o.skillAccess.Catalog,
		Loaded:                loaded,
		PromptCtx:             o.promptCtx,
		IncludeHistoryJournal: o.journal != nil && o.journal.Enabled(),
	}
	if o.systemPromptBuilder != nil {
		return o.systemPromptBuilder(in)
	}
	return BuildSystemPrompt(in)
}
