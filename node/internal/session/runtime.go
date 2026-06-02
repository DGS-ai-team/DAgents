package session

import (
	"context"
	"log/slog"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/history"
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
	queue   *queue.MessageQueue
	orch    *turn.Orchestrator
	store   *store.SQLiteStore
	hub     *stream.Hub
	agentID string

	skillsCatalog *skills.Catalog
	compression   *compression.Coordinator

	mu            sync.Mutex
	state         turn.State
	turnCancel    context.CancelFunc
	messages      []llm.Message
	loadedSkills  []skills.LoadedSkill
	pending       *turn.PendingHITL
	toolLoopCount int
	fsRoot        string

	triggerDelivery triggers.DeliveryTracker

	childMeta *childRuntimeMeta
}

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
		skillsCatalog: catalog,
		compression:   compression.NewCoordinator(llmClient, turnOpts.CompressionSilent, turnOpts.CompressionBlocking),
		state:         turn.StateIdle,
		messages:      append([]llm.Message(nil), initial...),
		loadedSkills:  append([]skills.LoadedSkill(nil), loaded...),
		pending:       initialPending,
		toolLoopCount: initialLoopCount,
		fsRoot:            turnOpts.FSRoot,
		triggerDelivery:   triggerDelivery,
	}
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
		logger,
	)
	rt.orch.SetToolResultEnqueuer(rt.enqueueToolResult)
	return rt
}

func (r *runtime) getLoadedSkills() []skills.LoadedSkill {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]skills.LoadedSkill(nil), r.loadedSkills...)
}

func (r *runtime) setLoadedSkills(items []skills.LoadedSkill) {
	r.mu.Lock()
	r.loadedSkills = append([]skills.LoadedSkill(nil), items...)
	r.mu.Unlock()
}

func (r *runtime) setTriggerDelivery(tracker triggers.DeliveryTracker) {
	r.triggerDelivery = tracker
}

func (r *runtime) start(parent context.Context) {
	go r.consumeLoop(parent)
}

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
			r.handleResume(ctx, env.ResumeValue)
		case queue.RequestTypeAsyncToolResult:
			r.handleAsyncToolResult(ctx, env.AsyncToolResult)
		case queue.RequestTypeToolResult:
			r.handleToolResult(ctx)
		case queue.RequestTypeMessage, "":
			r.handleHumanMessage(ctx, env.Content)
		default:
		}
	}
}

func (r *runtime) enqueueToolResult(_ string) error {
	return r.enqueue(queue.Envelope{RequestType: queue.RequestTypeToolResult}, queue.PriorityToolResult)
}

func (r *runtime) scheduleToolResult() error {
	return r.enqueueToolResult(r.session.ID)
}

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

func (r *runtime) handleHumanMessage(parent context.Context, content string) {
	r.mu.Lock()
	if r.pending != nil {
		pending := r.pending
		r.pending = nil
		r.orch.InterruptPending(r.session.ID, &r.messages, pending)
	}
	r.toolLoopCount = 0
	if r.compression != nil && r.compression.Enabled() && !r.isChildSession() {
		r.compression.MaybeHandle(parent, r.session.ID, r.agentID, r.hub, &r.messages)
	}
	turnCtx, cancel := context.WithCancel(parent)
	r.turnCancel = cancel
	r.state = turn.StateModelStreaming
	history := r.messages
	r.mu.Unlock()

	var scheduleToolResult bool
	defer func() {
		r.mu.Lock()
		r.state = turn.StateIdle
		r.turnCancel = nil
		r.mu.Unlock()
		if !scheduleToolResult {
			r.tryCompleteChildIfIdle()
		}
	}()

	setState := func(s turn.State) {
		r.mu.Lock()
		r.state = s
		r.mu.Unlock()
	}

	outcome := r.orch.RunHumanMessageTurn(turnCtx, r.session.ID, &history, content, setState)
	if outcome.Err != nil {
		r.mu.Lock()
		r.messages = history
		r.mu.Unlock()
		r.persist(context.Background())
		return
	}
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	scheduleToolResult = outcome.ScheduleToolResult
	if outcome.ScheduleToolResult {
		_ = r.scheduleToolResult()
	}
	r.persist(context.Background())
}

func (r *runtime) afterToolStep(outcome turn.StepOutcome) {
	if outcome.ScheduleToolResult {
		_ = r.scheduleToolResult()
	}
	r.persist(context.Background())
}

