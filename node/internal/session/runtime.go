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
	// InputBox is the FIFO ingress for user/trigger/A2A inputs. MessageQueue is
	// reserved for control and recovery events that must fence the active Turn.
	inputBox *InputBox
	// 控制/恢复队列
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

	// 技能目录。skillsCatalog 是控制面使用的 live catalog；
	// skillsTurnCatalog 是当前 human Turn 及其 continuation 使用的不可变视图。
	skillsCatalog     *skills.Catalog
	skillsTurnCatalog *skills.Catalog
	skillRevision     string
	// 上下文压缩逻辑
	compression *compression.Coordinator

	started bool
	done    chan struct{}

	mu              sync.Mutex         // 互斥锁
	persistMu       sync.Mutex         // serializes full runtime snapshots
	turnCancel      context.CancelFunc // 取消 turn 上下文
	turnCancelToken *struct{}          // prevents a stale step defer from clearing a newer cancel handle
	// sessionEpoch invalidates events queued before clear-context/rebuild.
	sessionEpoch uint64
	// turnEpoch fences the in-flight model step from clear-context.  A clear
	// can run concurrently with the provider call; its late history must not
	// be committed back over the freshly cleared snapshot.
	turnEpoch uint64
	// turnFenceActive distinguishes production model steps from direct lifecycle
	// transitions that intentionally do not install a provider fence.
	turnFenceActive bool
	// lifecycleMu serializes compound Coordinator transitions. The
	// TurnCoordinator owns Turn/Step identity and generation; runtime keeps no
	// second lifecycle projection.
	lifecycleMu           sync.Mutex
	lifecycleCommandSeq   uint64
	lifecycleEventSeq     uint64
	lifecycleEventsLoaded bool
	messages              []llm.Message        // 交互消息列表
	historyRevision       uint64               // committed message snapshot revision
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
	turnCatalog := catalog.NewTurnView()
	if turnCatalog == nil {
		turnCatalog = catalog
	}
	journal := history.NewJournal(turnOpts.RawMessageHistoryEnabled, turnOpts.RawMessageHistoryDir, logger)
	rt := &runtime{
		session:         Session{ID: id, AgentID: agentID},
		inputBox:        NewInputBox(),
		queue:           queue.NewMessageQueue(),
		turnCoordinator: turn.NewTurnCoordinator(id, agentID),
		done:            make(chan struct{}),
		// Zero is reserved for legacy envelopes without an epoch. Starting at
		// one makes the first human message fenceable against clear-context.
		sessionEpoch:      1,
		store:             st,
		hub:               eventHub,
		publisher:         pub,
		agentID:           agentID,
		logger:            logger,
		skillsCatalog:     catalog,
		skillsTurnCatalog: turnCatalog,
		compression: func() *compression.Coordinator {
			coord := compression.NewCoordinator(llmClient, turnOpts.CompressionSilent, turnOpts.CompressionBlocking)
			coord.SetLogger(logger)
			coord.SetRawMessageHistoryEnabled(turnOpts.RawMessageHistoryEnabled)
			return coord
		}(),
		messages:                append([]llm.Message(nil), initial...),
		loadedSkills:            append([]skills.LoadedSkill(nil), loaded...),
		skillRevision:           turnCatalog.Revision(),
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
			Catalog:           turnCatalog,
			CatalogToolMode:   turnOpts.SkillsCatalogToolMode,
			Get:               rt.getLoadedSkills,
			Set:               rt.setLoadedSkills,
			SetWithHookStatus: rt.setLoadedSkillsWithHookStatus,
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
		contextInjectionDigest := ""
		contextInjectionCount := 0
		if modelSnapshot := rt.orch.ModelContextSnapshot(sessionID); modelSnapshot != nil {
			contextInjectionDigest = modelSnapshot.ContextInjectionDigest
			contextInjectionCount = len(modelSnapshot.ContextInjections)
		}
		return map[string]any{
			"turn_id":                  snapshot.TurnID,
			"step_id":                  snapshot.StepID,
			"step_index":               snapshot.StepIndex,
			"context_epoch":            snapshot.ContextEpoch,
			"turn_status":              snapshot.TurnStatus,
			"turn_end_reason":          snapshot.TurnEndReason,
			"step_status":              snapshot.StepStatus,
			"step_end_reason":          snapshot.StepEndReason,
			"assistant_message_id":     snapshot.AssistantMsgID,
			"turn_generation":          snapshot.Generation,
			"runtime_revision":         snapshot.RuntimeRevision,
			"runtime_digest":           snapshot.RuntimeDigest,
			"prompt_digest":            snapshot.PromptDigest,
			"tool_digest":              snapshot.ToolDigest,
			"context_injection_digest": contextInjectionDigest,
			"context_injection_count":  contextInjectionCount,
			"event_seq":                rt.lifecycleEventSequence(),
			"recovery_required":        snapshot.RecoveryRequired,
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
	_ = r.setLoadedSkillsWithHookStatus(items)
}

func (r *runtime) setLoadedSkillsWithHookStatus(items []skills.LoadedSkill) turn.SkillHooksSyncResult {
	before := r.loadedSkillsSnapshot()
	r.mu.Lock()
	r.loadedSkills = append([]skills.LoadedSkill(nil), items...)
	r.mu.Unlock()
	hookSync := turn.SkillHooksSyncResult{Status: "unavailable"}
	if r.orch != nil {
		hookSync = r.orch.SyncLoadedSkillHooks(items)
		if turn.Digest(before) != turn.Digest(items) && r.turnState() != turn.StateIdle {
			r.orch.RequestModelContextRefresh(r.session.ID, "skills_api_update")
		}
	}
	return hookSync
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
		// Control/continuation records are drained before a new external input.
		// A human or trigger arriving while HITL is pending therefore stays in
		// InputBox and cannot preempt the active Turn.
		if r.queue.Len() > 0 {
			env, err := r.queue.Dequeue(ctx)
			if err != nil {
				return
			}
			if r.acceptEnvelope(env) {
				r.dispatchTurnRequest(ctx, env)
			}
			r.signalInputBox()
			continue
		}
		if record, ok := r.popInputIfIdle(); ok {
			if r.acceptEnvelope(record.Env) {
				r.dispatchInput(ctx, record)
			}
			r.signalInputBox()
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-r.queue.Wake():
			if r.queue.Closed() {
				return
			}
		case <-r.inputBox.Wake():
			if r.inputBox.Closed() {
				return
			}
		}
	}
}

