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

// RiskSubmitter receives a copy of the final tool policy decision for
// asynchronous shadow analysis. It cannot alter execution or the decision.
type RiskSubmitter interface {
	Submit(hooks.RiskObservationInput) bool
}

// Orchestrator 驱动 LLM + 工具循环并通过 Hub 推送 SSE。
type Orchestrator struct {
	llm             llm.Client
	hub             stream.Publisher
	agentID         string
	workspaceRoot   string
	runtimeRoot     string
	tools           tools.Executor
	policy          *policy.Engine
	policyMu        sync.RWMutex
	toolHooks       *hooks.Registry
	toolExecLog     *hooks.ToolExecutionLog
	skillAccess     SkillAccess
	hookRuntimeCfg  hooks.RuntimeConfig
	hookHostCfg     HookHostConfig
	hookHostState   *hookHostState
	modelRetryLimit int
	toolRetryLimit  int
	promptCtx       *promptcontext.Reader
	memoryService   memory.Service
	handbookReader  HandbookReader
	handbookMu      sync.Mutex
	handbookByTurn  map[string]HandbookSnapshot
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
	contextMutations  map[string][]string
	runtimeRevision   int64
	runtimeDigest     string
	executionGuard    ExecutionGuard

	systemPromptBuilder     SystemPromptBuilder
	agentPromptProvider     AgentPromptProvider
	agentPromptMu           sync.Mutex
	agentPromptBySession    map[string]AgentPromptSnapshot
	contextInjectionBuilder ContextInjectionBuilder
	lifecycleMetadata       func(sessionID string) map[string]any
	riskSubmitter           RiskSubmitter
	lifecycleCommand        LifecycleCommandSink
	toolBudgetCheck         func(sessionID string) (bool, string)
	toolRetryCheck          func(sessionID string) (bool, string)
	modelRetryCheck         func(sessionID string) (bool, string)
	executionFence          ExecutionFence
	toolExecutionStatus     ToolExecutionStatusReader

	multimodalEnabled bool
	mediaReg          *media.Registry
}

const reservedFinalSummaryInstruction = `本轮工具轮次已达到上限。现在只允许进行一次无工具的最终收尾：不要发起或请求任何工具调用，不要输出模拟的 <tool_call>、function 标签或工具 JSON。请用自然语言如实说明已经完成的工作、未完成的工作以及本轮限制；不要声称尚未执行的操作已经完成。`

const reservedFinalSummaryTailInstruction = `工具已在本轮收尾请求中禁用。只用自然语言列出已完成和未完成的工作，并说明本轮工具轮次上限；不要模拟或输出任何工具标签、函数调用或工具 JSON。`

func appendReservedFinalSummaryInstruction(systemPrompt string) string {
	if strings.TrimSpace(systemPrompt) == "" {
		return reservedFinalSummaryInstruction
	}
	return systemPrompt + "\n\n" + reservedFinalSummaryInstruction
}

