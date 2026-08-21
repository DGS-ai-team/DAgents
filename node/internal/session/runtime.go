package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/history"
	clihitl "github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/media"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func newContinuationID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("turn-%d", time.Now().UnixNano())
	}
	return "turn-" + hex.EncodeToString(b[:])
}

// Session 表示对外可见的 session 元数据。
type Session struct {
	ID      string
	AgentID string
}

type runtime struct {
	session Session
	// 消息队列
	queue *queue.MessageQueue
	// 编排器
	orch *turn.Orchestrator
	// New Turn/Step lifecycle coordinator; Orchestrator remains the execution
	// engine, while lifecycle authority lives entirely in this projection.
	turnCoordinator *turn.TurnCoordinator
	// 存储
	store *store.SQLiteStore
	// 事件中心
	hub *stream.Hub
	// publisher 是编排器实际使用的事件出口。父 runtime 通常与 hub 相同，
	// 子 runtime 则可能是 RelayHub；生命周期状态必须沿同一出口发布。
	publisher stream.Publisher
	// 代理 ID
	agentID string
	// 日志
	logger *slog.Logger

	// 技能目录
	skillsCatalog *skills.Catalog
	skillRevision string
	// 上下文压缩逻辑
	compression *compression.Coordinator

	started bool
	done    chan struct{}

	mu         sync.Mutex         // 互斥锁
	turnCancel context.CancelFunc // 取消 turn 上下文
	// sessionEpoch invalidates events queued before clear-context/rebuild.
	sessionEpoch uint64
	// lifecycleMu serializes compound Coordinator transitions. The
	// TurnCoordinator owns Turn/Step identity and generation; runtime keeps no
	// second lifecycle projection.
	lifecycleMu           sync.Mutex
	lifecycleCommandSeq   uint64
	lifecycleEventSeq     uint64
	lifecycleEventsLoaded bool
	messages              []llm.Message        // 交互消息列表
	loadedSkills          []skills.LoadedSkill // 加载的技能列表
	pendingLongTermScope  string               // scope changes wait for the next human Turn
	fsRoot                string               // 文件系统根路径
	media                 *media.Registry      // session 媒体索引（F-M1）

	triggerDelivery triggers.DeliveryTracker // trigger 消息投递跟踪器

	sideEffects *sideEffectStore // 旁路回灌缓冲（子 session 跳过）

	childMeta *childRuntimeMeta // 子 Agent 元数据

	idleAutoCompressApplied bool // 无动作自动压缩已完成；新对话时清除

	notifySeq int // F-E13：最后需 Client 关注的 SSE seq
	ackSeq    int // F-E13：Client 已确认看到的最大 SSE seq

	configRevision  int64 // 兼容旧观测字段；值与 runtimeRevision 一致
	runtimeRevision int64
	runtimeDigest   string
	turnBudget      turn.TurnBudget
}

// newRuntime 创建新的 session runtime
func newRuntime(
	id, agentID string,
	hub *stream.Hub,
	llmClient llm.Client,
	registry *tools.Registry,
	policyEngine *policy.Engine,
	st *store.SQLiteStore,
	logger *slog.Logger,
	initial []llm.Message,
	loaded []skills.LoadedSkill,
	initialPending *turn.PendingHITL,
	initialLoopCount int,
	initialHookStore map[string]json.RawMessage,
	idleAutoCompressApplied bool,
	initialNotifySeq int,
	initialAckSeq int,
	turnOpts TurnOptions,
	triggerDelivery triggers.DeliveryTracker,
) *runtime {
	return newRuntimeWithPublisher(id, agentID, hub, hub, llmClient, registry, policyEngine, st, logger,
		initial, loaded, initialPending, initialLoopCount, initialHookStore, idleAutoCompressApplied, initialNotifySeq, initialAckSeq, turnOpts, triggerDelivery)
}

