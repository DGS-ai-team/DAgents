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
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
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

func newLifecycleCommandScope() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d-%d", time.Now().UnixNano(), atomic.AddUint64(&lifecycleScopeFallbackSeq, 1))
	}
	return hex.EncodeToString(b[:])
}

var lifecycleScopeFallbackSeq uint64

func newLogicalRoundID() string {
	return "round-" + strings.TrimPrefix(newContinuationID(), "turn-")
}

func newRoundRevisionID() string {
	return "revision-" + strings.TrimPrefix(newContinuationID(), "turn-")
}

// Session 表示对外可见的 session 元数据。
type Session struct {
	ID      string
	AgentID string
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

type runtime struct {
	session Session
	// InputBox is the FIFO ingress for user/trigger/child-agent inputs. MessageQueue is
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
	agentID       string
	executionGate *agentExecutionGate
	// 日志
	logger *slog.Logger

	// 技能目录。skillsCatalog 是控制面使用的 live catalog；
	// skillsTurnCatalog 是当前 human Turn 及其 continuation 使用的不可变视图。
	skillsCatalog     *skills.Catalog
	skillsTurnCatalog *skills.Catalog
	skillRevision     string
	// 上下文压缩逻辑
	compression *compression.Coordinator
	// Optional background memory extraction; it is stopped before the memory
	// service so late compression callbacks cannot use a closed database.
	candidatePipeline *memory.CandidatePipeline

	started bool
	done    chan struct{}

	mu              sync.Mutex         // 互斥锁
	persistMu       sync.Mutex         // serializes full runtime snapshots
	deltaMu         sync.Mutex         // protects explicit transcript/checkpoint dirty state
	ingressMu       sync.Mutex         // serializes input acceptance with fail-closed state changes
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
	onLifecycle     func(string, turn.CoordinatorSnapshot) error
	// lifecycleMu serializes compound Coordinator transitions. The
	// TurnCoordinator owns Turn/Step identity and generation; runtime keeps no
	// second lifecycle projection.
	lifecycleMu               sync.Mutex
	lifecycleCommandSeq       uint64
	lifecycleCommandScope     string
	lifecycleEventSeq         uint64
	lifecycleEventsLoaded     bool
	messages                  []llm.Message // 交互消息列表
	pendingInputMessage       *llm.Message
	historyRevision           uint64 // committed message snapshot revision
	conversationRevision      uint64
	contextCheckpointRevision uint64
	recordDeltaUpserts        map[string]pendingRecordMutation
	recordDeltaDeletes        map[string]uint64
	recordMutationSequence    uint64
	// Epochs are protected by deltaMu so the history sink can reject late
	// mutations from a turn invalidated by Clear without acquiring r.mu.
	historyMutationSessionEpoch               uint64
	historyMutationTurnEpoch                  uint64
	historyMutationErr                        error
	activeRoundID                             string // protected by deltaMu; logical round, not execution TurnID
	activeRevisionID                          string // protected by deltaMu; one immutable execution revision
	checkpointDirty                           bool
	checkpointMutationSequence                uint64
	checkpointFullReplaceSequence             uint64
	checkpointSyncedMutationSequence          uint64
	checkpointMutations                       []pendingCheckpointMutation
	historyFullReplaceSequence                uint64
	historyMutations                          []pendingCheckpointMutation
	historyCommitDirty                        bool   // in-memory history has not reached the durable runtime snapshot
	resultCommitStatus                        string // empty, committed, or failed; surfaced to hydrate/turn_state
	executionStopped                          bool   // fail closed after a result could not cross its durability boundary
	toolCallProjectionHiddenThroughGeneration uint64
	activeContextStart                        int    // durable transcript index visible to new model requests
	lastContextResetID                        string // idempotency fence for the last logical reset
	lastHumanInteraction                      time.Time
	loadedSkills                              []skills.LoadedSkill // 加载的技能列表
	pendingMemoryScope                        string               // scope changes wait for the next human Turn
	workspaceRoot                             string               // Agent 工作区根路径
	workspaceStateRoot                        string
	idleNewConversationSeconds                int
	media                                     *media.Registry // session 媒体索引（F-M1）
	memoryService                             memory.Service  // v2 workspace memory authority

	triggerDelivery triggers.DeliveryTracker // trigger 消息投递跟踪器

	sideEffects *sideEffectStore // 旁路回灌缓冲（子 session 跳过）

	childMeta *childRuntimeMeta // 子 Agent 元数据

	notifySeq int // F-E13：最后需 Client 关注的 SSE seq
	ackSeq    int // F-E13：Client 已确认看到的最大 SSE seq

	runtimeRevision  int64
	runtimeDigest    string
	llmProfileDigest string
	// runtimeMultimodalEnabled is the user-selected multimodal setting captured
	// when this Agent runtime was built. It lets the API detect a stale runtime
	// even when the persisted Agent revision did not change.
	runtimeMultimodalEnabled  bool
	turnBudget                turn.TurnBudget
	triggerActivationProvider func(context.Context, string, string, string) (bool, error)
	conditionValidator        func(context.Context, turn.ConditionApprovalMetadata) error
	autoAgent                 bool
	budgetResolver            func() (turn.TurnBudget, error)
	conditionCompletion       func(triggers.ConditionRequest, triggers.ConditionResult) error
	dreamingAttempt           *DreamingAttempt
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
	initialHookStore map[string]json.RawMessage,
	initialNotifySeq int,
	initialAckSeq int,
	turnOpts TurnOptions,
	triggerDelivery triggers.DeliveryTracker,
) *runtime {
	return newRuntimeWithPublisher(id, agentID, hub, hub, llmClient, registry, policyEngine, st, logger,
		initial, loaded, initialHookStore, initialNotifySeq, initialAckSeq, turnOpts, triggerDelivery)
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
	initialHookStore map[string]json.RawMessage,
	initialNotifySeq int,
	initialAckSeq int,
	turnOpts TurnOptions,
	triggerDelivery triggers.DeliveryTracker,
) *runtime {
	conversationFound := turnOpts.initialContextCheckpointRevision > 0 || turnOpts.initialConversationRevision > 0
	if st != nil && !conversationFound {
		conversationRevision, checkpointRevision, found, err := st.LoadConversationRevisions(context.Background(), id)
		if err == nil && found {
			turnOpts.initialConversationRevision = conversationRevision
			turnOpts.initialContextCheckpointRevision = checkpointRevision
			conversationFound = true
		}
	}
	workspaceRoot := effectiveWorkspaceRoot(turnOpts)
	if strings.TrimSpace(turnOpts.WorkspaceRoot) == "" {
		turnOpts.WorkspaceRoot = workspaceRoot
	}
	catalog := skills.NewCatalog(turnOpts.SkillsRoot, turnOpts.SkillsEnabled, turnOpts.SkillsMaxInPrompt)
	if turnOpts.SkillsVisibleRestrict {
		catalog.RestrictVisible(turnOpts.SkillsVisible)
	}
	turnCatalog := catalog.NewTurnView()
	if turnCatalog == nil {
		turnCatalog = catalog
	}
	journal := history.NewJournal(turnOpts.RawMessageHistoryEnabled, turnOpts.RawMessageHistoryDir, logger)
	var candidatePipeline *memory.CandidatePipeline
	if turnOpts.MemoryAutoExtract && llmClient != nil && turnOpts.MemoryService != nil {
		if consolidator, ok := turnOpts.MemoryService.(memory.Consolidator); ok {
			candidatePipeline = memory.NewCandidatePipeline(
				memory.NewLLMCandidateExtractor(llmClient, turnOpts.MemoryCandidateMaxItems, 24000),
				consolidator,
				turnOpts.MemoryCandidateQueueSize,
				func(err error) {
					if logger != nil {
						logger.Warn("background memory candidate pipeline failed", "session_id", id, "error", err)
					}
				},
			)
			candidatePipeline.SetCoreBudget(turnOpts.MemoryCoreBudgetTokens)
		}
	}
	rt := &runtime{
		session:               Session{ID: id, AgentID: agentID},
		inputBox:              NewInputBox(),
		queue:                 queue.NewMessageQueue(),
		turnCoordinator:       turn.NewTurnCoordinator(id, agentID),
		done:                  make(chan struct{}),
		lifecycleCommandScope: newLifecycleCommandScope(),
		// Session epochs start at one so the first human message is fenceable
		// against clear-context.
		sessionEpoch:                1,
		historyMutationSessionEpoch: 1,
		historyMutationTurnEpoch:    1,
		store:                       st,
		hub:                         eventHub,
		publisher:                   pub,
		agentID:                     agentID,
		logger:                      logger,
		skillsCatalog:               catalog,
		skillsTurnCatalog:           turnCatalog,
		compression: func() *compression.Coordinator {
			coord := compression.NewCoordinator(llmClient, turnOpts.CompressionSilent, turnOpts.CompressionBlocking)
			coord.SetLogger(logger)
			coord.SetRawMessageHistoryEnabled(turnOpts.RawMessageHistoryEnabled)
			coord.SetRawMessageHistoryRelativeRoot(turnOpts.RawMessageHistoryRelativeRoot)
			coord.SetCandidateSubmitter(candidatePipeline)
			if scoped, ok := turnOpts.MemoryService.(interface{ Scope() memory.Scope }); ok {
				coord.SetCandidateScope(scoped.Scope())
			}
			return coord
		}(),
		candidatePipeline:         candidatePipeline,
		messages:                  append([]llm.Message(nil), initial...),
		historyRevision:           turnOpts.initialHistoryRevision,
		conversationRevision:      turnOpts.initialConversationRevision,
		contextCheckpointRevision: turnOpts.initialContextCheckpointRevision,
		recordDeltaUpserts:        make(map[string]pendingRecordMutation),
		recordDeltaDeletes:        make(map[string]uint64),
		checkpointDirty:           turnOpts.initialContextCheckpointRevision == 0,
		toolCallProjectionHiddenThroughGeneration: turnOpts.initialToolCallProjectionHiddenThroughGeneration,
		activeContextStart:                        nonNegative(turnOpts.initialActiveContextStart),
		lastContextResetID:                        strings.TrimSpace(turnOpts.initialLastContextResetID),
		lastHumanInteraction:                      turnOpts.initialLastHumanInteraction,
		loadedSkills:                              append([]skills.LoadedSkill(nil), loaded...),
		skillRevision:                             turnCatalog.Revision(),
		workspaceRoot:                             workspaceRoot,
		workspaceStateRoot:                        strings.TrimSpace(turnOpts.WorkspaceStateRoot),
		idleNewConversationSeconds:                turnOpts.IdleNewConversationSeconds,
		triggerDelivery:                           triggerDelivery,
		sideEffects:                               newSideEffectStore(),
		notifySeq:                                 initialNotifySeq,
		ackSeq:                                    initialAckSeq,
		runtimeRevision:                           turnOpts.RuntimeRevision,
		runtimeDigest:                             strings.TrimSpace(turnOpts.RuntimeDigest),
		llmProfileDigest:                          strings.TrimSpace(turnOpts.LLMProfileDigest),
		runtimeMultimodalEnabled:                  turnOpts.MultimodalEnabled,
		turnBudget:                                turnOpts.Budget,
		triggerActivationProvider:                 turnOpts.TriggerActivationProvider,
		conditionValidator:                        turnOpts.ConditionValidator,
		conditionCompletion:                       turnOpts.ConditionCompletion,
		autoAgent:                                 turnOpts.AutoAgent,
		budgetResolver:                            turnOpts.BudgetResolver,
		onLifecycle:                               turnOpts.OnLifecycle,
		memoryService:                             turnOpts.MemoryService,
	}
	// A fresh runtime may receive an explicit seed transcript (tests and
	// embedded callers). Assign stable IDs once at this ingress boundary and
	// commit those explicit mutations; restored runtimes already carry a durable
	// checkpoint revision and must not rewrite unchanged records.
	if st != nil && !conversationFound && turnOpts.initialContextCheckpointRevision == 0 {
		rt.mu.Lock()
		for index := range rt.messages {
			message := rt.messages[index]
			if message.Name == llm.UserNameCompression {
				continue
			}
			if strings.TrimSpace(message.RecordID) == "" {
				id, err := llm.NewOpaqueRecordID()
				if err != nil {
					rt.deltaMu.Lock()
					rt.historyMutationErr = err
					rt.deltaMu.Unlock()
					continue
				}
				message.RecordID = id
				rt.messages[index].RecordID = id
			}
			rt.noteRecordUpsertLocked(message)
		}
		rt.mu.Unlock()
	}
	if len(turnOpts.initialDreamingAttempt) > 0 {
		var attempt DreamingAttempt
		if err := json.Unmarshal(turnOpts.initialDreamingAttempt, &attempt); err == nil && attempt.Validate() == nil {
			rt.dreamingAttempt = &attempt
		}
	}
	if candidatePipeline != nil {
		candidatePipeline.SetOnChange(func(report memory.ConsolidationReport) {
			if rt.publisher == nil {
				return
			}
			rt.publisher.Publish(rt.agentID, "memory/changed", map[string]any{
				"agent_id":           rt.agentID,
				"change_kind":        "consolidated",
				"candidate_count":    report.CandidateCount,
				"added":              report.Added,
				"duplicates":         report.Duplicates,
				"pending_conflicts":  report.PendingConflicts,
				"superseded":         report.Superseded,
				"store_revision":     report.StoreRevision,
				"effective_boundary": "next_turn",
			})
		})
	}
	if reg, err := media.NewRegistry(id, workspaceRoot); err == nil {
		rt.media = reg
	} else if logger != nil {
		logger.Warn("session media registry init failed", "session_id", id, "error", err)
	}
	promptReader := promptcontext.NewReader()
	if turnOpts.PromptContent != nil {
		promptReader.SetContent(*turnOpts.PromptContent)
	}
	promptReader.SetPreferredName(turnOpts.PreferredName)
	promptReader.SetFilter(promptcontext.Filter{
		SoulEnabled:   turnOpts.PromptContext.SoulEnabled,
		CustomEnabled: turnOpts.PromptContext.CustomEnabled,
	})
	// 创建编排器
	rt.orch = turn.NewOrchestrator(
		agentID,
		workspaceRoot,
		pub,
		llmClient,
		toolExec,
		policyEngine,
		turn.SkillAccess{
			Catalog:           turnCatalog,
			LiveCatalog:       catalog,
			Get:               rt.getLoadedSkills,
			Set:               rt.setLoadedSkills,
			SetWithHookStatus: rt.setLoadedSkillsWithHookStatus,
		},
		promptReader,
		journal,
		hooks.RuntimeConfig{
			Duplicate: hooks.DuplicateConfigOrDefault(turnOpts.DuplicateToolCall),
			ToolResult: hooks.ToolResultConfigOrDefault(hooks.ToolResultConfig{
				Enabled:              turnOpts.ToolResult.Enabled,
				SpillThresholdTokens: turnOpts.ToolResult.SpillThresholdTokens,
				Tools:                turnOpts.ToolResult.Tools,
				WorkspaceRoot:        workspaceRoot,
				AgentID:              turnOpts.AgentID,
			}),
			InjectTodayDate: hooks.InjectTodayDateConfigOrDefault(turnOpts.InjectTodayDate),
			Plugins:         turnOpts.PluginHooks,
			Logger:          logger,
		},
		logger,
	)
	rt.orch.SetRuntimeRoot(turnOpts.RuntimeDir)
	rt.orch.SetHookHostConfig(turnOpts.HookHost)
	rt.orch.SetRuntimeIdentity(rt.runtimeRevision, rt.runtimeDigest)
	rt.orch.SetHandbookReader(turnOpts.HandbookReader)
	rt.orch.SetAgentPromptProvider(turnOpts.AgentPromptProvider)
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
	rt.orch.SetHistoryMutationSink(rt.trackHistoryMutation)
	rt.orch.SetExecutionFence(func(execution turn.TurnExecutionContext) bool {
		if execution.SessionID != rt.session.ID || rt.turnCoordinator == nil {
			return false
		}
		return rt.turnCoordinator.IsCurrentExecution(execution)
	})
	rt.orch.SetToolExecutionStatusReader(func(sessionID, toolCallID string) (turn.ToolExecutionStatus, bool) {
		if sessionID != rt.session.ID || rt.turnCoordinator == nil {
			return "", false
		}
		return rt.turnCoordinator.ToolExecutionStatusForCall(toolCallID)
	})
	rt.orch.SetToolBudgetCheck(func(sessionID, toolCallID string) (bool, string) {
		if sessionID != rt.session.ID || rt.turnCoordinator == nil {
			return true, ""
		}
		decision := rt.turnCoordinator.BudgetDecisionForToolCall(toolCallID)
		return decision.Allowed, decision.Reason
	})
	if len(initialHookStore) > 0 {
		rt.orch.SetHookStore(initialHookStore)
	}
	if turnOpts.MemoryService != nil {
		rt.orch.SetMemoryService(turnOpts.MemoryService)
		rt.orch.SetMemoryAutoRecall(turnOpts.MemoryAutoRecall)
		rt.orch.SetMemoryCoreBudgetTokens(turnOpts.MemoryCoreBudgetTokens)
	}
	rt.orch.SyncLoadedSkillHooks(loaded)
	if turnOpts.initialLifecycleEventsLoaded {
		rt.restoreLifecycleEventsFrom(turnOpts.initialLifecycleEvents, true)
	} else {
		rt.restoreLifecycleEvents()
	}
	if st != nil {
		if lifecycle := rt.turnCoordinator.Snapshot(); lifecycle.HasActiveTurn && !lifecycle.TurnStatus.Terminal() {
			if revision, err := st.LoadRoundRevisionForTurn(context.Background(), id, lifecycle.TurnID); err == nil && revision != nil && revision.Status == "active" {
				rt.deltaMu.Lock()
				rt.activeRoundID, rt.activeRevisionID = revision.RoundID, revision.RevisionID
				rt.deltaMu.Unlock()
			}
		}
	}
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
		r.pendingMemoryScope = scope
		r.mu.Unlock()
		return
	}
	r.setMemoryScope(scope)
}