func (r *runtime) signalInputBox() {
	if r != nil && r.inputBox != nil {
		r.inputBox.Signal()
	}
}

func (r *runtime) popInputIfIdle() (InputRecord, bool) {
	if r == nil || r.inputBox == nil {
		return InputRecord{}, false
	}
	if r.inputBox.Closed() {
		return InputRecord{}, false
	}
	state := r.turnCoordinator.Snapshot()
	if state.HasActiveTurn && !state.TurnStatus.Terminal() {
		return InputRecord{}, false
	}
	return r.inputBox.Pop()
}

func (r *runtime) dispatchInput(ctx context.Context, record InputRecord) {
	env := record.Env
	// InputBox records are data-plane inputs. They all enter the normal human
	// turn path; the UserName/source fields preserve whether the producer was
	// an actual user or a trigger.
	env.RequestType = queue.RequestTypeMessage
	r.clearIdleAutoCompressMark()
	source := turn.TurnSourceHuman
	if record.Kind == InputKindTrigger {
		source = turn.TurnSourceTrigger
	} else if record.Kind == InputKindA2A {
		source = turn.TurnSourceA2A
	}
	r.handleInputMessage(ctx, env, source)
	if record.Kind == InputKindTrigger && strings.TrimSpace(env.TriggerID) != "" && r.triggerDelivery != nil {
		r.triggerDelivery.ClearPendingDelivery(strings.TrimSpace(env.TriggerID))
	}
}