func (r *runtime) handleToolResult(parent context.Context) {
	r.mu.Lock()
	if r.compression != nil && r.compression.Enabled() && !r.isChildSession() {
		r.compression.MaybeHandle(parent, r.session.ID, r.agentID, r.hub, &r.messages)
	}
	turnCtx, cancel := context.WithCancel(parent)
	r.turnCancel = cancel
	r.state = turn.StateModelStreaming
	history := r.messages
	loopCount := r.toolLoopCount
	r.mu.Unlock()

	var scheduleToolResult bool
	defer func() {
		r.mu.Lock()
		r.state = turn.StateIdle
		r.turnCancel = nil
		r.mu.Unlock()
		if !scheduleToolResult {
			r.tryCompleteChildIfIdle()
		}
	}()

	setState := func(s turn.State) {
		r.mu.Lock()
		r.state = s
		r.mu.Unlock()
	}

	outcome := r.orch.RunToolMessageTurn(turnCtx, r.session.ID, &history, setState, loopCount)
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	scheduleToolResult = outcome.ScheduleToolResult
	r.afterToolStep(outcome)
}

func (r *runtime) handleAsyncToolResult(parent context.Context, payload *queue.AsyncToolResultPayload) {
	if payload == nil {
		return
	}
	r.mu.Lock()
	if r.compression != nil && r.compression.Enabled() && !r.isChildSession() {
		r.compression.MaybeHandle(parent, r.session.ID, r.agentID, r.hub, &r.messages)
	}
	turnCtx, cancel := context.WithCancel(parent)
	r.turnCancel = cancel
	r.state = turn.StateModelStreaming
	history := r.messages
	loopCount := r.toolLoopCount
	r.mu.Unlock()

	var scheduleToolResult bool
	defer func() {
		r.mu.Lock()
		r.state = turn.StateIdle
		r.turnCancel = nil
		r.mu.Unlock()
		if !scheduleToolResult {
			r.tryCompleteChildIfIdle()
		}
	}()

	setState := func(s turn.State) {
		r.mu.Lock()
		r.state = s
		r.mu.Unlock()
	}

	outcome := r.orch.HandleAsyncToolResult(turnCtx, r.session.ID, &history, turn.AsyncToolResultInput{
		JobID:      payload.JobID,
		ToolName:   payload.ToolName,
		ToolCallID: payload.ToolCallID,
		Status:     payload.Status,
		ResultText: payload.ResultText,
		ErrorText:  payload.ErrorText,
	}, setState, loopCount)
	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	scheduleToolResult = outcome.ScheduleToolResult
	r.afterToolStep(outcome)
}

func (r *runtime) handleMessage(parent context.Context, content string) {
	// 兼容旧调用路径；与 handleHumanMessage 等价。
	r.handleHumanMessage(parent, content)
}

func (r *runtime) handleResume(parent context.Context, resumeValue map[string]any) {
	r.mu.Lock()
	pending := r.pending
	if pending == nil {
		r.mu.Unlock()
		return
	}
	turnCtx, cancel := context.WithCancel(parent)
	r.turnCancel = cancel
	r.state = turn.StateAwaitingTool
	history := r.messages
	loopCount := r.toolLoopCount
	r.mu.Unlock()

	var scheduleToolResult bool
	defer func() {
		r.mu.Lock()
		r.state = turn.StateIdle
		r.turnCancel = nil
		r.mu.Unlock()
		if !scheduleToolResult {
			r.tryCompleteChildIfIdle()
		}
	}()

	setState := func(s turn.State) {
		r.mu.Lock()
		r.state = s
		r.mu.Unlock()
	}

	outcome := r.orch.ContinueAfterResume(turnCtx, r.session.ID, &history, resumeValue, pending, setState, loopCount)

	r.mu.Lock()
	r.applyStepOutcome(&history, outcome)
	r.mu.Unlock()
	scheduleToolResult = outcome.ScheduleToolResult
	r.afterToolStep(outcome)
}

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

func (r *runtime) contextView() *ContextView {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	return view
}

func (r *runtime) enqueue(env queue.Envelope, priority queue.Priority) error {
	return r.queue.Enqueue(env, priority)
}

func (r *runtime) cancelTurn() bool {
	r.mu.Lock()
	cancel := r.turnCancel
	state := r.state
	r.mu.Unlock()
	if cancel == nil || state == turn.StateIdle {
		return false
	}
	cancel()
	return true
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

func (r *runtime) clearLoadedSkills() {
	r.setLoadedSkills(nil)
	r.persist(context.Background())
}
