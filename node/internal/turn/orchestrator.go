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
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
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
	// Catalog is the frozen metadata/body view for the active human Turn.
	Catalog *skills.Catalog
	// LiveCatalog is used only by list_available_skills so a directory change
	// can be inspected without rewriting the active system prompt.
	LiveCatalog       *skills.Catalog
	Get               func() []skills.LoadedSkill
	Set               func([]skills.LoadedSkill)
	SetWithHookStatus func([]skills.LoadedSkill) SkillHooksSyncResult
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
	workspaceRoot   string
	runtimeRoot     string
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
	memoryService   memory.Service
	// memoryAutoRecall is separate from the memory tool group: an Agent may
	// receive automatic context while the model-facing memory tools remain
	// disabled, or expose tools without automatic recall.
	memoryAutoRecall       bool
	memoryCoreBudgetTokens int
	journal                *historypkg.Journal
	logger                 *slog.Logger

	childMgr       *childagent.Manager
	isChildSession bool

	turnUsageMu sync.Mutex
	turnUsage   map[string]llm.Usage
	// turnUsageLast stores the last provider snapshot for each model step.
	// Providers may emit cumulative usage more than once during a stream.
	turnUsageLast map[string]map[int]llm.Usage
	summaryMu     sync.Mutex
	summaryNext   map[string]bool

	ctxMetrics *contextMetricsStore

	modelSnapshots    *modelContextSnapshotStore
	contextMutationMu sync.Mutex
	contextMutations  map[string][]ContextMutation
	runtimeRevision   int64
	runtimeDigest     string
	executionGuard    ExecutionGuard

	systemPromptBuilder     SystemPromptBuilder
	contextInjectionBuilder ContextInjectionBuilder
	lifecycleMetadata       func(sessionID string) map[string]any
	lifecycleCommand        LifecycleCommandSink
	toolBudgetCheck         func(sessionID string) (bool, string)
	toolRetryCheck          func(sessionID string) (bool, string)
	modelRetryCheck         func(sessionID string) (bool, string)

	multimodalEnabled bool
	mediaReg          *media.Registry
}

// SetRuntimeRoot separates Node-managed runtime assets from the Agent
// workspace used by tools and the model-facing workspace description.
func (o *Orchestrator) SetRuntimeRoot(root string) {
	if o == nil {
		return
	}
	o.runtimeRoot = strings.TrimSpace(root)
}

// SetHookHostConfig 注入 Host 路径与配额配置。
func (o *Orchestrator) SetHookHostConfig(cfg HookHostConfig) {
	if o == nil {
		return
	}
	o.hookHostCfg = cfg.normalized()
	if o.hookHostState != nil {
		o.hookHostState.mu.Lock()
		o.hookHostState.workspaceRoot = o.workspaceRoot
		o.hookHostState.mu.Unlock()
	}
}

// SetSystemPromptBuilder 注入 system prompt 构造器；nil 时使用默认 BuildSystemPrompt。
func (o *Orchestrator) SetSystemPromptBuilder(fn SystemPromptBuilder) {
	o.systemPromptBuilder = fn
}

// SetContextInjectionBuilder 注入动态上下文构造器；nil 时使用默认
// BuildContextInjections。子 Agent 使用它来限制注入范围。
func (o *Orchestrator) SetContextInjectionBuilder(fn ContextInjectionBuilder) {
	if o == nil {
		return
	}
	o.contextInjectionBuilder = fn
}