// newRuntimeWithPublisher 创建新的 session runtime，并设置 publisher
func newRuntimeWithPublisher(
	id, agentID string,
	pub stream.Publisher,
	eventHub *stream.Hub,
	llmClient llm.Client,
	toolExec tools.Executor,
	policyEngine *policy.Engine,
	st *store.SQLiteStore,
	logger *slog.Logger,
	initial []llm.Message,
	loaded []skills.LoadedSkill,
	initialPending *turn.PendingHITL,
	initialLoopCount int,
	initialHookStore map[string]json.RawMessage,
	idleAutoCompressApplied bool,
	initialNotifySeq int,
	initialAckSeq int,
	turnOpts TurnOptions,
	triggerDelivery triggers.DeliveryTracker,
) *runtime {
	catalog := skills.NewCatalog(turnOpts.SkillsRoot, turnOpts.SkillsEnabled, turnOpts.SkillsMaxInPrompt)
	if turnOpts.SkillsVisibleRestrict {
		catalog.RestrictVisible(turnOpts.SkillsVisible)
	}
	journal := history.NewJournal(turnOpts.RawMessageHistoryEnabled, turnOpts.RawMessageHistoryDir, logger)
	rt := &runtime{
		session:         Session{ID: id, AgentID: agentID},
		queue:           queue.NewMessageQueue(),
		turnCoordinator: turn.NewTurnCoordinator(id, agentID),
		done:            make(chan struct{}),
		store:           st,
		hub:             eventHub,
		publisher:       pub,
		agentID:         agentID,
		logger:          logger,
		skillsCatalog:   catalog,
		compression: func() *compression.Coordinator {
			coord := compression.NewCoordinator(llmClient, turnOpts.CompressionSilent, turnOpts.CompressionBlocking)
			coord.SetLogger(logger)
			coord.SetRawMessageHistoryEnabled(turnOpts.RawMessageHistoryEnabled)
			return coord
		}(),
		messages:                append([]llm.Message(nil), initial...),
		loadedSkills:            append([]skills.LoadedSkill(nil), loaded...),
		skillRevision:           catalog.Revision(),
		fsRoot:                  turnOpts.FSRoot,
		triggerDelivery:         triggerDelivery,
		sideEffects:             newSideEffectStore(),
		idleAutoCompressApplied: idleAutoCompressApplied,
		notifySeq:               initialNotifySeq,
		ackSeq:                  initialAckSeq,
		configRevision:          firstNonZero(turnOpts.RuntimeRevision, turnOpts.ConfigRevision),
		runtimeRevision:         firstNonZero(turnOpts.RuntimeRevision, turnOpts.ConfigRevision),
		runtimeDigest:           strings.TrimSpace(turnOpts.RuntimeDigest),
		turnBudget:              turnOpts.Budget,
	}
	if reg, err := media.NewRegistry(id, turnOpts.FSRoot); err == nil {
		rt.media = reg
	} else if logger != nil {
		logger.Warn("session media registry init failed", "session_id", id, "error", err)
	}
	promptReader := promptcontext.NewReader(turnOpts.RuntimeDir)
	if turnOpts.PromptContent != nil {
		promptReader.SetContent(*turnOpts.PromptContent)
	}
	promptReader.SetPreferredName(turnOpts.PreferredName)
	promptReader.SetFilter(promptcontext.Filter{
		SoulEnabled:     turnOpts.PromptContext.SoulEnabled,
		CustomEnabled:   turnOpts.PromptContext.CustomEnabled,
		LongTermEnabled: turnOpts.PromptContext.LongTermEnabled,
	})
	// 创建编排器
	rt.orch = turn.NewOrchestrator(
		agentID,
		turnOpts.FSRoot,
		pub,
		llmClient,
		toolExec,
		policyEngine,
		turn.SkillAccess{
			Catalog: catalog,
			Get:     rt.getLoadedSkills,
			Set:     rt.setLoadedSkills,
		},
		turnOpts.MaxToolLoops,
		promptReader,
		journal,
		hooks.RuntimeConfig{
			Duplicate: hooks.DuplicateConfigOrDefault(turnOpts.DuplicateToolCall),
			ToolResult: hooks.ToolResultConfigOrDefault(hooks.ToolResultConfig{
				Enabled:              turnOpts.ToolResult.Enabled,
				SpillThresholdTokens: turnOpts.ToolResult.SpillThresholdTokens,
				Tools:                turnOpts.ToolResult.Tools,
				FSRoot:               turnOpts.FSRoot,
			}),
			InjectTodayDate: hooks.InjectTodayDateConfigOrDefault(turnOpts.InjectTodayDate),
			Plugins:         turnOpts.PluginHooks,
			Logger:          logger,
		},
		logger,
	)
	rt.orch.SetHookHostConfig(turnOpts.HookHost)
	rt.orch.SetRuntimeIdentity(rt.runtimeRevision, rt.runtimeDigest)
	modelRetries := turnOpts.MaxModelRetries
	if modelRetries == 0 {
		modelRetries = 2
	}
	rt.orch.SetModelRetryLimit(modelRetries)
	toolRetries := turnOpts.ToolRetryLimit
	if toolRetries == 0 {
		toolRetries = 1
	}
	rt.orch.SetToolRetryLimit(toolRetries)
	rt.orch.SetMultimodalEnabled(turnOpts.MultimodalEnabled)
	rt.orch.SetMediaRegistry(rt.media)
	rt.orch.SetLifecycleMetadataProvider(func(sessionID string) map[string]any {
		if sessionID != rt.session.ID || rt.turnCoordinator == nil {
			return nil
		}
		snapshot := rt.turnCoordinator.Snapshot()
		return map[string]any{
			"turn_id":              snapshot.TurnID,
			"step_id":              snapshot.StepID,
			"step_index":           snapshot.StepIndex,
			"context_epoch":        snapshot.ContextEpoch,
			"turn_status":          snapshot.TurnStatus,
			"turn_end_reason":      snapshot.TurnEndReason,
			"step_status":          snapshot.StepStatus,
			"step_end_reason":      snapshot.StepEndReason,
			"assistant_message_id": snapshot.AssistantMsgID,
			"turn_generation":      snapshot.Generation,
			"runtime_revision":     snapshot.RuntimeRevision,
			"runtime_digest":       snapshot.RuntimeDigest,
			"prompt_digest":        snapshot.PromptDigest,
			"tool_digest":          snapshot.ToolDigest,
			"event_seq":            rt.lifecycleEventSequence(),
			"recovery_required":    snapshot.RecoveryRequired,
		}
	})
	rt.orch.SetLifecycleCommandSink(func(sessionID string, command turn.TurnCommand) error {
		if sessionID != rt.session.ID || rt.turnCoordinator == nil {
			return nil
		}
		state := rt.turnCoordinator.Snapshot()
		execution := state.ExecutionContext()
		if command.Type == turn.CommandTurnSnapshotCreated && state.RuntimeDigest != "" {
			return nil
		}
		if command.TurnID == "" {
			command.TurnID = execution.TurnID
		}
		if command.Generation == 0 {
			command.Generation = execution.Generation
		}
		if command.StepID == "" {
			command.StepID = state.StepID
		}
		_, err := rt.lifecycleDispatchErr(command)
		return err
	})
	rt.orch.SetToolBudgetCheck(func(sessionID string) (bool, string) {
		if sessionID != rt.session.ID || rt.turnCoordinator == nil {
			return true, ""
		}
		decision := rt.turnCoordinator.BudgetDecisionFor(turn.CommandToolCallRecorded)
		return decision.Allowed, decision.Reason
	})
	rt.orch.SetToolRetryCheck(func(sessionID string) (bool, string) {
		if sessionID != rt.session.ID || rt.turnCoordinator == nil {
			return true, ""
		}
		decision := rt.turnCoordinator.BudgetDecisionFor(turn.CommandToolExecutionRetrying)
		return decision.Allowed, decision.Reason
	})
	rt.orch.SetModelRetryCheck(func(sessionID string) (bool, string) {
		if sessionID != rt.session.ID || rt.turnCoordinator == nil {
			return true, ""
		}
		decision := rt.turnCoordinator.BudgetDecisionFor(turn.CommandModelRequestStarted)
		return decision.Allowed, decision.Reason
	})
	if len(initialHookStore) > 0 {
		rt.orch.SetHookStore(initialHookStore)
	}
	// 设置工具结果入队器
	rt.orch.SetToolResultEnqueuer(rt.enqueueToolResult)
	if turnOpts.LongTermStore != nil {
		rt.orch.SetLongTermStore(turnOpts.LongTermStore)
	}
	rt.orch.SyncLoadedSkillHooks(loaded)
	rt.restoreLifecycleEvents()
	rt.restoreLegacyPending(initialPending, initialLoopCount)
	// 返回 runtime
	return rt
}