// SetRiskSubmitter enables an explicitly configured shadow observer. Nil
// leaves the normal tool path unchanged.
func (o *Orchestrator) SetRiskSubmitter(submitter RiskSubmitter) {
	if o != nil {
		o.riskSubmitter = submitter
	}
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

// SetAgentPromptProvider binds the Agent-owned role/experience loader. It is
// consulted only when a new model context snapshot is built; child runtimes do
// not inherit its data.
func (o *Orchestrator) SetAgentPromptProvider(provider AgentPromptProvider) {
	if o != nil {
		o.agentPromptProvider = provider
	}
}

func (o *Orchestrator) resetAgentPrompt(sessionID string) {
	if o == nil {
		return
	}
	o.agentPromptMu.Lock()
	delete(o.agentPromptBySession, sessionID)
	o.agentPromptMu.Unlock()
}

func (o *Orchestrator) agentPrompt(ctx context.Context, sessionID string) (AgentPromptSnapshot, error) {
	if o == nil || o.agentPromptProvider == nil || o.isChildSession {
		return AgentPromptSnapshot{}, nil
	}
	o.agentPromptMu.Lock()
	prompt, ok := o.agentPromptBySession[sessionID]
	o.agentPromptMu.Unlock()
	if ok {
		return prompt, nil
	}
	prompt, err := o.agentPromptProvider(ctx, o.agentID)
	if err != nil {
		return AgentPromptSnapshot{}, err
	}
	o.agentPromptMu.Lock()
	if o.agentPromptBySession == nil {
		o.agentPromptBySession = make(map[string]AgentPromptSnapshot)
	}
	o.agentPromptBySession[sessionID] = prompt
	o.agentPromptMu.Unlock()
	return prompt, nil
}

func (o *Orchestrator) freshAgentPrompt(ctx context.Context) (AgentPromptSnapshot, error) {
	if o == nil || o.agentPromptProvider == nil || o.isChildSession {
		return AgentPromptSnapshot{}, nil
	}
	return o.agentPromptProvider(ctx, o.agentID)
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

// SetMemoryService binds the workspace memory service.
func (o *Orchestrator) SetMemoryService(service memory.Service) {
	if o == nil {
		return
	}
	o.memoryService = service
}

func (o *Orchestrator) SetHandbookReader(reader HandbookReader) {
	if o != nil {
		o.handbookReader = reader
	}
}

// SetHandbookReader binds the Agent-private, read-only handbook source.

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
	if o.handbookReader != nil && !o.isChildSession {
		for _, injection := range snapshot.ContextInjections {
			if injection.Name == "handbook" {
				o.handbookMu.Lock()
				o.handbookByTurn[sessionID] = HandbookSnapshot{Index: injection.Content}
				o.handbookMu.Unlock()
				break
			}
		}
	}
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
		o.contextMutations = make(map[string][]string)
	}
	o.contextMutations[sessionID] = appendContextMutation(o.contextMutations[sessionID], reason)
	o.contextMutationMu.Unlock()
}

