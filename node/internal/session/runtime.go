package session

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

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
	// 存储
	store *store.SQLiteStore
	// 事件中心
	hub *stream.Hub
	// 代理 ID
	agentID string
	// 日志
	logger *slog.Logger

	// 技能目录
	skillsCatalog *skills.Catalog
	// 上下文压缩逻辑
	compression *compression.Coordinator

	mu            sync.Mutex           // 互斥锁
	state         turn.State           // 状态
	turnCancel    context.CancelFunc   // 取消 turn 上下文
	messages      []llm.Message        // 交互消息列表
	loadedSkills  []skills.LoadedSkill // 加载的技能列表
	pending       *turn.PendingHITL    // 暂停
	toolLoopCount int                  // tool 循环计数
	fsRoot        string               // 文件系统根路径
	media         *media.Registry      // session 媒体索引（F-M1）

	triggerDelivery triggers.DeliveryTracker // trigger 消息投递跟踪器

	sideEffects *sideEffectStore // 旁路回灌缓冲（子 session 跳过）

	childMeta *childRuntimeMeta // 子 Agent 元数据

	idleAutoCompressApplied bool // 无动作自动压缩已完成；新对话时清除

	notifySeq int // F-E13：最后需 Client 关注的 SSE seq
	ackSeq    int // F-E13：Client 已确认看到的最大 SSE seq

	configRevision int64 // Agent 配置版本（UpdatedAt UnixNano）
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
		session:       Session{ID: id, AgentID: agentID},
		queue:         queue.NewMessageQueue(),
		store:         st,
		hub:           eventHub,
		agentID:       agentID,
		logger:        logger,
		skillsCatalog: catalog,
		compression: func() *compression.Coordinator {
			coord := compression.NewCoordinator(llmClient, turnOpts.CompressionSilent, turnOpts.CompressionBlocking)
			coord.SetLogger(logger)
			coord.SetRawMessageHistoryEnabled(turnOpts.RawMessageHistoryEnabled)
			return coord
		}(),
		state:           turn.StateIdle,
		messages:        append([]llm.Message(nil), initial...),
		loadedSkills:    append([]skills.LoadedSkill(nil), loaded...),
		pending:         initialPending,
		toolLoopCount:   initialLoopCount,
		fsRoot:          turnOpts.FSRoot,
		triggerDelivery: triggerDelivery,
		sideEffects:     newSideEffectStore(),
		idleAutoCompressApplied: idleAutoCompressApplied,
		notifySeq:               initialNotifySeq,
		ackSeq:                  initialAckSeq,
		configRevision:          turnOpts.ConfigRevision,
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
	promptReader.SetFilter(promptcontext.Filter{
		SoulEnabled:     turnOpts.PromptContext.SoulEnabled,
		UserEnabled:     turnOpts.PromptContext.UserEnabled,
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
	rt.orch.SetMultimodalEnabled(turnOpts.MultimodalEnabled)
	rt.orch.SetMediaRegistry(rt.media)
	if len(initialHookStore) > 0 {
		rt.orch.SetHookStore(initialHookStore)
	}
	// 设置工具结果入队器
	rt.orch.SetToolResultEnqueuer(rt.enqueueToolResult)
	if turnOpts.LongTermStore != nil {
		rt.orch.SetLongTermStore(turnOpts.LongTermStore)
	}
	rt.orch.SyncLoadedSkillHooks(loaded)
	// 返回 runtime
	return rt
}

// setPolicy 热更新 orchestrator 策略。
func (r *runtime) setPolicy(engine *policy.Engine) {
	r.orch.SetPolicy(engine)
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
	go r.consumeLoop(parent)
}

// consumeLoop 消费消息循环
func (r *runtime) consumeLoop(ctx context.Context) {
	for {
		env, err := r.queue.Dequeue(ctx)
		if err != nil {
			return
		}
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
}

// enqueueToolResult 将 tool 结果入队
func (r *runtime) enqueueToolResult(_ string) error {
	return r.enqueue(queue.Envelope{RequestType: queue.RequestTypeToolResult}, queue.PriorityToolResult)
}

// scheduleToolResult 调度 tool 结果入队
func (r *runtime) scheduleToolResult() error {
	return r.enqueueToolResult(r.session.ID)
}

// applyStepOutcome 应用步骤结果
func (r *runtime) applyStepOutcome(history *[]llm.Message, outcome turn.StepOutcome) {
	r.messages = append([]llm.Message(nil), (*history)...)
	if outcome.Pending != nil {
		r.pending = outcome.Pending
		r.toolLoopCount = outcome.LoopCount
		return
	}
	r.pending = nil
	if outcome.ScheduleToolResult {
		r.toolLoopCount = outcome.LoopCount
	} else {
		r.toolLoopCount = 0
	}
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
	r.mu.Lock()
	if r.pending != nil {
		pending := r.pending
		r.pending = nil
		r.orch.InterruptPending(r.session.ID, &r.messages, pending)
	}
	if r.orch.RepairUnrespondedToolCalls(r.session.ID, &r.messages) {
		r.logger.Info("repaired orphan tool_calls before user message",
			"session_id", r.session.ID,
		)
	}
	r.toolLoopCount = 0
	firstInteraction := len(r.messages) == 0
	r.mu.Unlock()

	if firstInteraction && r.orch != nil {
		r.orch.ReloadLongTermMemory(parent)
	}

	outcome, history := r.runTurnStepWithSideEffects(parent, turn.StateModelStreaming, true, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(ctx, r.session.ID, history, userMsg, setState)
	})
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
		_ = r.scheduleToolResult()
	} else {
		r.finishTurnIdle(outcome)
	}
	r.persist(context.Background())
}