func (r *runtime) lifecycleEventSequence() uint64 {
	if r == nil {
		return 0
	}
	return atomic.LoadUint64(&r.lifecycleEventSeq)
}

func (r *runtime) setLifecycleEventSequence(sequence uint64) {
	if r == nil {
		return
	}
	for {
		current := atomic.LoadUint64(&r.lifecycleEventSeq)
		if sequence <= current || atomic.CompareAndSwapUint64(&r.lifecycleEventSeq, current, sequence) {
			return
		}
	}
}

func firstNonZero(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

// setPolicy 热更新 orchestrator 策略。
func (r *runtime) setPolicy(engine *policy.Engine) {
	r.orch.SetPolicy(engine)
}

// refreshPromptContext updates sidecar content and memory scope for future
// Turns. The orchestrator keeps the active Turn's model snapshot unchanged.
func (r *runtime) refreshPromptContext(content promptcontext.Content, scope string) {
	if r == nil || r.orch == nil {
		return
	}
	r.orch.SetPromptContent(content)
	// Do not switch the persistence target in the middle of a Turn: a memory
	// tool call must continue against the same scope as the active snapshot.
	// Agent snapshot revision will cause a rebuild at the next idle boundary.
	if r.turnState() != turn.StateIdle {
		r.mu.Lock()
		r.pendingLongTermScope = scope
		r.mu.Unlock()
		return
	}
	r.orch.SetLongTermScope(scope)
	r.orch.ReloadLongTermMemory(context.Background())
}

// getLoadedSkills 获取加载的技能列表
func (r *runtime) getLoadedSkills() []skills.LoadedSkill {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]skills.LoadedSkill(nil), r.loadedSkills...)
}

// setLoadedSkills 设置加载的技能列表
func (r *runtime) setLoadedSkills(items []skills.LoadedSkill) {
	r.mu.Lock()
	r.loadedSkills = append([]skills.LoadedSkill(nil), items...)
	r.mu.Unlock()
	if r.orch != nil {
		r.orch.SyncLoadedSkillHooks(items)
	}
}

// setTriggerDelivery 设置 trigger 消息投递跟踪器
func (r *runtime) setTriggerDelivery(tracker triggers.DeliveryTracker) {
	r.triggerDelivery = tracker
}

// start 启动 session runtime
func (r *runtime) start(parent context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	done := r.done
	r.mu.Unlock()
	go func() {
		defer close(done)
		r.consumeLoop(parent)
	}()
}

// consumeLoop 消费消息循环
func (r *runtime) consumeLoop(ctx context.Context) {
	for {
		env, err := r.queue.Dequeue(ctx)
		if err != nil {
			return
		}
		if !r.acceptEnvelope(env) {
			continue
		}
		r.dispatchTurnRequest(ctx, env)
	}
}