// SetSkillsCatalog replaces the model-facing Catalog view at an explicit
// human-Turn or control-plane context boundary. It must not be called while a
// model request is being built; the runtime invokes it before the next model
// Step so the active snapshot remains immutable.
func (o *Orchestrator) SetSkillsCatalog(catalog *skills.Catalog) {
	if o == nil {
		return
	}
	o.skillAccess.Catalog = catalog
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

// SetMemoryService binds the v2 memory service. The legacy LongTermStore is
// kept separately during migration so existing callers and stored records can
// continue to be read without changing Turn lifecycle semantics.
func (o *Orchestrator) SetMemoryService(service memory.Service) {
	if o == nil {
		return
	}
	o.memoryService = service
}

// SetMemoryAutoRecall controls whether a fresh model-context boundary performs
// automatic memory recall. It does not affect the availability of memory
// tools, which is controlled by the Agent tool group.
func (o *Orchestrator) SetMemoryAutoRecall(enabled bool) {
	if o == nil {
		return
	}
	o.memoryAutoRecall = enabled
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

// RequestModelContextRefresh schedules a new model context snapshot at the
// next model Step. It never mutates the active request in place, so an
// in-flight model attempt and its streamed history remain stable.
func (o *Orchestrator) RequestModelContextRefresh(sessionID, reason string) {
	if o == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "context_mutation"
	}
	o.contextMutationMu.Lock()
	if o.contextMutations == nil {
		o.contextMutations = make(map[string][]ContextMutation)
	}
	o.contextMutations[sessionID] = appendContextMutation(o.contextMutations[sessionID], reason)
	o.contextMutationMu.Unlock()
}

// consumeModelContextRefresh keeps the lifecycle/wire-compatible string at
// the boundary. Internally, distinct invalidation causes are stored as typed
// mutations so callers do not need to parse a delimiter-based field.
func (o *Orchestrator) consumeModelContextRefresh(sessionID string) string {
	if o == nil {
		return ""
	}
	o.contextMutationMu.Lock()
	defer o.contextMutationMu.Unlock()
	mutations := o.contextMutations[sessionID]
	delete(o.contextMutations, sessionID)
	return contextMutationReasons(mutations)
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
		"source_kind": llm.EffectiveMessageSource(userMsg).Kind,
		"source_form": llm.EffectiveMessageSource(userMsg).Form,
		"provenance":  llm.EffectiveMessageProvenance(userMsg),
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
		"source_kind", llm.EffectiveMessageSource(userMsg).Kind,
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

// ContinueAfterResume 在 Client 提交 resume 后写入 tool 结果并返回继续执行结果。
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
	agentID, workspaceRoot string,
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
	if strings.TrimSpace(hookCfg.ToolResult.WorkspaceRoot) == "" {
		hookCfg.ToolResult.WorkspaceRoot = workspaceRoot
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
		agentID:          agentID,
		workspaceRoot:    workspaceRoot,
		hub:              hub,
		llm:              client,
		tools:            toolExec,
		policy:           policyEngine,
		toolHooks:        toolHooks,
		toolExecLog:      toolExecLog,
		skillAccess:      skillAccess,
		hookRuntimeCfg:   hookCfg,
		maxToolLoops:     maxToolLoops,
		modelRetryLimit:  2,
		toolRetryLimit:   1,
		memoryAutoRecall: true,
		promptCtx:        promptCtx,
		journal:          journal,
		logger:           logx.OrDefault(logger),
		ctxMetrics:       newContextMetricsStore(),
		turnUsage:        make(map[string]llm.Usage),
		turnUsageLast:    make(map[string]map[int]llm.Usage),
		modelSnapshots:   newModelContextSnapshotStore(),
		contextMutations: make(map[string][]ContextMutation),
		summaryNext:      make(map[string]bool),
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

// CancelPendingToolCalls closes pending tool calls as part of an explicit
// Turn cancellation. Ordinary input never calls this method.
func (o *Orchestrator) CancelPendingToolCalls(
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
		meta = map[string]any{"interrupted_by_turn_cancel": true}
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
	// Skill bodies are activated as durable, independent context messages.
	// Ensure this happens before the first snapshot/hook-visible model request,
	// while the active Catalog view is still the Turn-frozen source of truth.
	o.ensureLoadedSkillInstructions(sessionID, history)
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
	var requestHistory []llm.Message
	var hookErr error
	var recalledMemory *memory.Snapshot
	contextMutationReason := o.consumeModelContextRefresh(sessionID)
	contextReplaced := false
	snapshot := o.ModelContextSnapshot(sessionID)
	if contextMutationReason != "" && snapshot != nil {
		o.clearModelContextSnapshot(sessionID)
		contextReplaced = true
		snapshot = nil
	}
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
		requestHistory = append([]llm.Message(nil), msgs...)
	} else {
		toolDefs = o.ToolDefinitions()
		if overToolBudget {
			toolDefs = nil
		}
		// Build one input for the whole request snapshot. In particular, the
		// date must not be read twice around midnight and produce a system
		// prompt/context mismatch.
		promptInput := o.systemPromptInput(sessionID)
		systemPrompt = o.buildSystemPromptWithInput(sessionID, promptInput)
		injections := o.buildContextInjectionsWithInput(promptInput)
		var memoryInjection *ContextInjection
		recalledMemory, memoryInjection = o.buildMemoryInjection(ctx, sessionID, *history)
		if memoryInjection != nil {
			injections = append(injections, *memoryInjection)
		}
		hookHistory := ApplyContextInjections(append([]llm.Message(nil), (*history)...), injections)
		msgs, systemPrompt, hookErr = o.runLLMBeforeCallPhase(ctx, sessionID, &hookHistory, systemPrompt)
		if hookErr != nil {
			o.runTurnErrorPhase(ctx, sessionID, history, hookErr)
			o.publishError(sessionID, hookErr.Error())
			o.publishTurnFinished(sessionID, "error")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: hookErr}
		}
		msgs = StripContextInjections(msgs)
		snapshot = NewModelContextSnapshotWithInjections(systemPrompt, toolDefs, injections, o.runtimeRevision, o.runtimeDigest)
		o.attachSkillsSnapshotMetadata(snapshot)
		if recalledMemory != nil {
			snapshot.MemorySnapshotID = recalledMemory.ID
			snapshot.MemoryStoreRevision = recalledMemory.StoreRevision
			snapshot.MemoryDigest = recalledMemory.Digest
			snapshot.MemoryCoreCount = len(recalledMemory.Core)
			snapshot.MemoryRecallCount = len(recalledMemory.Recalled)
			snapshot.MemoryEstimatedTokens = recalledMemory.TokenEstimate
		}
		o.setModelContextSnapshot(sessionID, snapshot)
		requestHistory = append([]llm.Message(nil), msgs...)
	}
	*history = msgs
	var snapshotInjections []ContextInjection
	if snapshot != nil {
		snapshotInjections = snapshot.ContextInjections
	}
	requestHistory = ApplyContextInjections(requestHistory, snapshotInjections)
	requestHistory = StripLegacyTodayDateMessages(requestHistory)
	requestHistory = o.filterSkillInstructionMessages(requestHistory)
	llmMessages := media.ExpandMessagesForLLM(requestHistory, o.mediaReg)
	if !o.multimodalEnabled {
		// The history may have been created while multimodal was enabled.
		// Keep those image parts durable for the UI, but never send them to the
		// model while the Agent setting is disabled.
		llmMessages = llm.PrepareMessagesForTextOnly(llmMessages)
	}
	// History/transcript retains the original tool body, while the model gets
	// the authoritative status projection in a request-only copy.
	llmMessages = llm.PrepareToolResultMessagesForModel(llmMessages)
	requestAt := time.Now().UTC()
	if snapshot != nil {
		commandType := CommandTurnSnapshotCreated
		reason := "model_context_snapshot_created"
		if contextReplaced {
			commandType = CommandModelContextChanged
			reason = "model_context_changed:" + contextMutationReason
		}
		if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type:            commandType,
			At:              requestAt,
			RuntimeRevision: snapshot.RuntimeRevision,
			RuntimeDigest:   snapshot.RuntimeDigest,
			PromptDigest:    snapshot.PromptDigest,
			ToolDigest:      snapshot.ToolDigest,
			ContextSnapshot: snapshot.Clone(),
			Reason:          reason,
		}); err != nil {
			o.runTurnErrorPhase(ctx, sessionID, history, err)
			o.publishError(sessionID, err.Error())
			o.publishTurnFinished(sessionID, "error")
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
		o.publishTurnFinished(sessionID, finishReason)
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
		o.publishTurnFinished(sessionID, "error")
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
		o.publishTurnFinished(sessionID, "error")
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
		o.publishTurnFinished(sessionID, "error")
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
			o.publishTurnFinished(sessionID, "error")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("record tool call: %w", err)}
		}
	}

	if len(result.ToolCalls) == 0 {
		o.publishTurnFinished(sessionID, finishReason)
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
			o.publishTurnFinished(sessionID, finishReason)
			o.clearModelContextSnapshot(sessionID)
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
		o.publishTurnFinished(sessionID, finishReason)
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: procErr}
	}
	if pending != nil {
		o.logger.Info("turn paused", "session_id", sessionID, "finish_reason", pauseReason, "step_index", stepIndex)
		return StepOutcome{Pending: pending, StepIndex: stepIndex}
	}
	return StepOutcome{StepIndex: stepIndex, ScheduleToolResult: true}
}