// setMemoryScope keeps every runtime consumer on the same authorized scope.
// The Orchestrator serves model-facing recall/tools while the compression
// coordinator stamps background extraction candidates; updating only one of
// them would make a scope change visible to the model but stale for extraction.
func (r *runtime) setMemoryScope(scope string) {
	if r == nil {
		return
	}
	if r.orch != nil {
		r.orch.SetMemoryScope(scope)
	}
	if r.compression != nil {
		r.compression.SetCandidateScope(memory.Scope(strings.TrimSpace(scope)))
	}
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
	changed := turn.Digest(before) != turn.Digest(items)
	hookSync := turn.SkillHooksSyncResult{Status: "unavailable"}
	if r.orch != nil {
		hookSync = r.orch.SyncLoadedSkillHooks(items)
		if changed && r.turnState() != turn.StateIdle {
			r.orch.RequestModelContextRefresh(r.session.ID, "skills_api_update")
		}
	}
	if changed && r.hub != nil {
		boundary := "next_human_turn"
		if r.turnState() != turn.StateIdle {
			boundary = "next_model_step"
		}
		r.hub.Publish(r.session.ID, "skills/changed", map[string]any{
			"agent_id":         r.agentID,
			"loaded_skills":    append([]skills.LoadedSkill(nil), items...),
			"applied_boundary": boundary,
			"change":           "loaded_set",
		})
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
		if r.executionIsStopped() {
			return
		}
		// Control/continuation records are drained before a new external input.
		// A human or trigger arriving while HITL is pending therefore stays in
		// InputBox and cannot preempt the active Turn.
		if r.executionGate != nil {
			if env, ok := r.executionGate.claimControl(ctx, r); ok {
				if r.acceptEnvelope(env) {
					r.dispatchTurnRequest(ctx, env)
				}
				r.executionGate.finishDispatch()
				r.signalInputBox()
				continue
			}
		} else if r.queue.Len() > 0 {
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
		if r.executionGate != nil {
			blocked, gateWake := r.executionGate.waitSnapshot()
			if !blocked {
				goto claimInput
			}
			select {
			case <-ctx.Done():
				return
			case <-r.inputBox.Wake():
			case <-gateWake:
			}
			continue
		}
	claimInput:
		var record InputRecord
		var ok bool
		if r.executionGate != nil {
			record, ok = r.executionGate.claimInput(r)
		} else {
			record, ok = r.popInputIfIdle()
		}
		if ok {
			// Persist the ownership transfer before executing the Turn. If the
			// process stops during a tool call, startup can recover this input
			// alongside the lifecycle projection instead of replaying it after
			// the recovered continuation.
			if err := r.persist(context.Background()); err != nil {
				// Ownership must be durable before any trigger side effect starts.
				// Put the record back and fail closed; retrying in a hot loop can
				// spin forever while the persistence backend is unavailable.
				r.inputBox.RequeueInFlight()
				if r.executionGate != nil {
					r.executionGate.finishDispatch()
				}
				return
			}
			consumed := true
			if r.acceptEnvelope(record.Env) {
				consumed = r.dispatchInput(ctx, record)
			}
			if consumed && r.inputBox != nil {
				r.inputBox.MarkCompleted(record.Seq)
				if err := r.persist(context.Background()); err == nil {
					r.inputBox.Ack(record.Seq)
					if err := r.persist(context.Background()); err == nil {
						r.clearTriggerDelivery(record.Env)
					} else {
						// The durable snapshot still contains the completed in-flight
						// record. Stop before consuming another input; otherwise a
						// later turn could overwrite that recovery boundary.
						r.inputBox.RestoreCompletedInFlight(record)
						if r.executionGate != nil {
							r.executionGate.finishDispatch()
						}
						return
					}
				} else {
					// Keep the completed in-flight guard and fail closed.
					if r.executionGate != nil {
						r.executionGate.finishDispatch()
					}
					return
				}
			}
			if r.executionGate != nil {
				r.executionGate.finishDispatch()
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
		case <-r.executionWake():
		}
	}
}

func (r *runtime) signalInputBox() {
	if r != nil && r.inputBox != nil {
		r.inputBox.Signal()
	}
}

func (r *runtime) executionWake() <-chan struct{} {
	if r == nil || r.executionGate == nil {
		return nil
	}
	return r.executionGate.Wake()
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

func (r *runtime) dispatchInput(ctx context.Context, record InputRecord) bool {
	env := record.Env
	isTrigger := record.Kind == InputKindTrigger || record.Kind == InputKindSystemAuto
	if isTrigger && record.RecoveredLegacy {
		// Legacy restored trigger envelopes have no durable delivery identity;
		// fail closed rather than replaying an unknown side effect.
		return true
	}
	if blocked, ok := r.triggerDelivery.(triggers.RecoveryDeliveryTracker); ok && isTrigger && blocked.IsRecoveryRequired(strings.TrimSpace(env.TriggerID)) {
		// Discard the recovered mailbox item without executing it or clearing the
		// durable recovery fence. An explicit recovery action must clear that fence.
		return true
	}
	if identity, ok := r.triggerDelivery.(triggers.DeliveryIdentityTracker); ok && isTrigger && strings.TrimSpace(env.DeliveryID) != "" && !identity.IsPendingDelivery(strings.TrimSpace(env.TriggerID), strings.TrimSpace(env.DeliveryID)) {
		// The delivery was explicitly recovered (or superseded); consume the
		// mailbox record without replaying its task.
		return true
	}
	// InputBox records are data-plane inputs. They all enter the normal human
	// turn path; the UserName/source fields preserve whether the producer was
	// an actual user or a trigger.
	env.RequestType = queue.RequestTypeMessage
	source := turn.TurnSourceHuman
	if isTrigger {
		source = turn.TurnSourceTrigger
	} else if record.Kind == InputKindChildAgent {
		source = turn.TurnSourceChildAgent
	}
	if isTrigger && r.triggerActivationProvider != nil {
		if trusted, err := r.triggerActivationProvider(ctx, r.agentID, strings.TrimSpace(env.TriggerID), strings.TrimSpace(env.DeliveryID)); err != nil {
			if r.logger != nil {
				r.logger.Warn("trusted trigger activation profile unavailable; activation dropped", "session_id", r.session.ID, "trigger_id", env.TriggerID, "error", err)
			}
			return true
		} else if trusted {
			if record.Kind == InputKindSystemAuto {
				ctx = tools.WithTrustedAutoIdleActivation(ctx, r.agentID, strings.TrimSpace(env.TriggerID), strings.TrimSpace(env.DeliveryID))
				if r.orch != nil {
					r.orch.BeginTrustedAutoIdleActivation(r.session.ID)
				}
			}
		}
	}
	consumed := r.handleInputMessage(ctx, record, source)
	return consumed
}

func (r *runtime) clearTriggerDelivery(env queue.Envelope) {
	if r == nil || r.triggerDelivery == nil || strings.TrimSpace(env.TriggerID) == "" {
		return
	}
	if blocked, ok := r.triggerDelivery.(triggers.RecoveryDeliveryTracker); ok && blocked.IsRecoveryRequired(strings.TrimSpace(env.TriggerID)) {
		return
	}
	if identity, ok := r.triggerDelivery.(triggers.DeliveryIdentityTracker); ok && strings.TrimSpace(env.DeliveryID) != "" {
		identity.ClearPendingDeliveryIfMatch(strings.TrimSpace(env.TriggerID), strings.TrimSpace(env.DeliveryID))
	} else {
		r.triggerDelivery.ClearPendingDelivery(strings.TrimSpace(env.TriggerID))
	}
}

// dispatchTurnRequest is the single runtime entry point for control and
// recovery queue commands. Keeping that translation in one place prevents
// producers from bypassing lifecycle fencing, snapshots, or recovery bookkeeping.
func (r *runtime) dispatchTurnRequest(ctx context.Context, env queue.Envelope) {
	switch env.RequestType {
	case queue.RequestTypeResume:
		r.logResumeDequeued(env.ResumeValue)
		r.handleResume(ctx, env.ResumeValue)
	case queue.RequestTypeAsyncToolResult:
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
	r.deltaMu.Lock()
	syncedMutationSequence := r.checkpointSyncedMutationSequence
	fullReplaceSequence := r.historyFullReplaceSequence
	mutationSequence := r.checkpointMutationSequence
	mutations := make([]pendingCheckpointMutation, 0, len(r.historyMutations))
	for _, mutation := range r.historyMutations {
		if mutation.Sequence > syncedMutationSequence {
			mutations = append(mutations, mutation)
		}
	}
	r.deltaMu.Unlock()
	if fullReplaceSequence <= syncedMutationSequence && len(mutations) > 0 {
		if updated, ok := applyHistoryMutations(r.messages, r.activeContextStart, mutations); ok {
			r.messages = updated
			r.historyRevision++
			r.deltaMu.Lock()
			r.checkpointSyncedMutationSequence = mutationSequence
			remaining := r.historyMutations[:0]
			for _, mutation := range r.historyMutations {
				if mutation.Sequence > mutationSequence {
					remaining = append(remaining, mutation)
				}
			}
			r.historyMutations = remaining
			r.deltaMu.Unlock()
			return true
		}
	}
	merged := *history
	if r.activeContextStart > 0 && r.activeContextStart <= len(r.messages) {
		merged = append(append([]llm.Message(nil), r.messages[:r.activeContextStart]...), (*history)...)
	}
	// Pending input history may already have been committed as part of the
	// durable begin-turn boundary. When Orchestrator returns that same source
	// item again, update its payload in place instead of creating a duplicate
	// transcript fact. Stable identities also make assistant partial→final
	// updates idempotent here.
	positions := make(map[string]int, len(merged))
	compact := make([]llm.Message, 0, len(merged))
	for _, message := range merged {
		id := strings.TrimSpace(message.RecordID)
		if id != "" {
			if index, ok := positions[id]; ok {
				compact[index] = message
				continue
			}
			positions[id] = len(compact)
		}
		compact = append(compact, message)
	}
	merged = compact
	if turn.Digest(r.messages) == turn.Digest(merged) {
		if mutationSequence > syncedMutationSequence {
			r.deltaMu.Lock()
			r.checkpointSyncedMutationSequence = mutationSequence
			remaining := r.historyMutations[:0]
			for _, mutation := range r.historyMutations {
				if mutation.Sequence > mutationSequence {
					remaining = append(remaining, mutation)
				}
			}
			r.historyMutations = remaining
			r.deltaMu.Unlock()
		}
		return false
	}
	r.messages = append([]llm.Message(nil), merged...)
	r.historyRevision++
	if fullReplaceSequence <= syncedMutationSequence || len(mutations) == 0 {
		r.noteCheckpointDirtyLocked()
	}
	r.deltaMu.Lock()
	r.checkpointSyncedMutationSequence = r.checkpointMutationSequence
	remaining := r.historyMutations[:0]
	for _, mutation := range r.historyMutations {
		if mutation.Sequence > r.checkpointSyncedMutationSequence {
			remaining = append(remaining, mutation)
		}
	}
	r.historyMutations = remaining
	r.deltaMu.Unlock()
	return true
}

func applyHistoryMutations(messages []llm.Message, activeStart int, mutations []pendingCheckpointMutation) ([]llm.Message, bool) {
	if activeStart < 0 || activeStart > len(messages) {
		return nil, false
	}
	length := len(messages)
	for _, mutation := range mutations {
		operation := mutation.Operation
		activeLength := length - activeStart
		switch operation.Kind {
		case string(turn.HistoryMutationAppend):
			length++
		case string(turn.HistoryMutationInsert):
			if operation.Index < 0 || operation.Index > activeLength {
				return nil, false
			}
			length++
		case string(turn.HistoryMutationReplace):
			if operation.Index < 0 || operation.Index >= activeLength {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	updated := messages
	for _, mutation := range mutations {
		operation := mutation.Operation
		activeLength := len(updated) - activeStart
		switch operation.Kind {
		case string(turn.HistoryMutationAppend):
			updated = append(updated, operation.Message)
		case string(turn.HistoryMutationInsert):
			position := activeStart + operation.Index
			updated = append(updated, llm.Message{})
			copy(updated[position+1:], updated[position:len(updated)-1])
			updated[position] = operation.Message
		case string(turn.HistoryMutationReplace):
			if operation.Index >= activeLength {
				return nil, false
			}
			updated[activeStart+operation.Index] = operation.Message
		}
	}
	return updated, true
}

// acceptEnvelope filters stale control continuations while preserving
// external facts for side-effect reconciliation after a turn is cancelled.
func (r *runtime) acceptEnvelope(env queue.Envelope) bool {
	if r.executionIsStopped() {
		return false
	}
	r.mu.Lock()
	epoch := r.sessionEpoch
	r.mu.Unlock()
	state := r.turnCoordinator.Snapshot()
	turnID, generation := state.TurnID, state.Generation
	validEpoch := env.SessionEpoch == 0 || env.SessionEpoch == epoch
	validTurn := true
	switch env.RequestType {
	case queue.RequestTypeTurnContinuation, queue.RequestTypeResume:
		validTurn = state.HasActiveTurn && env.TurnID != "" && env.TurnID == turnID && env.Generation == generation
	case queue.RequestTypeSideEffectContinue:
		if env.TurnID == "" && env.Generation == 0 && !state.HasActiveTurn {
			// A side-effect continuation may arrive before a new Coordinator Turn
			// is opened while reconciling an external completion.
			validTurn = true
		} else {
			validTurn = state.HasActiveTurn && env.TurnID != "" && env.TurnID == turnID && env.Generation == generation
		}
	}
	if !validEpoch {
		if r.logger != nil {
			r.logger.Info("stale session event dropped", "session_id", r.session.ID, "request_type", env.RequestType, "event_epoch", env.SessionEpoch, "session_epoch", epoch)
		}
		return false
	}
	switch env.RequestType {
	case queue.RequestTypeTurnContinuation, queue.RequestTypeResume, queue.RequestTypeSideEffectContinue:
		if !validTurn {
			if r.logger != nil {
				r.logger.Info("stale turn continuation dropped", "session_id", r.session.ID, "request_type", env.RequestType, "event_turn_id", env.TurnID, "turn_id", turnID, "event_generation", env.Generation, "generation", generation)
			}
			return false
		}
	}
	return true
}

func (r *runtime) executionIsStopped() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.executionStopped
}

func (r *runtime) handleInputMessage(parent context.Context, record InputRecord, source turn.TurnSource) bool {
	env := record.Env
	resubmit := env.ConversationResubmit
	if !r.sessionEpochCurrent(env.SessionEpoch) {
		r.logger.Info("stale human message dropped after session clear", "session_id", r.session.ID)
		return true
	}
	userMsg, err := r.buildInputUserMessage(env)
	if err != nil {
		r.logger.Warn("invalid user message", "session_id", r.session.ID, "error", err)
		return true
	}
	userMsg.RecordID = llm.InputRecordID(r.session.ID, record.Seq)
	userMsg.ClientRequestID = record.ClientRequestID
	// A new input must never preempt an active Turn. InputBox normally pops only
	// while idle; keep this defensive guard for races and direct test fixtures.
	if state := r.turnCoordinator.Snapshot(); state.HasActiveTurn && !state.TurnStatus.Terminal() {
		r.logger.Info("human input deferred while turn is active", "session_id", r.session.ID)
		if r.inputBox != nil {
			r.inputBox.RequeueInFlight()
		}
		r.signalInputBox()
		return false
	}
	if source == turn.TurnSourceHuman && resubmit == nil {
		if err := r.maybeStartIdleConversation(parent, env.SessionEpoch); err != nil && r.logger != nil {
			r.logger.Warn("idle conversation boundary failed; retaining existing context", "session_id", r.session.ID, "error", err)
		}
	}
	r.applyPendingMemoryScope()
	r.observeSkillCatalogChange()
	var resubmitCheckpoint *store.CheckpointCommit
	var previousMessages []llm.Message
	var previousContextStart int
	if resubmit != nil {
		if r.store == nil {
			r.logger.Warn("conversation resubmit rejected without durable history", "session_id", r.session.ID)
			return true
		}
		r.ingressMu.Lock()
		if !r.sessionEpochCurrent(env.SessionEpoch) {
			r.ingressMu.Unlock()
			return true
		}
		checkpoint, err := r.store.LoadConversationCheckpointRevision(parent, r.session.ID, resubmit.PreCheckpointRevision)
		if err != nil || checkpoint == nil || checkpoint.Revision != resubmit.PreCheckpointRevision {
			r.ingressMu.Unlock()
			if r.logger != nil {
				r.logger.Warn("conversation resubmit checkpoint unavailable", "session_id", r.session.ID, "round_id", resubmit.RoundID, "error", err)
			}
			return false
		}
		r.mu.Lock()
		r.deltaMu.Lock()
		if len(r.recordDeltaUpserts) != 0 || len(r.recordDeltaDeletes) != 0 || r.checkpointDirty || r.historyCommitDirty {
			r.deltaMu.Unlock()
			r.mu.Unlock()
			r.ingressMu.Unlock()
			if r.logger != nil {
				r.logger.Warn("conversation resubmit rejected with uncommitted history", "session_id", r.session.ID)
			}
			return false
		}
		previousMessages = append([]llm.Message(nil), r.messages...)
		previousContextStart = r.activeContextStart
		r.messages = append([]llm.Message(nil), checkpoint.Messages...)
		r.activeContextStart = 0
		r.deltaMu.Unlock()
		r.mu.Unlock()
		resubmitCheckpoint = &store.CheckpointCommit{Messages: append([]llm.Message(nil), checkpoint.Messages...), Lineage: "resubmit_base"}
	}
	r.mu.Lock()
	r.pendingInputMessage = &userMsg
	r.mu.Unlock()
	roundID, revisionID := newLogicalRoundID(), newRoundRevisionID()
	if resubmit != nil {
		roundID, revisionID = resubmit.RoundID, newRoundRevisionID()
	}
	var lifecycleErr error
	if resubmit != nil {
		lifecycleErr = r.lifecycleBeginInputTurnIdentityWithResubmit(source, roundID, revisionID, resubmit, resubmitCheckpoint)
	} else {
		lifecycleErr = r.lifecycleBeginInputTurnIdentity(source, roundID, revisionID)
	}
	if lifecycleErr != nil {
		r.mu.Lock()
		r.pendingInputMessage = nil
		r.mu.Unlock()
		if resubmit != nil {
			state := r.turnCoordinator.Snapshot()
			committed := state.HasActiveTurn && !state.TurnStatus.Terminal() && r.activeRoundID == resubmit.RoundID && r.activeRevisionID == revisionID
			if !committed {
				r.mu.Lock()
				r.messages = previousMessages
				r.activeContextStart = previousContextStart
				r.mu.Unlock()
			}
			r.ingressMu.Unlock()
		}
		if r.logger != nil {
			r.logger.Warn("start human turn lifecycle failed", "session_id", r.session.ID, "error", lifecycleErr)
		}
		if r.markResultCommitFailed() {
			r.publishTurnState(r.turnCoordinator.Snapshot(), "")
		}
		// The input remains uncompleted in InputBox. Do not acknowledge or
		// persist it as consumed when the atomic start boundary failed. The
		// runtime is stopped until recovery because the accepted input's
		// lifecycle claim could not be made durable.
		return false
	}
	if resubmit != nil {
		r.ingressMu.Unlock()
	}
	r.mu.Lock()
	r.pendingInputMessage = nil
	r.noteRecordUpsertLocked(userMsg)
	r.noteCheckpointMutationPendingLocked()
	r.mu.Unlock()
	// Clear-context may have won the race while lifecycleBeginHumanTurn was
	// opening the new turn. Do not let an already accepted queue envelope from
	// before the clear become the first message of the new context.
	if !r.sessionEpochCurrent(env.SessionEpoch) {
		r.cancelTurn()
		return true
	}
	historyStart := r.lifecycleHistoryLength()

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
	return true
}

func (r *runtime) buildInputUserMessage(env queue.Envelope) (llm.Message, error) {
	userName := llm.NormalizeUserMessageName(env.UserName)
	userMsg, err := llm.BuildUserMessageWithFileReferences(env.Content, env.ContentParts, userName, env.FileReferences)
	if err != nil {
		return llm.Message{}, err
	}
	if r.media != nil && llm.MessageHasImages(userMsg) {
		persisted, perr := media.PersistUserMessageImages(r.media, userMsg)
		if perr != nil {
			return llm.Message{}, perr
		}
		userMsg = persisted
	}
	return userMsg, nil
}

// runInlineToolContinuationChain keeps tool results inside the logical Turn.
// Each continuation starts a new lifecycle Step but remains in this serialized
// Turn chain, while MessageQueue only carries explicit control/recovery events.
func (r *runtime) runInlineToolContinuationChain(parent context.Context, expectedEpoch uint64, outcome turn.StepOutcome) turn.StepOutcome {
	if r.executionIsStopped() {
		outcome.ScheduleToolResult = false
		if outcome.Err == nil {
			outcome.Err = fmt.Errorf("result_commit_recovery_required")
		}
		return outcome
	}
	for outcome.Err == nil && outcome.Pending == nil && outcome.ScheduleToolResult {
		if r.executionIsStopped() {
			outcome.ScheduleToolResult = false
			outcome.Err = fmt.Errorf("result_commit_recovery_required")
			break
		}
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

// applyPendingMemoryScope starts the next human Turn with a scope that was
// changed while the preceding Turn was active (including an interrupted HITL
// turn). It is deliberately not called from tool continuations.
func (r *runtime) applyPendingMemoryScope() {
	if r == nil || r.orch == nil {
		return
	}
	r.mu.Lock()
	scope := r.pendingMemoryScope
	r.pendingMemoryScope = ""
	r.mu.Unlock()
	if scope == "" {
		return
	}
	r.setMemoryScope(scope)
}

func (r *runtime) handleTurnContinuation(parent context.Context) {
	dreamingContinuation := false
	r.mu.Lock()
	if r.dreamingAttempt != nil && (r.dreamingAttempt.State == DreamingAttemptRunning || r.dreamingAttempt.State == DreamingAttemptWaiting) {
		dreamingContinuation = r.turnCoordinator.Snapshot().TurnID == r.dreamingAttempt.TurnID
	}
	r.mu.Unlock()
	continuationCtx := parent
	if dreamingContinuation {
		continuationCtx = tools.WithHandbookMaintenance(parent)
	}
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
	outcome, history := r.runTurnStepWithSideEffects(continuationCtx, true, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		return r.orch.RunToolMessageTurn(ctx, r.session.ID, history)
	})
	if err := r.lifecycleAfterModelStep(outcome, history, historyStart); err != nil {
		r.logger.Warn("finish turn continuation lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(continuationCtx, 0, outcome)
	r.finishTurnIdle(outcome)
	if dreamingContinuation {
		if err := r.completeDreamingResume(history, outcome); err != nil && r.logger != nil {
			r.logger.Warn("complete dreaming resume failed", "session_id", r.session.ID, "error", err)
		}
	}
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
	for _, item := range pending.Items {
		if item.ConditionApproval != nil {
			r.handleConditionResume(parent, resumeValue, pending)
			return
		}
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
	dreamingResume := false
	r.mu.Lock()
	if r.dreamingAttempt != nil && r.dreamingAttempt.State == DreamingAttemptWaiting {
		dreamingResume = r.turnCoordinator.Snapshot().TurnID == r.dreamingAttempt.TurnID
	}
	r.mu.Unlock()
	resumeCtx := parent
	if dreamingResume {
		resumeCtx = tools.WithHandbookMaintenance(parent)
	}
	if err := r.lifecyclePrepareResume(resumeValue); err != nil {
		r.logger.Warn("prepare resume lifecycle failed", "session_id", r.session.ID, "error", err)
		r.persist(context.Background())
		return
	}

	outcome, history := r.runTurnStepWithSideEffects(resumeCtx, false, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		return r.orch.ContinueAfterResume(ctx, r.session.ID, history, resumeValue, pending)
	})
	if err := r.lifecycleAfterResume(outcome, history); err != nil {
		r.logger.Warn("finish resume lifecycle failed", "session_id", r.session.ID, "error", err)
		if outcome.Err == nil {
			outcome.Err = err
		}
	}
	r.commitHistoryFallback(history)
	outcome = r.runInlineToolContinuationChain(resumeCtx, 0, outcome)
	r.finishTurnIdle(outcome)
	if dreamingResume {
		if err := r.completeDreamingResume(history, outcome); err != nil && r.logger != nil {
			r.logger.Warn("complete dreaming resume failed", "session_id", r.session.ID, "error", err)
		}
	}
	r.persist(context.Background())
}

func (r *runtime) handleConditionResume(parent context.Context, resumeValue map[string]any, pending *turn.PendingHITL) (turn.StepOutcome, error) {
	if pending == nil || len(pending.Items) != 1 || pending.Items[0].ConditionApproval == nil {
		return turn.StepOutcome{}, fmt.Errorf("condition approval is missing")
	}
	// Validate the resume payload before resolving the durable interaction. A
	// malformed payload must leave the approval pending so the user can retry.
	if _, err := clihitl.ParseApprovalResume(resumeValue, []string{pending.Items[0].ToolCall.ID}); err != nil {
		r.logger.Warn("invalid condition approval resume", "session_id", r.session.ID, "error", err)
		return turn.StepOutcome{}, err
	}
	item := pending.Items[0]
	if r.conditionValidator != nil {
		if err := r.conditionValidator(parent, *item.ConditionApproval); err != nil {
			return turn.StepOutcome{}, err
		}
	}
	if err := r.lifecyclePrepareResume(resumeValue); err != nil {
		r.logger.Warn("prepare condition approval resume failed", "session_id", r.session.ID, "error", err)
		return turn.StepOutcome{}, err
	}
	// Fence the external side effect before opening the command.  A restart
	// after this fact must reconcile the execution rather than replay bash.
	state := r.turnCoordinator.Snapshot()
	if _, err := r.lifecycleDispatchErr(turn.TurnCommand{Type: turn.CommandToolExecutionStarted,
		SessionID: r.session.ID, TurnID: state.TurnID, StepID: state.StepID,
		Generation: state.Generation, ToolCallID: item.ToolCall.ID,
		ToolExecutionID: item.ToolCall.ID + "-execution", At: time.Now().UTC()}); err != nil {
		r.logger.Warn("start condition execution lifecycle failed", "session_id", r.session.ID, "error", err)
		return turn.StepOutcome{}, err
	}
	conditionRejected := false
	outcome, history := r.runTurnStepWithSideEffects(parent, false, func(ctx context.Context, history *[]llm.Message) turn.StepOutcome {
		result, err := r.orch.ExecuteConditionApproval(ctx, r.session.ID, pending, resumeValue)
		conditionRejected = result.Rejected
		return turn.StepOutcome{ConditionHandled: true, ConditionMatched: result.Matched,
			ConditionToolCallID: item.ToolCall.ID, ConditionExecutionID: item.ToolCall.ID + "-execution",
			ConditionResult: result.ResultContent, StepIndex: turn.StepIndexFromContext(ctx), Err: err}
	})
	if err := r.lifecycleAfterResume(outcome, history); err != nil {
		r.logger.Warn("finish condition approval lifecycle failed", "session_id", r.session.ID, "error", err)
		return outcome, err
	}
	r.finishTurnIdle(outcome)
	r.persist(context.Background())
	if r.conditionCompletion != nil && pending.Items[0].ConditionApproval != nil {
		meta := pending.Items[0].ConditionApproval
		status := triggers.ConditionNotMatched
		if execution, ok := r.turnCoordinator.ToolExecutionStatusForCall(item.ToolCall.ID); ok && execution == turn.ToolExecutionStatusSucceeded {
			status = triggers.ConditionMatched
		}
		if err := r.conditionCompletion(triggers.ConditionRequest{TriggerID: meta.TriggerID, DeliveryID: meta.DeliveryID, SessionID: r.session.ID, AgentID: meta.AgentID, Revision: meta.TriggerRevision, Occurrence: meta.Occurrence}, triggers.ConditionResult{Status: status, Rejected: conditionRejected, Failed: outcome.Err != nil}); err != nil {
			if r.logger != nil {
				r.logger.Error("condition completion callback failed", "session_id", r.session.ID, "trigger_id", meta.TriggerID, "delivery_id", meta.DeliveryID, "error", err)
			}
			return outcome, fmt.Errorf("condition completion callback: %w", err)
		}
	}
	return outcome, nil
}

func (r *runtime) commitHistoryFallback(history []llm.Message) {
	r.mu.Lock()
	if !r.turnEpochCurrentLocked() {
		r.mu.Unlock()
		return
	}
	changed := r.commitStepHistory(&history)
	if changed {
		r.historyCommitDirty = true
	}
	needsSave := r.historyCommitDirty
	r.mu.Unlock()
	if needsSave {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := r.persist(ctx)
		cancel()
		if err != nil {
			r.lifecycleMu.Lock()
			changed := r.markResultCommitFailed()
			state := r.turnCoordinator.Snapshot()
			r.lifecycleMu.Unlock()
			if changed {
				r.publishTurnState(state, "")
			}
			return
		}
		r.mu.Lock()
		r.historyCommitDirty = false
		if !r.executionStopped {
			r.resultCommitStatus = "committed"
		}
		r.mu.Unlock()
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

func (r *runtime) clearMessages(ctx context.Context) error {
	// Context clearing is a logical cancellation boundary. Keep the durable
	// Turn terminal event even though the durable transcript snapshot is removed;
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
	// Serialize the epoch change with every external input admission. Without
	// this boundary, a POST could persist an old-epoch receipt after clear had
	// already removed its mailbox entry and still receive an accepted response.
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	// A prior snapshot writer must finish before the clear mutates memory. Keep
	// the lifecycle epoch, transcript clear, and replacement snapshot within one
	// serialized durability boundary so an old Save cannot resurrect history.
	r.lifecycleMu.Lock()
	r.persistMu.Lock()
	r.mu.Lock()
	r.sessionEpoch++
	newEpoch := r.sessionEpoch
	r.deltaMu.Lock()
	r.historyMutationSessionEpoch = newEpoch
	r.deltaMu.Unlock()
	r.toolCallProjectionHiddenThroughGeneration = r.turnCoordinator.Snapshot().Generation
	r.messages = nil
	r.activeContextStart = 0
	r.lastContextResetID = ""
	r.lastHumanInteraction = time.Now().UTC()
	r.historyRevision++
	r.noteCheckpointDirtyLocked()
	r.loadedSkills = nil
	r.mu.Unlock()
	if r.inputBox != nil {
		r.inputBox.DropStale(newEpoch)
	}
	if r.orch != nil {
		r.orch.ClearHookStore()
		r.orch.SyncLoadedSkillHooks(nil)
	}
	var persistErr error
	if r.store != nil {
		var inputState json.RawMessage
		if r.inputBox != nil {
			inputState = r.inputBox.SnapshotForPersistence()
		}
		var hookStore map[string]json.RawMessage
		if r.orch != nil {
			hookStore = hooks.CloneSessionStore(r.orch.HookStoreSnapshot())
		}
		r.mu.Lock()
		state := store.RuntimeState{InputBoxState: inputState, HistoryRevision: r.historyRevision, ConversationRevision: r.conversationRevision, ContextCheckpointRevision: r.contextCheckpointRevision, ToolCallProjectionHiddenThroughGeneration: r.toolCallProjectionHiddenThroughGeneration, ActiveContextStart: 0, LastContextResetID: "", LastHumanInteraction: r.lastHumanInteraction, HookStore: hookStore, NotifySeq: r.notifySeq, AckSeq: r.ackSeq}
		r.mu.Unlock()
		persistErr = r.store.ClearMessagesWithRuntimeState(ctx, r.session.ID, state)
		if persistErr == nil {
			if r.inputBox != nil {
				r.inputBox.MarkPersistenceCommitted(inputState)
			}
			r.mu.Lock()
			r.conversationRevision++
			r.contextCheckpointRevision++
			r.deltaMu.Lock()
			r.recordDeltaUpserts = make(map[string]pendingRecordMutation)
			r.recordDeltaDeletes = make(map[string]uint64)
			// A successful clear discards all identity-allocation failures tied
			// to the old transcript. New input must be allowed to establish fresh
			// identities after this recovery boundary.
			r.historyMutationErr = nil
			r.checkpointDirty = false
			r.checkpointFullReplaceSequence = 0
			r.checkpointSyncedMutationSequence = r.checkpointMutationSequence
			r.checkpointMutations = nil
			r.historyFullReplaceSequence = r.checkpointMutationSequence
			r.historyMutations = nil
			r.deltaMu.Unlock()
			r.historyCommitDirty = false
			r.mu.Unlock()
		}
	}
	r.persistMu.Unlock()
	r.lifecycleMu.Unlock()
	return persistErr
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	if r.lifecycleExecutionBusy() {
		return compression.ForceResult{Status: "busy"}
	}
	r.persistMu.Lock()
	r.mu.Lock()
	previousMessages := append([]llm.Message(nil), r.messages...)
	previousStart := r.activeContextStart
	previousHistoryRevision := r.historyRevision
	r.deltaMu.Lock()
	previousCheckpointDirty := r.checkpointDirty
	previousCheckpointMutationSequence := r.checkpointMutationSequence
	previousCheckpointFullReplaceSequence := r.checkpointFullReplaceSequence
	previousHistoryFullReplaceSequence := r.historyFullReplaceSequence
	r.deltaMu.Unlock()
	start := r.activeContextStart
	if start < 0 || start > len(r.messages) {
		start = len(r.messages)
		r.activeContextStart = start
	}
	active := append([]llm.Message(nil), r.messages[start:]...)
	beforeDigest := turn.Digest(active)
	beforeCount := len(active)
	result := r.compression.ForceBlocking(ctx, r.session.ID, r.agentID, r.hub, &active, prefix)
	afterDigest := turn.Digest(active)
	afterCount := len(active)
	if result.Status == "applied" {
		r.messages = append(append([]llm.Message(nil), r.messages[:start]...), active...)
		r.historyRevision++
		r.noteCheckpointDirtyLocked()
	}
	r.mu.Unlock()
	if result.Status != "applied" {
		r.persistMu.Unlock()
		return result
	}
	var persistErr error
	if r.store != nil {
		persistErr = r.persistLocked(ctx)
	}
	if persistErr != nil {
		r.mu.Lock()
		r.messages = previousMessages
		r.activeContextStart = previousStart
		r.historyRevision = previousHistoryRevision
		r.deltaMu.Lock()
		r.checkpointDirty = previousCheckpointDirty
		r.checkpointMutationSequence = previousCheckpointMutationSequence
		r.checkpointFullReplaceSequence = previousCheckpointFullReplaceSequence
		r.historyFullReplaceSequence = previousHistoryFullReplaceSequence
		r.deltaMu.Unlock()
		r.mu.Unlock()
		r.persistMu.Unlock()
		if r.logger != nil {
			r.logger.Warn("manual context compression checkpoint failed", "session_id", r.session.ID, "error", persistErr)
		}
		result.Status = "persistence_failed"
		return result
	}
	r.persistMu.Unlock()
	if err := r.lifecycleContextCompactedLocked("context_compressed_manual", beforeDigest, afterDigest, beforeCount, afterCount); err != nil && r.logger != nil {
		r.logger.Warn("record manual context compaction lifecycle failed", "session_id", r.session.ID, "error", err)
	}
	// Manual compression has the same semantics as pre-step compression:
	// the next model request must build a fresh context segment.
	r.scheduleModelContextRebuild("context_compression_manual", "next_model_step")
	return result
}

func (r *runtime) enqueue(env queue.Envelope, priority queue.Priority) error {
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	r.mu.Lock()
	stopped := r.executionStopped
	r.mu.Unlock()
	if stopped {
		return fmt.Errorf("result_commit_recovery_required")
	}
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

// appendInput is the only runtime ingress for user/trigger/child-agent data.  It
// assigns the current session epoch before appending so clear-context can
// invalidate an accepted-but-not-yet-consumed input without touching the
// order of newer records.
func (r *runtime) appendInput(kind InputKind, env queue.Envelope) (uint64, error) {
	if r == nil || r.inputBox == nil {
		return 0, fmt.Errorf("input box unavailable")
	}
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	r.mu.Lock()
	if r.executionStopped {
		r.mu.Unlock()
		return 0, fmt.Errorf("result_commit_recovery_required")
	}
	if env.SessionEpoch == 0 {
		env.SessionEpoch = r.sessionEpoch
	}
	currentEpoch := r.sessionEpoch
	r.mu.Unlock()
	if env.SessionEpoch != currentEpoch {
		return 0, nil
	}
	r.persistMu.Lock()
	seq, err := r.inputBox.AppendDeferred(kind, env)
	if err != nil {
		r.persistMu.Unlock()
		return 0, err
	}
	// Persist the accepted tail before making it visible to the consumer. The
	// input remains hidden behind InputBox's deferred marker until this write
	// commits, closing the append→wake race for both idle and busy sessions.
	if r.store != nil {
		err = r.persistLocked(context.Background())
		if err != nil {
			r.inputBox.RollbackDeferred(seq)
			r.persistMu.Unlock()
			return 0, err
		}
	}
	r.inputBox.CommitDeferred(seq)
	r.persistMu.Unlock()
	r.publishInputQueueSnapshot()
	if r.executionGate != nil {
		r.executionGate.notifyInput()
	}
	return seq, nil
}

func (r *runtime) appendInputWithReceipt(kind InputKind, env queue.Envelope, receipt InputReceipt) (InputReceipt, bool, error) {
	if r == nil || r.inputBox == nil {
		return InputReceipt{}, false, fmt.Errorf("input box unavailable")
	}
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	r.mu.Lock()
	if r.executionStopped {
		r.mu.Unlock()
		return InputReceipt{}, false, fmt.Errorf("result_commit_recovery_required")
	}
	currentEpoch := r.sessionEpoch
	r.mu.Unlock()
	if receipt.ConversationEpoch != currentEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	if r.store != nil {
		storedReceipt, found, err := r.store.LookupInputReceipt(context.Background(), r.session.ID, receipt.ClientRequestID)
		if err != nil {
			return InputReceipt{}, false, err
		}
		if found {
			if storedReceipt.ConversationEpoch != receipt.ConversationEpoch {
				return InputReceipt{}, false, ErrInputConversationChanged
			}
			if storedReceipt.PayloadDigest != receipt.PayloadDigest {
				return InputReceipt{}, false, ErrInputRequestConflict
			}
			return r.inputBox.ProjectReceipt(storedReceipt, receipt.ConversationEpoch)
		}
	}
	env.SessionEpoch = currentEpoch
	r.persistMu.Lock()
	receipt, duplicate, err := r.inputBox.AppendDeferredWithReceipt(kind, env, receipt)
	if err != nil || duplicate {
		r.persistMu.Unlock()
		return receipt, duplicate, err
	}
	// The queue row and retry receipt commit together. The deferred entry stays
	// invisible to the consumer until both are durable, so a successful HTTP
	// response can be reconciled after a lost response or process restart.
	if r.store != nil {
		if err := r.persistLockedWithInputReceipt(context.Background(), &receipt); err != nil {
			r.inputBox.RollbackDeferred(receipt.InputSeq)
			r.persistMu.Unlock()
			return InputReceipt{}, false, err
		}
		r.inputBox.ForgetReceipt(receipt.ClientRequestID)
	}
	r.inputBox.CommitDeferred(receipt.InputSeq)
	r.persistMu.Unlock()
	r.publishInputQueueSnapshot()
	if r.executionGate != nil {
		r.executionGate.notifyInput()
	}
	return receipt, false, nil
}

func (r *runtime) appendConversationResubmitWithReceipt(env queue.Envelope, receipt InputReceipt, guard store.ConversationResubmitGuard) (InputReceipt, bool, error) {
	if r == nil || r.inputBox == nil || r.store == nil || env.ConversationResubmit == nil {
		return InputReceipt{}, false, fmt.Errorf("conversation resubmit requires durable history")
	}
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	r.mu.Lock()
	if r.executionStopped {
		r.mu.Unlock()
		return InputReceipt{}, false, fmt.Errorf("result_commit_recovery_required")
	}
	currentEpoch := r.sessionEpoch
	r.mu.Unlock()
	if receipt.ConversationEpoch != currentEpoch {
		return InputReceipt{}, false, ErrInputConversationChanged
	}
	storedReceipt, found, err := r.store.LookupInputReceipt(context.Background(), r.session.ID, receipt.ClientRequestID)
	if err != nil {
		return InputReceipt{}, false, err
	}
	if found {
		if storedReceipt.ConversationEpoch != receipt.ConversationEpoch {
			return InputReceipt{}, false, ErrInputConversationChanged
		}
		if storedReceipt.PayloadDigest != receipt.PayloadDigest {
			return InputReceipt{}, false, ErrInputRequestConflict
		}
		return r.inputBox.ProjectReceipt(storedReceipt, receipt.ConversationEpoch)
	}
	gate := r.executionGate
	if gate != nil {
		gate.mu.Lock()
		defer gate.mu.Unlock()
		if gate.maintenance || gate.dispatches != 0 {
			return InputReceipt{}, false, store.ErrConversationResubmitConflict
		}
	}
	if r.turnState() != "idle" || r.queue.Len() != 0 || r.inputBox.Len() != 0 || r.inputBox.HasInFlight() || r.hasPendingHITL() {
		return InputReceipt{}, false, store.ErrConversationResubmitConflict
	}
	env.SessionEpoch = currentEpoch
	wasPaused, controlRevision := r.inputBox.QueueControlState()
	if wasPaused {
		if _, _, err := r.inputBox.SetQueuePaused(false, controlRevision); err != nil {
			return InputReceipt{}, false, err
		}
	}
	r.persistMu.Lock()
	accepted, duplicate, err := r.inputBox.AppendDeferredWithReceipt(InputKindUser, env, receipt)
	if err != nil || duplicate {
		r.persistMu.Unlock()
		if wasPaused {
			r.inputBox.RestoreQueueControl(wasPaused, controlRevision)
		}
		return accepted, duplicate, err
	}
	if err := r.persistLockedWithInputReceiptAndResubmit(context.Background(), &accepted, &guard); err != nil {
		r.inputBox.RollbackDeferred(accepted.InputSeq)
		if wasPaused {
			r.inputBox.RestoreQueueControl(wasPaused, controlRevision)
		}
		r.persistMu.Unlock()
		return InputReceipt{}, false, err
	}
	r.inputBox.ForgetReceipt(accepted.ClientRequestID)
	r.inputBox.CommitDeferred(accepted.InputSeq)
	r.persistMu.Unlock()
	if gate != nil {
		gate.notify()
		if gate.maintenanceCancel != nil {
			gate.maintenanceCancel()
		}
	}
	r.signalInputBox()
	return accepted, false, nil
}

func (r *runtime) editQueuedInput(seq, expectedRevision uint64, env queue.Envelope) (InputRecord, error) {
	return r.mutateQueuedInput(seq, expectedRevision, &env)
}

func (r *runtime) deleteQueuedInput(seq, expectedRevision uint64) (InputRecord, error) {
	return r.mutateQueuedInput(seq, expectedRevision, nil)
}

// mutateQueuedInput serializes edits/deletes with ingress and runtime snapshot
// writes. InputBox pauses claims while the tentative mutation is persisted;
// persistence failure restores the exact queue order before claims resume.
func (r *runtime) mutateQueuedInput(seq, expectedRevision uint64, replacement *queue.Envelope) (InputRecord, error) {
	if r == nil || r.inputBox == nil {
		return InputRecord{}, fmt.Errorf("input box unavailable")
	}
	r.ingressMu.Lock()
	defer r.ingressMu.Unlock()
	r.mu.Lock()
	if r.executionStopped {
		r.mu.Unlock()
		return InputRecord{}, fmt.Errorf("result_commit_recovery_required")
	}
	r.mu.Unlock()

	r.persistMu.Lock()
	var record InputRecord
	var mutation *inputBoxMutation
	var err error
	if replacement != nil {
		record, mutation, err = r.inputBox.BeginQueuedEdit(seq, expectedRevision, *replacement)
	} else {
		record, mutation, err = r.inputBox.BeginQueuedDelete(seq, expectedRevision)
	}
	if err != nil {
		r.persistMu.Unlock()
		return InputRecord{}, err
	}
	if r.store != nil {
		if err := r.persistLocked(context.Background()); err != nil {
			r.inputBox.RollbackMutation(mutation)
			r.persistMu.Unlock()
			return InputRecord{}, err
		}
	}
	r.inputBox.CommitMutation(mutation)
	r.persistMu.Unlock()
	r.publishInputQueueSnapshot()
	if r.executionGate != nil {
		r.executionGate.notifyInput()
	}
	return record, nil
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
	lifecycleWasActive := false
	turnID := ""
	if r.turnCoordinator != nil {
		state := r.turnCoordinator.Snapshot()
		lifecycleWasActive = state.HasActiveTurn
		turnID = state.TurnID
	}
	stateBefore := r.turnState()
	pending := r.pendingSnapshot()
	if err := r.lifecycleCancel(); err != nil && r.logger != nil {
		r.logger.Warn("cancel lifecycle failed", "session_id", r.session.ID, "error", err)
	}
	if lifecycleWasActive {
		if err := r.markDreamingCancelled(r.turnCoordinator.Snapshot().TurnID); err != nil && r.logger != nil {
			r.logger.Warn("persist dreaming cancellation failed", "session_id", r.session.ID, "error", err)
		}
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
	if lifecycleActive {
		r.orch.RecordTurnCancellationContext(r.session.ID, &r.messages, turnID)
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
	if historyChanged {
		r.historyRevision++
	}
	r.mu.Unlock()
	if changed || historyChanged {
		r.logger.Info("cancelled turn",
			"session_id", r.session.ID,
		)
		r.persist(context.Background())
	}
	if pending == nil {
		r.maybeScheduleContinueAfterCancel()
	}
	return changed || historyChanged
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
	r.mu.Lock()
	pipeline := r.candidatePipeline
	r.candidatePipeline = nil
	service := r.memoryService
	r.memoryService = nil
	r.mu.Unlock()
	if pipeline != nil {
		_ = pipeline.Close()
	}
	if closer, ok := service.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func (r *runtime) stop() {
	r.requestStop()
	r.waitStopped()
	if r.orch != nil {
		r.orch.ForgetSession(r.session.ID)
	}
}