// dispatchTurnRequest is the single runtime entry point for queue commands.
// The queue only transports an envelope; this boundary is responsible for
// translating the legacy request type into one Turn/Step operation. Keeping
// that translation in one place prevents new request producers from calling
// the Orchestrator directly and bypassing lifecycle fencing, snapshots, or
// recovery bookkeeping.
func (r *runtime) dispatchTurnRequest(ctx context.Context, env queue.Envelope) {
	// Trigger delivery 在 Apply 成功时清除，不在 dequeue 时清除。
	switch env.RequestType {
	case queue.RequestTypeResume:
		r.clearIdleAutoCompressMark()
		r.logResumeDequeued(env.ResumeValue)
		r.handleResume(ctx, env.ResumeValue)
	case queue.RequestTypeAsyncToolResult:
		r.clearIdleAutoCompressMark()
		r.handleSideEffectProduceAsync(ctx, env.AsyncToolResult)
	case queue.RequestTypeTriggerMessage:
		r.clearIdleAutoCompressMark()
		r.handleSideEffectProduceExternal(ctx, env)
	case queue.RequestTypeSideEffectContinue:
		r.handleSideEffectContinue(ctx, env.SideEffectContinueSource)
	case queue.RequestTypeToolResult:
		r.handleToolResult(ctx)
	case queue.RequestTypeMessage, "":
		r.clearIdleAutoCompressMark()
		r.handleHumanMessage(ctx, env)
	default:
	}
}

// enqueueToolResult 将 tool 结果入队
func (r *runtime) enqueueToolResult(ctx context.Context, _ string) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if execution, ok := turn.ExecutionContextFromContext(ctx); ok && !r.turnCoordinator.IsCurrentExecution(execution) {
		// The step which owns this callback was cancelled or superseded. Do not
		// let a late orchestrator callback create a new continuation.
		return nil
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.TurnID == "" || state.Generation == 0 {
		return fmt.Errorf("cannot schedule tool result without an active turn")
	}
	r.mu.Lock()
	epoch := r.sessionEpoch
	r.mu.Unlock()
	env := queue.Envelope{
		RequestType:  queue.RequestTypeToolResult,
		SessionEpoch: epoch,
		TurnID:       state.TurnID,
		Generation:   state.Generation,
	}
	return r.enqueue(env, queue.PriorityToolResult)
}

// scheduleToolResult 调度 tool 结果入队
func (r *runtime) scheduleToolResult() error {
	return r.enqueueToolResult(nil, r.session.ID)
}

// applyStepOutcome 应用步骤结果
func (r *runtime) applyStepOutcome(history *[]llm.Message, outcome turn.StepOutcome) {
	r.messages = append([]llm.Message(nil), (*history)...)
}

// acceptEnvelope filters stale internal continuations while preserving external
// facts for side-effect reconciliation after a turn is cancelled.
func (r *runtime) acceptEnvelope(env queue.Envelope) bool {
	r.mu.Lock()
	epoch := r.sessionEpoch
	r.mu.Unlock()
	state := r.turnCoordinator.Snapshot()
	turnID, generation := state.TurnID, state.Generation
	validEpoch := env.SessionEpoch == 0 || env.SessionEpoch == epoch
	validTurn := true
	switch env.RequestType {
	case queue.RequestTypeToolResult, queue.RequestTypeResume, queue.RequestTypeSideEffectContinue:
		if env.TurnID == "" && env.Generation == 0 && !state.HasActiveTurn {
			// Legacy persisted HITL and post-cancel side-effect recovery may
			// intentionally arrive before a new Coordinator Turn is opened.
			validTurn = true
		} else {
			validTurn = state.HasActiveTurn && env.TurnID != "" && env.TurnID == turnID && env.Generation == generation
		}
	}
	if !validEpoch {
		r.logger.Info("stale session event dropped", "session_id", r.session.ID, "request_type", env.RequestType, "event_epoch", env.SessionEpoch, "session_epoch", epoch)
		return false
	}
	switch env.RequestType {
	case queue.RequestTypeToolResult, queue.RequestTypeResume, queue.RequestTypeSideEffectContinue:
		if !validTurn {
			r.logger.Info("stale turn continuation dropped", "session_id", r.session.ID, "request_type", env.RequestType, "event_turn_id", env.TurnID, "turn_id", turnID, "event_generation", env.Generation, "generation", generation)
			return false
		}
	}
	return true
}