// attachSkillsSnapshotMetadata records the skill inputs that accompanied the
// frozen model context. The values are diagnostics only; the prompt and tool
// snapshots remain authoritative for the active Turn.
func (o *Orchestrator) attachSkillsSnapshotMetadata(snapshot *ModelContextSnapshot) {
	if o == nil || snapshot == nil {
		return
	}
	if o.skillAccess.Catalog != nil {
		snapshot.SkillsCatalogRevision = o.skillAccess.Catalog.Revision()
	}
	if o.skillAccess.Get != nil {
		loaded := o.skillAccess.Get()
		snapshot.LoadedSkillsDigest = Digest(loaded)
		if len(loaded) > 0 {
			// Do not persist or expose the body itself in lifecycle metadata. The
			// digest lets replay/diagnostics distinguish a body change from a
			// loaded-set change without changing the model-facing protocol.
			snapshot.LoadedSkillsContentDigest = Digest(o.activeSkillInstructionMessages())
		}
	}
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
	delete(o.turnUsageLast, sessionID)
	o.turnUsageMu.Unlock()
}

// resetUsageAttempt clears the provider snapshot cursor for one model step.
// A retry is a new provider completion and must not be treated as a
// continuation of the previous attempt's cumulative counters.
func (o *Orchestrator) resetUsageAttempt(sessionID string, llmStep int) {
	if o == nil {
		return
	}
	o.turnUsageMu.Lock()
	if o.turnUsageLast != nil {
		delete(o.turnUsageLast[sessionID], llmStep)
	}
	o.turnUsageMu.Unlock()
}