// dispatchTurnRequest is the single runtime entry point for control and
// recovery queue commands. Keeping that translation in one place prevents
// producers from bypassing lifecycle fencing, snapshots, or recovery bookkeeping.
func (r *runtime) dispatchTurnRequest(ctx context.Context, env queue.Envelope) {
	switch env.RequestType {
	case queue.RequestTypeResume:
		r.clearIdleAutoCompressMark()
		r.logResumeDequeued(env.ResumeValue)
		r.handleResume(ctx, env.ResumeValue)
	case queue.RequestTypeAsyncToolResult:
		r.clearIdleAutoCompressMark()
		r.handleSideEffectProduceAsync(ctx, env.AsyncToolResult)
	case queue.RequestTypeSideEffectContinue:
		r.handleSideEffectContinue(ctx, env.SideEffectContinueSource)
	case queue.RequestTypeTurnContinuation:
		r.handleTurnContinuation(ctx)
	default:
	}
}

// enqueueTurnContinuation schedules a recovered or externally reconciled
// continuation. Normal tool results never enter MessageQueue.
func (r *runtime) enqueueTurnContinuation(ctx context.Context) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	state := r.turnCoordinator.Snapshot()
	if !state.HasActiveTurn || state.TurnID == "" || state.Generation == 0 {
		return fmt.Errorf("cannot schedule turn continuation without an active turn")
	}
	r.mu.Lock()
	epoch := r.sessionEpoch
	r.mu.Unlock()
	env := queue.Envelope{
		RequestType:  queue.RequestTypeTurnContinuation,
		SessionEpoch: epoch,
		TurnID:       state.TurnID,
		Generation:   state.Generation,
	}
	return r.enqueue(env, queue.PriorityContinuation)
}

// commitStepHistory publishes a new in-memory message snapshot. Callers must
// hold r.mu while invoking it. The digest check makes fallback commits
// idempotent when the lifecycle adapter has already committed the snapshot.
func (r *runtime) commitStepHistory(history *[]llm.Message) bool {
	if history == nil {
		return false
	}
	if turn.Digest(r.messages) == turn.Digest(*history) {
		return false
	}
	r.messages = append([]llm.Message(nil), (*history)...)
	r.historyRevision++
	return true
}

// acceptEnvelope filters stale control continuations while preserving
// external facts for side-effect reconciliation after a turn is cancelled.
func (r *runtime) acceptEnvelope(env queue.Envelope) bool {
	r.mu.Lock()
	epoch := r.sessionEpoch
	r.mu.Unlock()
	state := r.turnCoordinator.Snapshot()
	turnID, generation := state.TurnID, state.Generation
	validEpoch := env.SessionEpoch == 0 || env.SessionEpoch == epoch
	validTurn := true
	switch env.RequestType {
	case queue.RequestTypeTurnContinuation, queue.RequestTypeResume, queue.RequestTypeSideEffectContinue:
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
	case queue.RequestTypeTurnContinuation, queue.RequestTypeResume, queue.RequestTypeSideEffectContinue:
		if !validTurn {
			r.logger.Info("stale turn continuation dropped", "session_id", r.session.ID, "request_type", env.RequestType, "event_turn_id", env.TurnID, "turn_id", turnID, "event_generation", env.Generation, "generation", generation)
			return false
		}
	}
	return true
}