func (r *runtime) handleHumanMessage(parent context.Context, env queue.Envelope) {
	userName := llm.NormalizeUserMessageName(env.UserName)
	userMsg, err := llm.BuildUserMessage(env.Content, env.ContentParts, userName)
	if err != nil {
		r.logger.Warn("invalid user message", "session_id", r.session.ID, "error", err)
		return
	}
	if r.media != nil && llm.MessageHasImages(userMsg) {
		persisted, perr := media.PersistUserMessageImages(r.media, userMsg)
		if perr != nil {
			r.logger.Warn("persist user images failed", "session_id", r.session.ID, "error", perr)
			return
		}
		userMsg = persisted
	}
	pending := r.pendingSnapshot()
	r.mu.Lock()
	if pending != nil {
		r.orch.InterruptPending(r.session.ID, &r.messages, pending)
	}
	if r.orch.RepairUnrespondedToolCalls(r.session.ID, &r.messages) {
		r.logger.Info("repaired orphan tool_calls before user message",
			"session_id", r.session.ID,
		)
	}
	firstInteraction := len(r.messages) == 0
	r.mu.Unlock()
	r.applyPendingLongTermScope()
	r.observeSkillCatalogChange()
	if err := r.lifecycleBeginHumanTurn(); err != nil {
		r.mu.Lock()
		r.messages = append(r.messages, userMsg)
		r.mu.Unlock()
		r.logger.Warn("start human turn lifecycle failed", "session_id", r.session.ID, "error", err)
		r.persist(context.Background())
		return
	}
	historyStart := r.lifecycleHistoryLength()

	if firstInteraction && r.orch != nil {
		r.orch.ReloadLongTermMemory(parent)
	}

	outcome, history := r.runTurnStepWithSideEffects(parent, true, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(ctx, r.session.ID, history, userMsg)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil {
		r.logger.Warn("finish model step lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	if outcome.Err != nil {
		r.mu.Lock()
		r.messages = history
		r.mu.Unlock()
		r.persist(context.Background())
		r.finishTurnIdle(outcome)
		return
	}
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	if outcome.ScheduleToolResult {
		if err := r.scheduleToolResult(); err != nil {
			r.logger.Warn("schedule tool result failed",
				"session_id", r.session.ID,
				"error", err,
			)
			r.finishTurnIdle(outcome)
		}
	} else {
		r.finishTurnIdle(outcome)
	}
	r.persist(context.Background())
}

// applyPendingLongTermScope starts the next human Turn with a scope that was
// changed while the preceding Turn was active (including an interrupted HITL
// turn). It is deliberately not called from tool continuations.
func (r *runtime) applyPendingLongTermScope() {
	if r == nil || r.orch == nil {
		return
	}
	r.mu.Lock()
	scope := r.pendingLongTermScope
	r.pendingLongTermScope = ""
	r.mu.Unlock()
	if scope == "" {
		return
	}
	r.orch.SetLongTermScope(scope)
	r.orch.ReloadLongTermMemory(context.Background())
}

// observeSkillCatalogChange applies external Skill edits only at a new human
// Turn boundary. The active Turn snapshot remains untouched.
func (r *runtime) observeSkillCatalogChange() {
	if r == nil || r.skillsCatalog == nil {
		return
	}
	current := r.skillsCatalog.Revision()
	if current == "" {
		return
	}
	r.mu.Lock()
	previous := r.skillRevision
	if previous == current {
		r.mu.Unlock()
		return
	}
	r.skillRevision = current
	r.mu.Unlock()
	if r.hub != nil {
		r.hub.Publish(r.session.ID, "skills/changed", map[string]any{
			"agent_id":         r.agentID,
			"previous":         previous,
			"revision":         current,
			"applied_boundary": "next_turn",
		})
	}
}

func (r *runtime) afterToolStep(outcome turn.StepOutcome) {
	if outcome.ScheduleToolResult {
		if err := r.scheduleToolResult(); err != nil {
			r.logger.Warn("schedule tool result failed",
				"session_id", r.session.ID,
				"error", err,
			)
			r.finishTurnIdle(outcome)
		}
	} else {
		r.finishTurnIdle(outcome)
	}
	r.persist(context.Background())
}

func (r *runtime) handleToolResult(parent context.Context) {
	started, err := r.lifecycleBeginContinuationStep(turn.TurnSourceHuman)
	if err != nil {
		r.logger.Warn("start tool continuation lifecycle failed", "session_id", r.session.ID, "error", err)
		r.persist(context.Background())
		r.finishTurnIdle(turn.StepOutcome{})
		return
	}
	if !started {
		r.persist(context.Background())
		r.finishTurnIdle(turn.StepOutcome{})
		return
	}
	historyStart := r.lifecycleHistoryLength()
	outcome, history := r.runTurnStepWithSideEffects(parent, true, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		return r.orch.RunToolMessageTurn(ctx, r.session.ID, history)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil {
		r.logger.Warn("finish tool continuation lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	r.afterToolStep(outcome)
}

func (r *runtime) handleResume(parent context.Context, resumeValue map[string]any) {
	pending := r.pendingSnapshot()
	if pending == nil {
		r.logger.Info("resume ignored no pending hitl",
			"session_id", r.session.ID,
			"resume_value", resumeValue,
		)
		return
	}
	pendingKind, pendingToolCallID := pendingHITLLogFields(pending)

	resumeKind := clihitl.ResumeValueKind(resumeValue)
	if resumeKind != "nil" && resumeKind != "unknown" && pending != nil &&
		!resumeKindMatchesPending(pending, resumeKind) {
		r.logger.Warn("resume kind mismatch (diagnostic only, still processing)",
			"session_id", r.session.ID,
			"pending_summary", pendingKind,
			"pending_tool_call_id", pendingToolCallID,
			"resume_value_kind", resumeKind,
			"resume_value", resumeValue,
		)
	}

	r.logger.Info("resume handling",
		"session_id", r.session.ID,
		"pending_kind", pendingKind,
		"pending_tool_call_id", pendingToolCallID,
		"resume_value_kind", resumeKind,
		"resume_value", resumeValue,
	)
	if err := r.lifecyclePrepareResume(resumeValue); err != nil {
		r.logger.Warn("prepare resume lifecycle failed", "session_id", r.session.ID, "error", err)
		r.persist(context.Background())
		return
	}

	outcome, history := r.runTurnStepWithSideEffects(parent, false, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		return r.orch.ContinueAfterResume(ctx, r.session.ID, history, resumeValue, pending)
	})
	if err := r.lifecycleAfterResume(outcome, history); err != nil {
		r.logger.Warn("finish resume lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	r.afterToolStep(outcome)
}

// persist 持久化 session 数据
func (r *runtime) persist(ctx context.Context) {
	if r.store == nil || r.isChildSession() {
		return
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	idleMarked := r.idleAutoCompressApplied
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	stepCount := r.stepIndexSnapshot()
	_ = r.store.Save(ctx, store.Record{
		AgentID:      r.session.ID,
		NodeID:       r.session.AgentID,
		Messages:     msgs,
		LoadedSkills: loaded,
		RuntimeState: store.RuntimeState{
			Pending:                 pending,
			ToolLoopCount:           stepCount,
			HookStore:               hooks.CloneSessionStore(r.orch.HookStoreSnapshot()),
			IdleAutoCompressApplied: idleMarked,
			NotifySeq:               notifySeq,
			AckSeq:                  ackSeq,
		},
	})
}

// replacementData returns the in-memory state needed when a manager without
// a persistence store replaces a runtime. Production managers normally load
// this state from SQLite after persist; keeping this fallback prevents tests
// and embedded callers from losing history during a swap.
func (r *runtime) replacementData() ([]llm.Message, []skills.LoadedSkill, *turn.PendingHITL, int, map[string]json.RawMessage, bool, int, int) {
	if r == nil {
		return nil, nil, nil, 0, nil, false, 0, 0
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	idleMarked := r.idleAutoCompressApplied
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	stepCount := r.stepIndexSnapshot()
	var hookStore map[string]json.RawMessage
	if r.orch != nil {
		hookStore = r.orch.HookStoreSnapshot()
	}
	return msgs, loaded, pending, stepCount, hookStore, idleMarked, notifySeq, ackSeq
}

func (r *runtime) clearMessages(ctx context.Context) {
	// Context clearing is a logical cancellation boundary. Keep the durable
	// Turn terminal event even though the legacy message snapshot is removed;
	// otherwise a restart could resurrect an in-flight Step from lifecycle
	// events while the visible session is empty.
	if err := r.lifecycleCancel(); err != nil && r.logger != nil {
		r.logger.Warn("cancel lifecycle during clear failed", "session_id", r.session.ID, "error", err)
	}
	if r.compression != nil {
		r.compression.CancelSession(r.session.ID)
	}
	if r.sideEffectsEnabled() {
		r.sideEffects.ClearSession(r.session.ID, r.orch, r.triggerDelivery)
	}
	r.mu.Lock()
	r.sessionEpoch++
	r.messages = nil
	r.loadedSkills = nil
	r.mu.Unlock()
	if r.orch != nil {
		r.orch.ClearHookStore()
		r.orch.SyncLoadedSkillHooks(nil)
		r.orch.ReloadLongTermMemory(ctx)
	}
	if r.store != nil {
		_ = r.store.ClearMessages(ctx, r.session.ID)
	}
}

func (r *runtime) messagesSnapshot() []llm.Message {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]llm.Message(nil), r.messages...)
}

func (r *runtime) loadedSkillsSnapshot() []skills.LoadedSkill {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]skills.LoadedSkill(nil), r.loadedSkills...)
}

func (r *runtime) messageCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.messages)
}

func (r *runtime) queueDepth() int {
	return r.queue.Len()
}

func (r *runtime) turnState() turn.State {
	if r == nil || r.turnCoordinator == nil {
		return turn.StateIdle
	}
	return turnStateFromCoordinatorSnapshot(r.turnCoordinator.Snapshot())
}

func (r *runtime) hasPendingHITL() bool {
	return r.pendingSnapshot() != nil
}

// lifecycleExecutionBusy is the Coordinator-backed execution predicate used
// by maintenance and compression boundaries. Waiting for client interaction
// is deliberately not considered model execution, matching the old HITL
// eviction behavior without consulting a second pending field.
func (r *runtime) lifecycleExecutionBusy() bool {
	if r == nil || r.turnCoordinator == nil {
		return false
	}
	snapshot := r.turnCoordinator.Snapshot()
	if !snapshot.HasActiveTurn {
		return false
	}
	return snapshot.StepStatus != turn.StepStatusWaitingInteraction
}

// pendingHITLLogFields 提取 pending HITL 日志字段。
func pendingHITLLogFields(pending *turn.PendingHITL) (summary string, toolCallID string) {
	if pending == nil || len(pending.Items) == 0 {
		return "", ""
	}
	if len(pending.Items) == 1 {
		return "hitl", pending.Items[0].ToolCall.ID
	}
	return fmt.Sprintf("hitl(%d)", len(pending.Items)), pending.Items[0].ToolCall.ID
}

func (r *runtime) sidecarPrefix() compression.SidecarPrefix {
	return compression.SidecarPrefix{
		SystemPrompt: r.orch.SystemPromptForSession(r.session.ID),
		Tools:        r.orch.ToolDefinitionsForSession(r.session.ID),
	}
}

func (r *runtime) compressContext(ctx context.Context) compression.ForceResult {
	if r.isChildSession() {
		return compression.ForceResult{Status: "unsupported"}
	}
	busy := r.lifecycleExecutionBusy()
	if busy {
		return compression.ForceResult{Status: "busy"}
	}
	if r.compression == nil {
		return compression.ForceResult{Status: "disabled"}
	}
	// sidecarPrefix → SystemPromptForSession → getLoadedSkills 会抢 r.mu，须在持锁前计算。
	prefix := r.sidecarPrefix()
	r.mu.Lock()
	result := r.compression.ForceBlocking(ctx, r.session.ID, r.agentID, r.hub, &r.messages, prefix)
	r.mu.Unlock()
	if result.Status == "applied" && r.orch != nil {
		r.orch.ReloadLongTermMemory(ctx)
	}
	if result.Status == "applied" {
		r.persist(ctx)
	}
	return result
}

func (r *runtime) contextView() *ContextView {
	lifecycle := turn.CoordinatorSnapshot{}
	if r.turnCoordinator != nil {
		lifecycle = r.turnCoordinator.Snapshot()
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	queuePending := r.queue.Len()
	state := r.turnState()
	view := &ContextView{
		SessionID:           r.session.ID,
		MessagesCount:       len(r.messages),
		MessagesTotalTokens: estimateMessageTokens(r.messages),
		ToolLoopCount:       lifecycle.Usage.Steps,
		LoadedSkills:        loaded,
		QueuePending:        queuePending,
		HasActiveTurn:       lifecycle.HasActiveTurn,
		TurnID:              lifecycle.TurnID,
		StepID:              lifecycle.StepID,
		StepIndex:           lifecycle.StepIndex,
		ContextEpoch:        lifecycle.ContextEpoch,
		TurnStatus:          lifecycle.TurnStatus,
		TurnEndReason:       lifecycle.TurnEndReason,
		StepStatus:          lifecycle.StepStatus,
		StepEndReason:       lifecycle.StepEndReason,
		TurnGeneration:      lifecycle.Generation,
		RuntimeRevision:     lifecycle.RuntimeRevision,
		RuntimeDigest:       lifecycle.RuntimeDigest,
		PromptDigest:        lifecycle.PromptDigest,
		ToolDigest:          lifecycle.ToolDigest,
		RecoveryRequired:    lifecycle.RecoveryRequired,
		TurnState:           state,
		Messages:            msgs,
	}
	if view.LoadedSkills == nil {
		view.LoadedSkills = []skills.LoadedSkill{}
	}
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	view.PendingToolCallsCount = pendingToolCallsCount(pending)
	view.SystemPrompt = r.orch.SystemPromptForSession(r.session.ID)
	enrichContextPromptStats(view, r.skillsCatalog)
	if r.compression != nil {
		if snap, ok := r.compression.LastCompression(r.session.ID); ok {
			s := snap
			view.LastCompression = &s
		}
	}
	return view
}

func (r *runtime) enqueue(env queue.Envelope, priority queue.Priority) error {
	r.mu.Lock()
	if env.SessionEpoch == 0 {
		env.SessionEpoch = r.sessionEpoch
	}
	epoch := r.sessionEpoch
	r.mu.Unlock()
	state := r.turnCoordinator.Snapshot()
	internalContinuation := false
	if env.RequestType == queue.RequestTypeResume || env.RequestType == queue.RequestTypeSideEffectContinue {
		internalContinuation = true
		if env.TurnID == "" {
			if state.HasActiveTurn {
				env.TurnID = state.TurnID
				env.Generation = state.Generation
			}
		}
	}
	if env.SessionEpoch != epoch {
		// The producer raced with clear-context after it captured the session
		// epoch. Do not enqueue an event that is guaranteed to be stale.
		return nil
	}
	if internalContinuation && env.TurnID != "" && (!state.HasActiveTurn || env.TurnID != state.TurnID || env.Generation != state.Generation) {
		// The producer raced with cancel or a new Turn. Do not enqueue a
		// continuation that can only be rejected by the consumer.
		return nil
	}
	if env.RequestType == queue.RequestTypeAsyncToolResult || env.RequestType == queue.RequestTypeTriggerMessage {
		// External facts are intentionally not bound to turnID/generation, but
		// they still belong to the current session epoch. This lets a cancelled
		// turn reconcile them later without allowing clear-context events back in.
		internalContinuation = false
	}
	if env.RequestType == queue.RequestTypeResume {
		before := r.resumeDiagSnapshot()
		err := r.queue.Enqueue(env, priority)
		after := r.resumeDiagSnapshot()
		r.logger.Info("resume enqueued",
			"session_id", r.session.ID,
			"resume_value_kind", clihitl.ResumeValueKind(env.ResumeValue),
			"approval_id", resumeFieldString(env.ResumeValue, "approval_id"),
			"tool_call_id", resumeFieldString(env.ResumeValue, "tool_call_id"),
			"queue_len_before", before.queueLen,
			"queue_len_after", after.queueLen,
			"resume_in_queue_before", before.resumeInQueue,
			"resume_in_queue_after", after.resumeInQueue,
			"turn_state_before", before.turnState,
			"pending_kind_before", before.pendingKind,
			"pending_tool_call_id_before", before.pendingToolCallID,
			"enqueue_err", fmt.Sprint(err),
		)
		return err
	}
	err := r.queue.Enqueue(env, priority)
	return err
}

type resumeDiagSnapshot struct {
	queueLen          int
	resumeInQueue     int
	turnState         turn.State
	pendingKind       string
	pendingToolCallID string
}

func (r *runtime) resumeDiagSnapshot() resumeDiagSnapshot {
	pending := r.pendingSnapshot()
	pendingKind, pendingID := pendingHITLLogFields(pending)
	r.mu.Lock()
	r.mu.Unlock()
	state := r.turnState()
	return resumeDiagSnapshot{
		queueLen:          r.queue.Len(),
		resumeInQueue:     r.queue.CountByRequestType(queue.RequestTypeResume),
		turnState:         state,
		pendingKind:       pendingKind,
		pendingToolCallID: pendingID,
	}
}

func (r *runtime) logResumeDequeued(resumeValue map[string]any) {
	snap := r.resumeDiagSnapshot()
	r.logger.Info("resume dequeued",
		"session_id", r.session.ID,
		"resume_value_kind", clihitl.ResumeValueKind(resumeValue),
		"approval_id", resumeFieldString(resumeValue, "approval_id"),
		"tool_call_id", resumeFieldString(resumeValue, "tool_call_id"),
		"queue_len_remaining", snap.queueLen,
		"resume_still_queued", snap.resumeInQueue,
		"turn_state", snap.turnState,
		"pending_kind", snap.pendingKind,
		"pending_tool_call_id", snap.pendingToolCallID,
	)
}

func resumeKindMatchesPending(pending *turn.PendingHITL, resumeKind string) bool {
	if pending == nil {
		return false
	}
	for _, item := range pending.Items {
		name := item.ToolCall.Function.Name
		switch resumeKind {
		case "user_information":
			if tools.IsAskUserInformation(name) {
				return true
			}
		case "approval":
			if !tools.IsAskUserInformation(name) {
				return true
			}
		}
	}
	return false
}

func resumeFieldString(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	raw, ok := value[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func (r *runtime) cancelTurn() bool {
	return r.cancelTurnWithReason("", nil)
}

func (r *runtime) cancelTurnWithReason(interruptMessage string, metadata map[string]any) bool {
	lifecycleWasActive := r.turnCoordinator != nil && r.turnCoordinator.Snapshot().HasActiveTurn
	stateBefore := r.turnState()
	pending := r.pendingSnapshot()
	if err := r.lifecycleCancel(); err != nil && r.logger != nil {
		r.logger.Warn("cancel lifecycle failed", "session_id", r.session.ID, "error", err)
	}
	r.mu.Lock()
	cancel := r.turnCancel
	active := cancel != nil && stateBefore != turn.StateIdle
	lifecycleActive := lifecycleWasActive
	changed := pending != nil || lifecycleActive || active
	if pending != nil {
		if interruptMessage == "" {
			interruptMessage = "工具调用已被用户取消。"
		}
		r.orch.InterruptPendingWithReason(
			r.session.ID,
			&r.messages,
			pending,
			interruptMessage,
			metadata,
		)
	}
	if active {
		r.mu.Unlock()
		cancel()
		if pending == nil {
			r.maybeScheduleContinueAfterCancel()
		}
		return true
	}
	repaired := r.orch.RepairUnrespondedToolCalls(r.session.ID, &r.messages)
	r.mu.Unlock()
	if changed || repaired {
		r.logger.Info("repaired orphan tool_calls on idle cancel",
			"session_id", r.session.ID,
		)
		r.persist(context.Background())
	}
	if pending == nil {
		r.maybeScheduleContinueAfterCancel()
	}
	return changed || repaired
}

func (r *runtime) requestStop() {
	if r.compression != nil {
		r.compression.CancelSession(r.session.ID)
	}
	if r.sideEffectsEnabled() {
		r.sideEffects.ClearSession(r.session.ID, r.orch, r.triggerDelivery)
	}
	r.queue.Close()
}

func (r *runtime) waitStopped() {
	r.mu.Lock()
	started := r.started
	done := r.done
	r.mu.Unlock()
	if started {
		<-done
	}
}

func (r *runtime) stop() {
	r.requestStop()
	r.waitStopped()
}

func (r *runtime) setLoadedSkillsByName(names []string) []skills.LoadedSkill {
	if r.skillsCatalog == nil {
		return nil
	}
	loaded := r.skillsCatalog.SetLoadedSkills(names)
	r.setLoadedSkills(loaded)
	r.persist(context.Background())
	return loaded
}

func (r *runtime) unloadSkillsByName(names []string) []skills.LoadedSkill {
	if r.skillsCatalog == nil {
		return r.loadedSkillsSnapshot()
	}
	loaded := r.skillsCatalog.UnloadSkills(r.loadedSkillsSnapshot(), names)
	r.setLoadedSkills(loaded)
	r.persist(context.Background())
	return loaded
}