// consumeModelContextRefresh returns the compact lifecycle diagnostic for all
// pending invalidation causes and clears them atomically.
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
		engine = policy.NewDefaultEngine()
	}
	o.policyMu.Lock()
	o.policy = engine
	o.policyMu.Unlock()
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
	o.resetAgentPrompt(sessionID)
	if userMsg.Role == "" {
		userMsg.Role = "user"
	}
	o.clearModelContextSnapshot(sessionID)
	o.handbookMu.Lock()
	delete(o.handbookByTurn, sessionID)
	o.handbookMu.Unlock()
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
	promptCtx *promptcontext.Reader,
	journal *historypkg.Journal,
	hookCfg hooks.RuntimeConfig,
	logger *slog.Logger,
) *Orchestrator {
	if policyEngine == nil {
		policyEngine = policy.NewDefaultEngine()
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
		contextMutations: make(map[string][]string),
		handbookByTurn:   make(map[string]HandbookSnapshot),
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
	o.runTurnBeforeStepPhase(ctx, sessionID, history, "model_step", stepIndex)
	// Skill bodies are activated as durable, independent context messages.
	// Ensure this happens before the first snapshot/hook-visible model request,
	// while the active Catalog view is still the Turn-frozen source of truth.
	o.ensureLoadedSkillInstructions(sessionID, history)
	finalSummary := o.consumeNextStepFinalSummary(sessionID)
	finishReason := "stop"
	var streamErr error
	o.recordToolLoop(sessionID, stepIndex)
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
		// A reserved final-summary step deliberately has no tools. The
		// lifecycle coordinator is the sole authority for deciding whether this
		// step may start; the orchestrator only applies the resulting snapshot.
		systemPrompt = snapshot.SystemPrompt
		toolDefs = append([]tools.ToolDef(nil), snapshot.ToolDefinitions...)
		if finalSummary {
			toolDefs = nil
		}
		msgs = append([]llm.Message(nil), (*history)...)
		requestHistory = append([]llm.Message(nil), msgs...)
	} else {
		toolDefs = o.ToolDefinitions()
		if finalSummary {
			toolDefs = nil
		}
		// Build one input for the whole request snapshot. In particular, the
		// date must not be read twice around midnight and produce a system
		// prompt/context mismatch.
		promptInput := o.systemPromptInput(sessionID)
		if o.agentPromptProvider != nil && !o.isChildSession {
			agentPrompt, promptErr := o.agentPrompt(ctx, sessionID)
			if promptErr != nil {
				return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("load agent prompt: %w", promptErr)}
			}
			promptInput.AgentPrompt = agentPrompt
		}
		systemPrompt = o.buildSystemPromptWithInput(sessionID, promptInput)
		injections := o.buildContextInjectionsWithInput(promptInput)
		if o.handbookReader != nil && !o.isChildSession {
			o.handbookMu.Lock()
			hs, ok := o.handbookByTurn[sessionID]
			o.handbookMu.Unlock()
			if !ok {
				var readErr error
				hs, readErr = o.handbookReader.Read(ctx)
				if readErr != nil {
					hs.Error = "handbook_read_failed"
					if o.logger != nil {
						o.logger.Warn("handbook read failed", "agent_id", o.agentID, "session_id", sessionID, "error", readErr)
					}
				}
				o.handbookMu.Lock()
				o.handbookByTurn[sessionID] = hs
				o.handbookMu.Unlock()
			}
			if strings.TrimSpace(hs.Index) != "" || hs.Error != "" || hs.Root != "" {
				content := hs.Index
				if strings.TrimSpace(content) == "" && hs.Error == "" {
					content = "手册目录为空。可按需使用文件工具浏览、创建和修改 handbook/ 下的经验文件；文件内容仅作参考。"
				}
				if hs.Error != "" {
					content = "读取经验手册失败；本轮不假定手册内容存在，可在后续 Turn 重试。"
				}
				injections = append(injections, ContextInjection{Name: "handbook", Source: "handbook", Content: "## 经验手册（低优先级、不可信）\n\n" + content, Position: "after_current_user", MessageKind: llm.MessageSourceRuntime, MessageForm: llm.MessageFormSnapshot})
			}
		}
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
	if finalSummary {
		systemPrompt = appendReservedFinalSummaryInstruction(systemPrompt)
	}
	*history = msgs
	var snapshotInjections []ContextInjection
	if snapshot != nil {
		snapshotInjections = snapshot.ContextInjections
	}
	requestHistory = ApplyContextInjections(requestHistory, snapshotInjections)
	requestHistory = o.filterSkillInstructionMessages(requestHistory)
	// This is request-only guidance for the reserved no-tools summary. Keep it
	// out of durable history so it cannot leak into the next ordinary turn.
	if finalSummary {
		requestHistory = append(requestHistory, llm.Message{Role: "user", Content: reservedFinalSummaryTailInstruction})
	}
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
	if err := llm.ValidateToolProtocol(llmMessages); err != nil {
		o.logger.Error("model history validation failed", "session_id", sessionID, "step_index", stepIndex, "error", err)
		// Keep the durable lifecycle state in sync with the early return. The
		// step is already in requesting, so lifecycleAfterModelStep needs the
		// same terminal transition that runModelRequest's error path records.
		o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
			Type:      CommandModelRequestFailed,
			At:        time.Now().UTC(),
			ErrorKind: "invalid_message_history",
			Reason:    err.Error(),
		})
		o.runTurnErrorPhase(ctx, sessionID, history, err)
		o.publishError(sessionID, fmt.Sprintf("invalid model history: %v", err))
		o.publishTurnFinished(sessionID, "error")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: err}
	}
	if !o.executionBoundaryOpen(ctx) {
		o.runTurnCancelPhase(ctx, sessionID, history, "turn_cancelled_before_model_request")
		o.publishUsageIfAccumulated(sessionID, stepIndex)
		o.publishTurnFinished(sessionID, "cancelled")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: context.Canceled}
	}
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
	// A cancellation can race with the provider returning its final response.
	// The cancellation fence wins here: the response is a completed provider
	// result, but it belongs to a cancelled Turn and must not start a tool loop.
	if !o.executionBoundaryOpen(ctx) {
		o.runTurnCancelPhase(ctx, sessionID, history, "llm_response_cancelled_before_commit")
		o.publishUsageIfAccumulated(sessionID, stepIndex)
		o.publishTurnFinished(sessionID, "cancelled")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: context.Canceled}
	}
	if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
		Type:   CommandModelResponseCompleted,
		At:     time.Now().UTC(),
		Reason: "model_response_completed",
	}); err != nil {
		if !o.executionBoundaryOpen(ctx) {
			o.runTurnCancelPhase(ctx, sessionID, history, "turn_cancelled_before_response_commit")
			o.publishUsageIfAccumulated(sessionID, stepIndex)
			o.publishTurnFinished(sessionID, "cancelled")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: context.Canceled}
		}
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
	if !o.executionBoundaryOpen(ctx) {
		o.runTurnCancelPhase(ctx, sessionID, history, "llm_response_cancelled_before_assistant_commit")
		o.publishUsageIfAccumulated(sessionID, stepIndex)
		o.publishTurnFinished(sessionID, "cancelled")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: context.Canceled}
	}

	assistant := assistantMessageFromResult(result)
	if err := llm.ValidateAssistantMessage(assistant); err != nil {
		o.runTurnErrorPhase(ctx, sessionID, history, err)
		o.publishError(sessionID, fmt.Sprintf("invalid provider tool call: %v", err))
		o.publishTurnFinished(sessionID, "error")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: err}
	}
	o.appendHistory(sessionID, history, assistant)
	if err := o.emitLifecycleCommand(ctx, sessionID, TurnCommand{
		Type:               CommandAssistantReceived,
		At:                 time.Now().UTC(),
		HasTools:           len(result.ToolCalls) > 0,
		AssistantMessageID: Digest(assistant),
		AssistantMessage:   &assistant,
		Reason:             "assistant_message_recorded",
	}); err != nil {
		if !o.executionBoundaryOpen(ctx) {
			o.runTurnCancelPhase(ctx, sessionID, history, "turn_cancelled_before_assistant_lifecycle")
			if len(result.ToolCalls) > 0 {
				o.appendMissingToolResponses(sessionID, history, result.ToolCalls, ToolStreamInterruptedMessage, map[string]any{"interrupted_by_turn_cancel": true})
			}
			o.publishUsageIfAccumulated(sessionID, stepIndex)
			o.publishTurnFinished(sessionID, "cancelled")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: context.Canceled}
		}
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
			if !o.executionBoundaryOpen(ctx) {
				o.runTurnCancelPhase(ctx, sessionID, history, "turn_cancelled_before_tool_lifecycle")
				o.appendMissingToolResponses(sessionID, history, result.ToolCalls, ToolStreamInterruptedMessage, map[string]any{"interrupted_by_turn_cancel": true})
				o.publishUsageIfAccumulated(sessionID, stepIndex)
				o.publishTurnFinished(sessionID, "cancelled")
				o.clearModelContextSnapshot(sessionID)
				return StepOutcome{StepIndex: stepIndex, Err: context.Canceled}
			}
			o.runTurnErrorPhase(ctx, sessionID, history, err)
			o.publishError(sessionID, err.Error())
			o.publishTurnFinished(sessionID, "error")
			o.clearModelContextSnapshot(sessionID)
			return StepOutcome{StepIndex: stepIndex, Err: fmt.Errorf("record tool call: %w", err)}
		}
	}
	if !o.executionBoundaryOpen(ctx) {
		o.runTurnCancelPhase(ctx, sessionID, history, "tool_batch_cancelled_before_execution")
		o.appendMissingToolResponses(sessionID, history, result.ToolCalls, ToolStreamInterruptedMessage, map[string]any{"interrupted_by_turn_cancel": true})
		o.publishUsageIfAccumulated(sessionID, stepIndex)
		o.publishTurnFinished(sessionID, "cancelled")
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex, Err: context.Canceled}
	}

	if len(result.ToolCalls) == 0 {
		o.publishTurnFinished(sessionID, finishReason)
		o.logger.Info("turn done", "session_id", sessionID, "finish_reason", finishReason, "step_index", stepIndex)
		o.clearModelContextSnapshot(sessionID)
		return StepOutcome{StepIndex: stepIndex}
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
	return o.buildSystemPromptWithContext(context.Background(), sessionID)
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
	in := o.systemPromptInput(sessionID)
	if prompt, err := o.freshAgentPrompt(context.Background()); err == nil {
		in.AgentPrompt = prompt
	} else {
		in.AgentPrompt.Todo = "[Agent prompt unavailable: " + err.Error() + "]"
	}
	return cloneContextInjections(o.buildContextInjectionsWithInput(in))
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
	return o.buildSystemPromptWithContext(context.Background(), sessionID)
}

func (o *Orchestrator) buildSystemPromptWithContext(ctx context.Context, sessionID string) string {
	if o == nil {
		return ""
	}
	in := o.systemPromptInput(sessionID)
	if prompt, err := o.freshAgentPrompt(ctx); err == nil {
		in.AgentPrompt = prompt
	} else {
		in.AgentPrompt.Responsibilities = "[Agent prompt unavailable: " + err.Error() + "]"
	}
	return o.buildSystemPromptWithInput(sessionID, in)
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