func (r *runtime) handleInputMessage(parent context.Context, env queue.Envelope, source turn.TurnSource) {
	if !r.sessionEpochCurrent(env.SessionEpoch) {
		r.logger.Info("stale human message dropped after session clear", "session_id", r.session.ID)
		return
	}
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
	// A new input must never preempt an active Turn. InputBox normally pops only
	// while idle; keep this defensive guard for races and direct test fixtures.
	if state := r.turnCoordinator.Snapshot(); state.HasActiveTurn && !state.TurnStatus.Terminal() {
		r.logger.Info("human input deferred while turn is active", "session_id", r.session.ID)
		return
	}
	r.mu.Lock()
	if r.orch.RepairUnrespondedToolCalls(r.session.ID, &r.messages) {
		r.logger.Info("repaired orphan tool_calls before new turn",
			"session_id", r.session.ID,
		)
	}
	firstInteraction := len(r.messages) == 0
	r.mu.Unlock()
	r.applyPendingLongTermScope()
	r.observeSkillCatalogChange()
	if err := r.lifecycleBeginInputTurn(source); err != nil {
		r.mu.Lock()
		r.messages = append(r.messages, userMsg)
		r.historyRevision++
		r.mu.Unlock()
		r.logger.Warn("start human turn lifecycle failed", "session_id", r.session.ID, "error", err)
		r.persist(context.Background())
		return
	}
	// Clear-context may have won the race while lifecycleBeginHumanTurn was
	// opening the new turn. Do not let an already accepted queue envelope from
	// before the clear become the first message of the new context.
	if !r.sessionEpochCurrent(env.SessionEpoch) {
		r.cancelTurn()
		return
	}
	historyStart := r.lifecycleHistoryLength()

	if firstInteraction && r.orch != nil {
		r.orch.ReloadLongTermMemory(parent)
	}

	outcome, history := r.runTurnStepWithSideEffectsAtEpoch(parent, true, env.SessionEpoch, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(ctx, r.session.ID, history, userMsg)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil {
		r.logger.Warn("finish model step lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(parent, env.SessionEpoch, outcome)
	r.finishTurnIdle(outcome)
	r.persist(context.Background())
}

// runInlineToolContinuationChain keeps tool results inside the logical Turn.
// Each continuation starts a new lifecycle Step but remains in this serialized
// Turn chain, while MessageQueue only carries explicit control/recovery events.
func (r *runtime) runInlineToolContinuationChain(parent context.Context, expectedEpoch uint64, outcome turn.StepOutcome) turn.StepOutcome {
	for outcome.Err == nil && outcome.Pending == nil && outcome.ScheduleToolResult {
		started, err := r.lifecycleBeginContinuationStep(turn.TurnSourceHuman)
		if err != nil {
			outcome.Err = err
			break
		}
		if !started {
			outcome.ScheduleToolResult = false
			break
		}
		historyStart := r.lifecycleHistoryLength()
		next, history := r.runTurnStepWithSideEffectsAtEpoch(parent, true, expectedEpoch, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
			return r.orch.RunToolMessageTurn(ctx, r.session.ID, history)
		})
		if err := r.lifecycleAfterModelStep(next, history, historyStart); err != nil {
			r.logger.Warn("finish inline tool continuation lifecycle failed", "session_id", r.session.ID, "error", err)
			if next.Err == nil {
				next.Err = err
			}
		}
		r.commitHistoryFallback(history)
		outcome = next
	}
	return outcome
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
	view := r.skillsCatalog.NewTurnView()
	if view == nil {
		return
	}
	current := view.Revision()
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
	r.skillsTurnCatalog = view
	r.mu.Unlock()
	if r.orch != nil {
		r.orch.SetSkillsCatalog(view)
	}
	if r.hub != nil {
		r.hub.Publish(r.session.ID, "skills/changed", map[string]any{
			"agent_id":         r.agentID,
			"previous":         previous,
			"revision":         current,
			"applied_boundary": "next_turn",
		})
	}
}

// refreshSkillsCatalogForExplicitMutation switches the model-facing view
// after a control-plane mutation. Model-issued load_skills deliberately keeps
// the existing Turn view; control-plane mutation is an explicit context
// boundary and may expose the latest catalog at the next model Step.
func (r *runtime) refreshSkillsCatalogForExplicitMutation() {
	if r == nil || r.skillsCatalog == nil {
		return
	}
	view := r.skillsCatalog.NewTurnView()
	if view == nil {
		return
	}
	r.mu.Lock()
	r.skillsTurnCatalog = view
	r.skillRevision = view.Revision()
	r.mu.Unlock()
	if r.orch != nil {
		r.orch.SetSkillsCatalog(view)
	}
}

func (r *runtime) handleTurnContinuation(parent context.Context) {
	started, err := r.lifecycleBeginContinuationStep(turn.TurnSourceHuman)
	if err != nil {
		r.logger.Warn("start turn continuation lifecycle failed", "session_id", r.session.ID, "error", err)
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
		r.logger.Warn("finish turn continuation lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(parent, 0, outcome)
	r.finishTurnIdle(outcome)
	r.persist(context.Background())
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
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(parent, 0, outcome)
	r.finishTurnIdle(outcome)
	r.persist(context.Background())
}

func (r *runtime) commitHistoryFallback(history []llm.Message) {
	r.mu.Lock()
	if !r.turnEpochCurrentLocked() {
		r.mu.Unlock()
		return
	}
	changed := r.commitStepHistory(&history)
	r.mu.Unlock()
	if changed {
		r.persist(context.Background())
	}
}

func (r *runtime) sessionEpochCurrent(epoch uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return epoch == 0 || epoch == r.sessionEpoch
}

func (r *runtime) turnEpochCurrentLocked() bool {
	if !r.turnFenceActive {
		return true
	}
	return r.turnEpoch == r.sessionEpoch
}

// persist 持久化 session 数据
func (r *runtime) persist(ctx context.Context) {
	if r.store == nil || r.isChildSession() {
		return
	}
	r.persistMu.Lock()
	defer r.persistMu.Unlock()
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	idleMarked := r.idleAutoCompressApplied
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	historyRevision := r.historyRevision
	r.mu.Unlock()
	var inputBoxState json.RawMessage
	if r.inputBox != nil {
		inputBoxState = r.inputBox.Snapshot()
	}
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
			InputBoxState:           inputBoxState,
			HistoryRevision:         historyRevision,
			HookStore:               hooks.CloneSessionStore(r.orch.HookStoreSnapshot()),
			IdleAutoCompressApplied: idleMarked,
			NotifySeq:               notifySeq,
			AckSeq:                  ackSeq,
		},
	})
}

func (r *runtime) restoreInputBoxState(raw json.RawMessage) {
	if r == nil || r.inputBox == nil || len(raw) == 0 {
		return
	}
	if err := r.inputBox.Restore(raw); err != nil && r.logger != nil {
		r.logger.Warn("restore input box state failed", "session_id", r.session.ID, "error", err)
	}
}

// replacementData returns the in-memory state needed when a manager without
// a persistence store replaces a runtime. Production managers normally load
// this state from SQLite after persist; keeping this fallback prevents tests
// and embedded callers from losing history during a swap.
func (r *runtime) replacementData() ([]llm.Message, []skills.LoadedSkill, *turn.PendingHITL, int, map[string]json.RawMessage, bool, int, int, uint64, json.RawMessage) {
	if r == nil {
		return nil, nil, nil, 0, nil, false, 0, 0, 0, nil
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	idleMarked := r.idleAutoCompressApplied
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	historyRevision := r.historyRevision
	r.mu.Unlock()
	pending := r.pendingSnapshot()
	stepCount := r.stepIndexSnapshot()
	var hookStore map[string]json.RawMessage
	if r.orch != nil {
		hookStore = r.orch.HookStoreSnapshot()
	}
	var inputBoxState json.RawMessage
	if r.inputBox != nil {
		inputBoxState = r.inputBox.Snapshot()
	}
	return msgs, loaded, pending, stepCount, hookStore, idleMarked, notifySeq, ackSeq, historyRevision, inputBoxState
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
		r.sideEffects.ClearSession(r.session.ID, r.orch)
	}
	r.mu.Lock()
	r.sessionEpoch++
	newEpoch := r.sessionEpoch
	r.messages = nil
	r.historyRevision++
	r.loadedSkills = nil
	r.mu.Unlock()
	if r.inputBox != nil {
		r.inputBox.DropStale(newEpoch)
	}
	if r.orch != nil {
		r.orch.ClearHookStore()
		r.orch.SyncLoadedSkillHooks(nil)
		r.orch.ReloadLongTermMemory(ctx)
	}
	if r.store != nil {
		_ = r.store.ClearMessages(ctx, r.session.ID)
		// ClearMessages intentionally resets the legacy snapshot. Persist the
		// monotonic revision again so an older in-flight hydrate cannot become
		// newer merely because the context was cleared.
		r.persist(ctx)
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
		r.mu.Lock()
		r.historyRevision++
		r.mu.Unlock()
		r.persist(ctx)
	}
	return result
}

func (r *runtime) contextView() *ContextView {
	r.lifecycleMu.Lock()
	lifecycle := turn.CoordinatorSnapshot{}
	if r.turnCoordinator != nil {
		lifecycle = r.turnCoordinator.Snapshot()
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	queuePending := r.queue.Len()
	if r.inputBox != nil {
		queuePending += r.inputBox.Len()
	}
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
	r.lifecycleMu.Unlock()
	pending := r.pendingSnapshot()
	view.PendingToolCallsCount = pendingToolCallsCount(pending)
	view.SystemPrompt = r.orch.SystemPromptForSession(r.session.ID)
	if snapshot := r.orch.ModelContextSnapshot(r.session.ID); snapshot != nil {
		view.ContextInjectionDigest = snapshot.ContextInjectionDigest
		view.ContextInjectionCount = len(snapshot.ContextInjections)
	} else {
		injections := r.orch.ContextInjectionsForSession(r.session.ID)
		view.ContextInjectionCount = len(injections)
		if len(injections) > 0 {
			view.ContextInjectionDigest = turn.Digest(injections)
		}
	}
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
	if env.RequestType == queue.RequestTypeResume || env.RequestType == queue.RequestTypeSideEffectContinue || env.RequestType == queue.RequestTypeTurnContinuation {
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
	if env.RequestType == queue.RequestTypeAsyncToolResult {
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

// appendInput is the only runtime ingress for user/trigger/A2A data.  It
// assigns the current session epoch before appending so clear-context can
// invalidate an accepted-but-not-yet-consumed input without touching the
// order of newer records.
func (r *runtime) appendInput(kind InputKind, env queue.Envelope) (uint64, error) {
	if r == nil || r.inputBox == nil {
		return 0, fmt.Errorf("input box unavailable")
	}
	r.mu.Lock()
	if env.SessionEpoch == 0 {
		env.SessionEpoch = r.sessionEpoch
	}
	currentEpoch := r.sessionEpoch
	r.mu.Unlock()
	if env.SessionEpoch != currentEpoch {
		return 0, nil
	}
	seq, err := r.inputBox.Append(kind, env)
	if err == nil {
		// Persist the accepted tail before the consumer starts processing it.
		// This preserves inputs accepted while a Turn is waiting for approval.
		r.persist(context.Background())
	}
	return seq, err
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
	defer r.signalInputBox()
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
	historyChanged := false
	if pending != nil {
		if interruptMessage == "" {
			interruptMessage = "工具调用已被用户取消。"
		}
		r.orch.CancelPendingToolCalls(
			r.session.ID,
			&r.messages,
			pending,
			interruptMessage,
			metadata,
		)
		historyChanged = true
	}
	if active {
		if historyChanged {
			r.historyRevision++
		}
		r.mu.Unlock()
		cancel()
		if pending == nil {
			r.maybeScheduleContinueAfterCancel()
		}
		return true
	}
	repaired := r.orch.RepairUnrespondedToolCalls(r.session.ID, &r.messages)
	if repaired {
		historyChanged = true
	}
	if historyChanged {
		r.historyRevision++
	}
	r.mu.Unlock()
	if changed || repaired || historyChanged {
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
		r.sideEffects.ClearSession(r.session.ID, r.orch)
	}
	if r.inputBox != nil {
		r.inputBox.Close()
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