// SystemPromptForSession 返回当前 session 下一步 LLM 调用将使用的 system prompt。
func (o *Orchestrator) SystemPromptForSession(sessionID string) string {
	if snapshot := o.ModelContextSnapshot(sessionID); snapshot != nil {
		return snapshot.SystemPrompt
	}
	return o.buildSystemPrompt(sessionID)
}

// ContextInjectionsForSession returns the active Turn's frozen injections, or
// the next-request injections when the session is idle. It is intended for
// diagnostics and compression prefix construction; callers receive a copy.
func (o *Orchestrator) ContextInjectionsForSession(sessionID string) []ContextInjection {
	if o == nil {
		return nil
	}
	if snapshot := o.ModelContextSnapshot(sessionID); snapshot != nil {
		return cloneContextInjections(snapshot.ContextInjections)
	}
	return cloneContextInjections(o.buildContextInjections(sessionID))
}

// ToolDefinitions 返回与 runOneStep 相同的 tools 列表（侧车压缩前缀对齐用）。
func (o *Orchestrator) ToolDefinitions() []tools.ToolDef {
	if o == nil || o.tools == nil {
		return nil
	}
	defs := o.tools.Definitions()
	if o.skillAccess.Catalog == nil || !o.skillAccess.Catalog.Enabled() {
		return defs
	}
	// Discovery is a regular part of the Skills tool group. It is appended only
	// when load_skills is visible, so child/allowlisted registries keep the same
	// permission boundary as the other Skills tools.
	hasLoadSkills := false
	for _, def := range defs {
		if def.Function.Name == "load_skills" {
			hasLoadSkills = true
			break
		}
	}
	if !hasLoadSkills {
		return defs
	}
	listDef := tools.ListAvailableSkillsToolDef()
	listDef.Function.Description = strings.TrimSpace(listDef.Function.Description) + tools.ResultDescriptionSuffixForTool(listDef.Function.Name)
	defs = append(defs, listDef)
	return defs
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
	if o == nil {
		return ""
	}
	return o.buildSystemPromptWithInput(sessionID, o.systemPromptInput(sessionID))
}

