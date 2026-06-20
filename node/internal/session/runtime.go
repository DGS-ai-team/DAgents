package session

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/history"
	clihitl "github.com/DGS-ai-team/DAgents/node/internal/hitl"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
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

	triggerDelivery triggers.DeliveryTracker // trigger 消息投递跟踪器

	childMeta *childRuntimeMeta // 子 Agent 元数据
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
	turnOpts TurnOptions,
	triggerDelivery triggers.DeliveryTracker,
) *runtime {
	return newRuntimeWithPublisher(id, agentID, hub, hub, llmClient, registry, policyEngine, st, logger,
		initial, loaded, initialPending, initialLoopCount, turnOpts, triggerDelivery)
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
	turnOpts TurnOptions,
	triggerDelivery triggers.DeliveryTracker,
) *runtime {
	catalog := skills.NewCatalog(turnOpts.SkillsRoot, turnOpts.SkillsEnabled, turnOpts.SkillsMaxInPrompt)
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
			return coord
		}(),
		state:           turn.StateIdle,
		messages:        append([]llm.Message(nil), initial...),
		loadedSkills:    append([]skills.LoadedSkill(nil), loaded...),
		pending:         initialPending,
		toolLoopCount:   initialLoopCount,
		fsRoot:          turnOpts.FSRoot,
		triggerDelivery: triggerDelivery,
	}
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
		promptcontext.NewReader(turnOpts.RuntimeDir),
		journal,
		hooks.RuntimeConfig{
			Duplicate: hooks.DuplicateConfigOrDefault(turnOpts.DuplicateToolCall),
			ToolResult: hooks.ToolResultConfigOrDefault(hooks.ToolResultConfig{
				Enabled:              turnOpts.ToolResult.Enabled,
				SpillThresholdTokens: turnOpts.ToolResult.SpillThresholdTokens,
				Tools:                turnOpts.ToolResult.Tools,
				FSRoot:               turnOpts.FSRoot,
			}),
			External: turnOpts.ExternalHooks,
			ExternalDeps: hooks.ExternalDeps{
				Logger: logger,
			},
		},
		logger,
	)
	// 设置工具结果入队器
	rt.orch.SetToolResultEnqueuer(rt.enqueueToolResult)
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
		// trigger 消息出队即视为已消费，允许同一 trigger 再次 fire。
		if env.TriggerID != "" && r.triggerDelivery != nil {
			r.triggerDelivery.ClearPendingDelivery(env.TriggerID)
		}
		switch env.RequestType {
		case queue.RequestTypeResume:
			r.logResumeDequeued(env.ResumeValue)
			r.handleResume(ctx, env.ResumeValue)
		case queue.RequestTypeAsyncToolResult:
			r.handleAsyncToolResult(ctx, env.AsyncToolResult)
		case queue.RequestTypeToolResult:
			r.handleToolResult(ctx)
		case queue.RequestTypeMessage, "":
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
	content := env.Content
	userName := llm.NormalizeUserMessageName(env.UserName)
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
	r.mu.Unlock()

	outcome, history := r.runTurnStep(parent, turn.StateModelStreaming, true, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		return r.orch.RunHumanMessageTurn(ctx, r.session.ID, history, content, userName, setState)
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
	outcome, history := r.runTurnStep(parent, turn.StateModelStreaming, true, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		return r.orch.RunToolMessageTurn(ctx, r.session.ID, history, setState, loopCount)
	})
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	r.afterToolStep(outcome)
}

func (r *runtime) handleAsyncToolResult(parent context.Context, payload *queue.AsyncToolResultPayload) {
	if payload == nil {
		return
	}
	r.mu.Lock()
	savedPending := r.pending
	savedLoopCount := r.toolLoopCount
	r.mu.Unlock()

	loopCount := r.toolLoopCountSnapshot()
	outcome, history := r.runTurnStep(parent, turn.StateModelStreaming, true, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
		return r.orch.HandleAsyncToolResult(ctx, r.session.ID, history, turn.AsyncToolResultInput{
			JobID:                  payload.JobID,
			ToolName:               payload.ToolName,
			ToolCallID:             payload.ToolCallID,
			Status:                 payload.Status,
			ResultText:             payload.ResultText,
			ErrorText:              payload.ErrorText,
			OutputCompressSavedPct: payload.OutputCompressSavedPct,
			OutputCompressRawRunes: payload.OutputCompressRawRunes,
			OutputCompressOutRunes: payload.OutputCompressOutRunes,
		}, setState, loopCount)
	})
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	if savedPending != nil && outcome.Pending == nil {
		r.pending = savedPending
		r.toolLoopCount = savedLoopCount
		r.logger.Info("async_tool_result preserved pending hitl",
			"session_id", r.session.ID,
			"job_id", payload.JobID,
			"tool_name", payload.ToolName,
		)
	}
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
	if resumeKind != "nil" && resumeKind != "unknown" && pendingKind != "" &&
		!resumeKindMatchesPending(pendingKind, resumeKind) {
		r.logger.Warn("resume kind mismatch (diagnostic only, still processing)",
			"session_id", r.session.ID,
			"pending_kind", pendingKind,
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

	outcome, history := r.runTurnStep(parent, turn.StateAwaitingTool, false, func(ctx context.Context, history *[]llm.Message, setState turn.StateSetter) turn.StepOutcome {
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
	r.mu.Unlock()
	_ = r.store.Save(ctx, store.Record{
		SessionID:    r.session.ID,
		AgentID:      r.session.AgentID,
		Messages:     msgs,
		LoadedSkills: loaded,
		RuntimeState: store.RuntimeState{
			Pending:       pending,
			ToolLoopCount: loopCount,
		},
	})
}

func (r *runtime) clearMessages(ctx context.Context) {
	if r.compression != nil {
		r.compression.CancelSession(r.session.ID)
	}
	r.mu.Lock()
	r.messages = nil
	r.loadedSkills = nil
	r.pending = nil
	r.toolLoopCount = 0
	r.mu.Unlock()
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

// pendingHITLLogFields 提取 pending HITL 日志字段（kind 与首个 tool_call_id）。
func pendingHITLLogFields(pending *turn.PendingHITL) (kind string, toolCallID string) {
	if pending == nil {
		return "", ""
	}
	kind = string(pending.Kind)
	if pending.Kind == turn.HITLUserInformation && pending.UserInfo != nil {
		return kind, pending.UserInfo.ID
	}
	if pending.Kind == turn.HITLApproval && len(pending.ToolCalls) > 0 {
		return kind, pending.ToolCalls[0].ID
	}
	return kind, ""
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

func resumeKindMatchesPending(pendingKind, resumeKind string) bool {
	switch pendingKind {
	case "approval":
		return resumeKind == "approval"
	case "user_information":
		return resumeKind == "user_information"
	default:
		return false
	}
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
	return false
}

func (r *runtime) stop() {
	if r.compression != nil {
		r.compression.CancelSession(r.session.ID)
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