func (r *runtime) afterToolStep(outcome turn.StepOutcome) {
	if outcome.ScheduleToolResult {
		_ = r.scheduleToolResult()
	} else {
		r.finishTurnIdle(outcome)
	}
	r.persist(context.Background())
}

func (r *runtime) handleToolResult(parent context.Context) {
	loopCount := r.toolLoopCountSnapshot()
	outcome, history := r.runTurnStepWithSideEffects(parent, turn.StateModelStreaming, true, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		return r.orch.RunToolMessageTurn(ctx, r.session.ID, history, setState, loopCount)
	})
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	r.afterToolStep(outcome)
}

func (r *runtime) handleResume(parent context.Context, resumeValue map[string]any) {
	r.mu.Lock()
	pending := r.pending
	if pending == nil {
		r.mu.Unlock()
		r.logger.Info("resume ignored no pending hitl",
			"session_id", r.session.ID,
			"resume_value", resumeValue,
		)
		return
	}
	pendingKind, pendingToolCallID := pendingHITLLogFields(pending)
	loopCount := r.toolLoopCount
	r.mu.Unlock()

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

	outcome, history := r.runTurnStepWithSideEffects(parent, turn.StateAwaitingTool, false, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		return r.orch.ContinueAfterResume(ctx, r.session.ID, history, resumeValue, pending, setState, loopCount)
	})
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
	pending := r.pending
	loopCount := r.toolLoopCount
	idleMarked := r.idleAutoCompressApplied
	notifySeq := r.notifySeq
	ackSeq := r.ackSeq
	r.mu.Unlock()
	_ = r.store.Save(ctx, store.Record{
		AgentID:      r.session.ID,
		NodeID:       r.session.AgentID,
		Messages:     msgs,
		LoadedSkills: loaded,
		RuntimeState: store.RuntimeState{
			Pending:                 pending,
			ToolLoopCount:           loopCount,
			HookStore:               hooks.CloneSessionStore(r.orch.HookStoreSnapshot()),
			IdleAutoCompressApplied: idleMarked,
			NotifySeq:               notifySeq,
			AckSeq:                  ackSeq,
		},
	})
}

func (r *runtime) clearMessages(ctx context.Context) {
	if r.compression != nil {
		r.compression.CancelSession(r.session.ID)
	}
	if r.sideEffectsEnabled() {
		r.sideEffects.ClearSession(r.session.ID, r.orch, r.triggerDelivery)
	}
	r.mu.Lock()
	r.messages = nil
	r.loadedSkills = nil
	r.pending = nil
	r.toolLoopCount = 0
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
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state
}

func (r *runtime) hasPendingHITL() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pending != nil
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
		Tools:        r.orch.ToolDefinitions(),
	}
}

func (r *runtime) compressContext(ctx context.Context) compression.ForceResult {
	if r.isChildSession() {
		return compression.ForceResult{Status: "unsupported"}
	}
	r.mu.Lock()
	busy := r.state != turn.StateIdle || r.pending != nil
	r.mu.Unlock()
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
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	view := &ContextView{
		SessionID:             r.session.ID,
		MessagesCount:         len(r.messages),
		MessagesTotalTokens:   estimateMessageTokens(r.messages),
		PendingToolCallsCount: pendingToolCallsCount(r.pending),
		ToolLoopCount:         r.toolLoopCount,
		LoadedSkills:          loaded,
		QueuePending:          r.queue.Len(),
		TurnState:             r.state,
		Messages:              msgs,
	}
	view.HasActiveTurn = r.state != turn.StateIdle || r.pending != nil
	if view.LoadedSkills == nil {
		view.LoadedSkills = []skills.LoadedSkill{}
	}
	r.mu.Unlock()
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
	return r.queue.Enqueue(env, priority)
}

type resumeDiagSnapshot struct {
	queueLen          int
	resumeInQueue     int
	turnState         turn.State
	pendingKind       string
	pendingToolCallID string
}

func (r *runtime) resumeDiagSnapshot() resumeDiagSnapshot {
	r.mu.Lock()
	pendingKind, pendingID := pendingHITLLogFields(r.pending)
	state := r.state
	r.mu.Unlock()
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
	r.mu.Lock()
	cancel := r.turnCancel
	state := r.state
	if cancel != nil && state != turn.StateIdle {
		r.mu.Unlock()
		cancel()
		r.maybeScheduleContinueAfterCancel()
		return true
	}
	repaired := r.orch.RepairUnrespondedToolCalls(r.session.ID, &r.messages)
	if repaired {
		r.pending = nil
	}
	r.mu.Unlock()
	if repaired {
		r.logger.Info("repaired orphan tool_calls on idle cancel",
			"session_id", r.session.ID,
		)
		r.persist(context.Background())
	}
	r.maybeScheduleContinueAfterCancel()
	return false
}

func (r *runtime) stop() {
	if r.compression != nil {
		r.compression.CancelSession(r.session.ID)
	}
	if r.sideEffectsEnabled() {
		r.sideEffects.ClearSession(r.session.ID, r.orch, r.triggerDelivery)
	}
	r.queue.Close()
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