func (o *Orchestrator) buildSystemPromptWithInput(sessionID string, in SystemPromptInput) string {
	if o.toolHooks == nil {
		return o.composeSystemPromptWithInput(sessionID, in)
	}
	basePrompt := o.composeSystemPromptWithInput(sessionID, in)
	hc := hooks.BuildPromptBuildContext(sessionID, o.agentID, basePrompt)
	out, err := o.runPhase(context.Background(), hooks.PhasePromptBuild, hc, sessionID, nil, "")
	if err != nil {
		return o.composeSystemPromptWithInput(sessionID, in)
	}
	prompt := hooks.SystemPromptFrom(out, "")
	if prompt == "" {
		return o.composeSystemPromptWithInput(sessionID, in)
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
	// v2 memory is recalled per new model-context boundary and follows the
	// current root user message. It must never be copied into the stable
	// prompt-sidecar reader or refreshed in place during compression/resume.
	if o.memoryService != nil {
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
	if o == nil {
		return ""
	}
	return o.composeSystemPromptWithInput(sessionID, o.systemPromptInput(sessionID))
}

func (o *Orchestrator) composeSystemPromptWithInput(sessionID string, in SystemPromptInput) string {
	if o.systemPromptBuilder != nil {
		return o.systemPromptBuilder(in)
	}
	return BuildSystemPrompt(in)
}

func (o *Orchestrator) systemPromptInput(sessionID string) SystemPromptInput {
	if o == nil {
		return SystemPromptInput{}
	}
	in := SystemPromptInput{
		AgentID:               o.agentID,
		WorkspaceRoot:         o.workspaceRoot,
		RuntimeRoot:           o.runtimeRoot,
		SessionID:             sessionID,
		TodayDateEnabled:      o.hookRuntimeCfg.InjectTodayDate.IsEnabled(),
		Catalog:               o.skillAccess.Catalog,
		PromptCtx:             o.promptCtx,
		IncludeHistoryJournal: o.journal != nil && o.journal.Enabled(),
	}
	if in.TodayDateEnabled {
		in.CurrentDate = time.Now().Format("20060102")
	}
	return in
}

func (o *Orchestrator) buildContextInjections(sessionID string) []ContextInjection {
	if o == nil {
		return nil
	}
	return o.buildContextInjectionsWithInput(o.systemPromptInput(sessionID))
}

func (o *Orchestrator) buildContextInjectionsWithInput(in SystemPromptInput) []ContextInjection {
	if o == nil {
		return nil
	}
	if o.contextInjectionBuilder != nil {
		return cloneContextInjections(o.contextInjectionBuilder(in))
	}
	return BuildContextInjections(in)
}
