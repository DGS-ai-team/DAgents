// Package session 管理 session 表、per-session 队列与 turn 消费循环。
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
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/compression"
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

// TurnOptions 为 session turn 编排配置（system prompt、skills、压缩等）。
type TurnOptions struct {
	FSRoot       string
	MaxToolLoops int
	// MaxModelRetries retries only transient provider failures within one Step;
	// zero uses the default (2), -1 disables retries. Partial streamed output is
	// never retried by the orchestrator.
	MaxModelRetries int
	// Budget provides hard lifecycle limits. Zero values mean unlimited.
	Budget turn.TurnBudget
	// ToolRetryLimit bounds automatic retries for explicitly retry-safe tools;
	// zero uses one retry, and negative disables automatic retries.
	ToolRetryLimit    int
	SkillsRoot        string
	SkillsEnabled     bool
	SkillsMaxInPrompt int
	// SkillsVisibleRestrict 为 true 时仅暴露 SkillsVisible 中的 skill（空切片=不可见）。
	SkillsVisibleRestrict       bool
	SkillsVisible               []string
	RuntimeDir                  string
	CompressionSilent           int
	CompressionBlocking         int
	IdleAutoCompressSeconds     int
	IdleAutoCompressPollSeconds int
	IdleAutoCompressMinTokens   int
	RawMessageHistoryEnabled    bool
	RawMessageHistoryDir        string
	DuplicateToolCall           hooks.DuplicateConfig
	ToolResult                  hooks.ToolResultConfig
	InjectTodayDate             hooks.InjectTodayDateConfig
	PluginHooks                 hooks.PluginsConfig
	HookHost                    turn.HookHostConfig
	MultimodalEnabled           bool
	// ConfigRevision 保留兼容旧调用方；新代码使用 RuntimeRevision。
	ConfigRevision int64
	// RuntimeRevision 是独立于 agents.updated_at 的 Agent runtime 版本。
	RuntimeRevision int64
	// RuntimeDigest 标识该 runtime 的模型可见输入（prompt + tools）。
	RuntimeDigest string
	// PromptContext 控制 soul/custom/long_term 侧车是否注入（缺省全开）。
	PromptContext PromptContextOptions
	// PromptContent 为侧车正文（来自 agents.db，经 Content 注入 runtime）。
	PromptContent *promptcontext.Content
	// PreferredName 为本机使用者称呼（Node 首配）；注入 system prompt，替代 user.md。
	PreferredName string
	// LongTermStore 持久化长期记忆（remember 工具写入 SQLite）。
	LongTermStore turn.LongTermStore
}

// PromptContextOptions 为侧车 / 长期记忆注入开关。
type PromptContextOptions struct {
	SoulEnabled     *bool
	UserEnabled     *bool // deprecated：用户信息改走 PreferredName，保留字段兼容旧快照
	CustomEnabled   *bool
	LongTermEnabled *bool
	LongTermScope   *string // global | agent
}

// Manager 维护 session 表；每个 session 独立队列与 consumer goroutine。
type Manager struct {
	agentID string
	hub     *stream.Hub
	llm     llm.Client
	tools   *tools.Registry
	policy  *policy.Engine
	store   *store.SQLiteStore
	turn    TurnOptions

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.RWMutex
	sessions map[string]*runtime
	logger   *slog.Logger

	mediaOnlyMu sync.Mutex
	mediaOnly   map[string]*media.Registry

	triggerDelivery triggers.DeliveryTracker

	children *childagent.Manager

	// OnReleased 在 session 成功卸出内存后回调（用于回收 docker 沙箱等）。
	OnReleased func(sessionID string)
}

// NewManager 绑定 agent、SSE Hub、LLM、工具、策略与持久化 store。
func NewManager(
	agentID string,
	hub *stream.Hub,
	llmClient llm.Client,
	registry *tools.Registry,
	policyEngine *policy.Engine,
	st *store.SQLiteStore,
	turnOpts TurnOptions,
	logger *slog.Logger,
) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	if turnOpts.MaxToolLoops <= 0 {
		turnOpts.MaxToolLoops = turn.DefaultMaxToolLoops()
	}
	if turnOpts.MaxModelRetries == 0 {
		turnOpts.MaxModelRetries = 2
	}
	return &Manager{
		agentID:  agentID,
		hub:      hub,
		llm:      llmClient,
		tools:    registry,
		policy:   policyEngine,
		store:    st,
		turn:     turnOpts,
		ctx:      ctx,
		cancel:   cancel,
		sessions: make(map[string]*runtime),
		logger:   logx.OrDefault(logger),
	}
}

// SetMultimodalEnabled 仅更新 Manager 默认 TurnOptions 与默认 Registry。
// 不广播到已装入的 Agent runtime（多模态随 Agent 绑定的 LLM 在 ensure/reload 时生效）。
func (m *Manager) SetMultimodalEnabled(enabled bool) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turn.MultimodalEnabled = enabled
	if m.tools != nil {
		m.tools.SetMultimodalEnabled(enabled)
	}
}

// SetTriggerDeliveryTracker 注入 trigger 待消费跟踪器；对已存在 session 同步生效。
func (m *Manager) SetTriggerDeliveryTracker(tracker triggers.DeliveryTracker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggerDelivery = tracker
	for _, rt := range m.sessions {
		rt.setTriggerDelivery(tracker)
	}
}

// Stop 停止全部 session consumer（进程退出时调用）。
func (m *Manager) Stop() {
	m.logger.Info("session manager stopping")
	m.cancel()
	m.mu.Lock()
	runtimes := make([]*runtime, 0, len(m.sessions))
	for _, rt := range m.sessions {
		runtimes = append(runtimes, rt)
	}
	m.mu.Unlock()
	for _, rt := range runtimes {
		rt.requestStop()
	}
	for _, rt := range runtimes {
		rt.waitStopped()
	}
}

// DefaultTurnOptions 返回 Manager 启动时的默认 TurnOptions 副本。
func (m *Manager) DefaultTurnOptions() TurnOptions {
	if m == nil {
		return TurnOptions{}
	}
	return m.turn
}

// DefaultTools 返回 Manager 共享的默认 Registry。
func (m *Manager) DefaultTools() *tools.Registry {
	if m == nil {
		return nil
	}
	return m.tools
}

// SessionTools 返回指定 session 的 *tools.Registry（per-agent 沙箱）；不存在则 nil。
func (m *Manager) SessionTools(sessionID string) *tools.Registry {
	if m == nil {
		return nil
	}
	rt := m.getRuntime(sessionID)
	if rt == nil || rt.orch == nil {
		return nil
	}
	return rt.orch.ToolRegistry()
}

// CreateWithOptions 用指定 TurnOptions、工具执行器与可选策略引擎创建/复用 session（per-agent 沙箱用）。
// toolExec 为 nil 时回退到 Manager 默认 Registry；policyEngine 为 nil 时回退到 Manager 默认策略。
func (m *Manager) CreateWithOptions(requestedID string, turnOpts TurnOptions, toolExec tools.Executor, policyEngine *policy.Engine) (*Session, bool, error) {
	id := strings.TrimSpace(requestedID)
	if id == "" {
		return nil, false, fmt.Errorf("session id is required for CreateWithOptions")
	}
	if toolExec == nil {
		toolExec = m.tools
	}
	if policyEngine == nil {
		policyEngine = m.policy
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.sessions[id]; ok {
		m.logger.Info("session reuse", "session_id", id)
		return &existing.session, false, nil
	}
	msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, err := m.loadSessionData(id)
	if err != nil {
		m.logger.Error("session load failed", "session_id", id, "error", err)
		return nil, false, err
	}
	created := len(msgs) == 0 && !m.sessionExistsInStore(id)
	rt := newRuntimeWithPublisher(id, m.agentID, m.hub, m.hub, m.llm, toolExec, policyEngine, m.store, m.logger,
		msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, turnOpts, m.triggerDelivery)
	m.sessions[id] = rt
	m.attachUserChildTools(rt)
	rt.start(m.ctx)
	rt.orch.RunSessionLifecyclePhase(context.Background(), id, "create")
	if created {
		rt.persist(context.Background())
		m.logger.Info("session created", "session_id", id, "restored", false, "fs_root", turnOpts.FSRoot)
	} else {
		m.logger.Info("session restored", "session_id", id, "messages", len(msgs), "has_pending_hitl", pending != nil)
	}
	return &rt.session, created, nil
}

// ReplaceWithOptions atomically replaces an existing runtime after the new
// runtime has been fully hydrated and started. If loading the replacement
// fails, the old runtime remains registered and continues serving requests.
// This is the reload counterpart to CreateWithOptions; callers must only use
// it at an idle boundary so the old Turn snapshot is not interrupted.
func (m *Manager) ReplaceWithOptions(requestedID string, turnOpts TurnOptions, toolExec tools.Executor, policyEngine *policy.Engine) (*Session, bool, error) {
	id := strings.TrimSpace(requestedID)
	if id == "" {
		return nil, false, fmt.Errorf("session id is required for ReplaceWithOptions")
	}
	if toolExec == nil {
		toolExec = m.tools
	}
	if policyEngine == nil {
		policyEngine = m.policy
	}

	m.mu.Lock()
	old := m.sessions[id]
	if old != nil && old.isChildSession() {
		m.mu.Unlock()
		return nil, false, fmt.Errorf("cannot replace child session")
	}
	var msgs []llm.Message
	var loaded []skills.LoadedSkill
	var pending *turn.PendingHITL
	var loopCount int
	var hookStore map[string]json.RawMessage
	var idleMarked bool
	var notifySeq, ackSeq int
	if old != nil {
		// Persist for cold-start recovery, but hydrate from the old runtime's
		// in-memory snapshot. This avoids losing a last-minute state change when
		// the store write fails or when the manager is embedded without a store.
		old.persist(context.Background())
		if m.store != nil {
			if _, _, _, _, _, _, _, _, err := m.loadSessionData(id); err != nil {
				m.mu.Unlock()
				m.logger.Error("session replacement store check failed; old runtime retained", "session_id", id, "error", err)
				return nil, false, err
			}
		}
		msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq = old.replacementData()
	} else {
		var err error
		msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, err = m.loadSessionData(id)
		if err != nil {
			m.mu.Unlock()
			m.logger.Error("session replacement load failed", "session_id", id, "error", err)
			return nil, false, err
		}
	}
	created := len(msgs) == 0 && !m.sessionExistsInStore(id)
	rt := newRuntimeWithPublisher(id, m.agentID, m.hub, m.hub, m.llm, toolExec, policyEngine, m.store, m.logger,
		msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, turnOpts, m.triggerDelivery)
	m.attachUserChildTools(rt)
	rt.start(m.ctx)
	rt.orch.RunSessionLifecyclePhase(context.Background(), id, "create")
	m.sessions[id] = rt
	m.mu.Unlock()

	if old != nil {
		old.stop()
	}
	if created {
		rt.persist(context.Background())
	}
	m.logger.Info("session replaced", "session_id", id, "had_previous_runtime", old != nil)
	return &rt.session, created, nil
}

// Create 创建或复用 session；若 DB 中已有则加载历史并启动 consumer。
func (m *Manager) Create(requestedID string) (*Session, bool, error) {
	id := strings.TrimSpace(requestedID)
	if id != "" {
		m.mu.Lock()
		defer m.mu.Unlock()
		if existing, ok := m.sessions[id]; ok {
			m.logger.Info("session reuse", "session_id", id)
			return &existing.session, false, nil
		}
		msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, err := m.loadSessionData(id)
		if err != nil {
			m.logger.Error("session load failed", "session_id", id, "error", err)
			return nil, false, err
		}
		created := len(msgs) == 0 && !m.sessionExistsInStore(id)
		rt := newRuntime(id, m.agentID, m.hub, m.llm, m.tools, m.policy, m.store, m.logger, msgs, loaded, pending, loopCount, hookStore, idleMarked, notifySeq, ackSeq, m.turn, m.triggerDelivery)
		m.sessions[id] = rt
		m.attachUserChildTools(rt)
		rt.start(m.ctx)
		rt.orch.RunSessionLifecyclePhase(context.Background(), id, "create")
		if created {
			rt.persist(context.Background())
		}
		if created {
			m.logger.Info("session created", "session_id", id, "restored", false)
		} else {
			m.logger.Info("session restored", "session_id", id, "messages", len(msgs), "has_pending_hitl", pending != nil)
		}
		return &rt.session, created, nil
	}

	newID, err := generateSessionID()
	if err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rt := newRuntime(newID, m.agentID, m.hub, m.llm, m.tools, m.policy, m.store, m.logger, nil, nil, nil, 0, nil, false, 0, 0, m.turn, m.triggerDelivery)
	m.sessions[newID] = rt
	m.attachUserChildTools(rt)
	rt.start(m.ctx)
	rt.orch.RunSessionLifecyclePhase(context.Background(), newID, "create")
	rt.persist(context.Background())
	m.logger.Info("session created", "session_id", newID, "restored", false)
	return &rt.session, true, nil
}

func (m *Manager) loadSessionData(sessionID string) ([]llm.Message, []skills.LoadedSkill, *turn.PendingHITL, int, map[string]json.RawMessage, bool, int, int, error) {
	if m.store == nil {
		return nil, nil, nil, 0, nil, false, 0, 0, nil
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, nil, nil, 0, nil, false, 0, 0, err
	}
	if rec == nil {
		return nil, nil, nil, 0, nil, false, 0, 0, nil
	}
	var pending *turn.PendingHITL
	if rec.RuntimeState.Pending != nil {
		pending = rec.RuntimeState.Pending
	}
	return rec.Messages, rec.LoadedSkills, pending, rec.RuntimeState.ToolLoopCount, rec.RuntimeState.HookStore, rec.RuntimeState.IdleAutoCompressApplied, rec.RuntimeState.NotifySeq, rec.RuntimeState.AckSeq, nil
}

func (m *Manager) sessionExistsInStore(sessionID string) bool {
	if m.store == nil {
		return false
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	return err == nil && rec != nil
}

// Get 按 ID 查找 session；不存在返回 nil。
func (m *Manager) Get(sessionID string) *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt, ok := m.sessions[strings.TrimSpace(sessionID)]; ok {
		return &rt.session
	}
	return nil
}

// ConfigRevision 返回内存 runtime 装入时的配置版本；不存在返回 0。
func (m *Manager) ConfigRevision(sessionID string) int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt, ok := m.sessions[strings.TrimSpace(sessionID)]; ok {
		return rt.configRevision
	}
	return 0
}

// RuntimeRevision 返回内存 runtime 装入时的独立配置版本。
func (m *Manager) RuntimeRevision(sessionID string) int64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt, ok := m.sessions[strings.TrimSpace(sessionID)]; ok {
		return rt.runtimeRevision
	}
	return 0
}

// RuntimeDigest 返回内存 runtime 的模型上下文输入摘要。
func (m *Manager) RuntimeDigest(sessionID string) string {
	if m == nil {
		return ""
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if rt, ok := m.sessions[strings.TrimSpace(sessionID)]; ok {
		return rt.runtimeDigest
	}
	return ""
}

// RefreshRuntimePromptContext synchronizes persisted prompt sidecar changes
// into an already-loaded runtime. It intentionally does not rebuild the
// runtime or mutate a running Turn's ModelContextSnapshot.
func (m *Manager) RefreshRuntimePromptContext(sessionID string, content promptcontext.Content, scope string) bool {
	if m == nil {
		return false
	}
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return false
	}
	rt.refreshPromptContext(content, scope)
	return true
}

// ListActive 返回内存中活跃 session。
func (m *Manager) ListActive() []*Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, rt := range m.sessions {
		out = append(out, &rt.session)
	}
	return out
}

// SetSessionPolicy 热更新指定 session 的策略引擎。
func (m *Manager) SetSessionPolicy(sessionID string, engine *policy.Engine) {
	if engine == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	if rt, ok := m.sessions[sessionID]; ok && rt != nil {
		rt.setPolicy(engine)
	}
}

// ToolNames 返回 registry 已知工具名。
func (m *Manager) ToolNames() []string {
	if m.tools == nil {
		return nil
	}
	defs := m.tools.Definitions()
	names := make([]string, 0, len(defs))
	for _, def := range defs {
		names = append(names, def.Function.Name)
	}
	return names
}

// ListPersisted 返回 DB 中全部 session 摘要。
func (m *Manager) ListPersisted(ctx context.Context) ([]store.Summary, error) {
	if m.store == nil {
		return nil, nil
	}
	return m.store.List(ctx)
}

// SessionDisplayMeta 用于 session 列表展示：优先 DB 中的 updated_at / 首条用户消息；新活跃 session 用当前时间。
func (m *Manager) SessionDisplayMeta(sessionID string) (firstUser string, updatedAt time.Time) {
	updatedAt = time.Now().UTC()
	if m.store == nil {
		return "", updatedAt
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil || rec == nil {
		return "", updatedAt
	}
	return rec.FirstUserMessage, rec.UpdatedAt
}

// RuntimeInfo 返回 session 运行时观测（队列深度、turn 状态）。
func (m *Manager) RuntimeInfo(sessionID string) (queuePending int, hasActiveTurn bool, turnState turn.State, err error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return 0, false, "", fmt.Errorf("agent_not_found")
	}
	state := rt.turnState()
	return rt.queueDepth(), state != turn.StateIdle, state, nil
}

// GetContextView 返回 session context 摘要（活跃 runtime 或 DB 持久化）。
func (m *Manager) GetContextView(sessionID string) (*ContextView, error) {
	rt := m.getRuntime(sessionID)
	if rt != nil {
		return rt.contextView(), nil
	}
	if m.store == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	pending := rec.RuntimeState.Pending
	stepCount := rec.RuntimeState.ToolLoopCount
	lifecycle, hasLifecycleProjection, projectionErr := m.loadLifecycleProjection(context.Background(), sessionID, rec.NodeID)
	if projectionErr != nil {
		m.logger.Warn("load persisted turn lifecycle projection failed", "session_id", sessionID, "error", projectionErr)
	} else if hasLifecycleProjection {
		pending = pendingFromLifecycleSnapshot(lifecycle, nil)
		stepCount = lifecycle.Usage.Steps
	}
	view := &ContextView{
		SessionID:             sessionID,
		MessagesCount:         len(rec.Messages),
		MessagesTotalTokens:   estimateMessageTokens(rec.Messages),
		PendingToolCallsCount: pendingToolCallsCount(pending),
		ToolLoopCount:         stepCount,
		LoadedSkills:          rec.LoadedSkills,
		Messages:              rec.Messages,
		HasActiveTurn:         lifecycle.HasActiveTurn,
		TurnID:                lifecycle.TurnID,
		StepID:                lifecycle.StepID,
		StepIndex:             lifecycle.StepIndex,
		ContextEpoch:          lifecycle.ContextEpoch,
		TurnStatus:            lifecycle.TurnStatus,
		TurnEndReason:         lifecycle.TurnEndReason,
		StepStatus:            lifecycle.StepStatus,
		StepEndReason:         lifecycle.StepEndReason,
		TurnGeneration:        lifecycle.Generation,
		RuntimeRevision:       lifecycle.RuntimeRevision,
		RuntimeDigest:         lifecycle.RuntimeDigest,
		PromptDigest:          lifecycle.PromptDigest,
		ToolDigest:            lifecycle.ToolDigest,
		RecoveryRequired:      lifecycle.RecoveryRequired,
	}
	if !hasLifecycleProjection && pending != nil {
		view.HasActiveTurn = true
		view.TurnState = turn.StateAwaitingTool
	} else if hasLifecycleProjection && lifecycle.StepStatus == turn.StepStatusWaitingInteraction {
		view.TurnState = turn.StateAwaitingTool
	} else if hasLifecycleProjection {
		view.TurnState = turnStateFromCoordinatorSnapshot(lifecycle)
	}
	if view.LoadedSkills == nil {
		view.LoadedSkills = []skills.LoadedSkill{}
	}
	if m.turn.SkillsEnabled && strings.TrimSpace(m.turn.SkillsRoot) != "" {
		catalog := skills.NewCatalog(m.turn.SkillsRoot, true, m.turn.SkillsMaxInPrompt)
		enrichContextPromptStats(view, catalog)
	} else {
		enrichContextPromptStats(view, nil)
	}
	return view, nil
}

// ContextSummary 返回 session 上下文摘要。
func (m *Manager) ContextSummary(sessionID string) (messageCount int, messages []llm.Message, err error) {
	rt := m.getRuntime(sessionID)
	if rt != nil {
		return rt.messageCount(), rt.messagesSnapshot(), nil
	}
	if m.store == nil {
		return 0, nil, fmt.Errorf("agent_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return 0, nil, err
	}
	if rec == nil {
		return 0, nil, fmt.Errorf("agent_not_found")
	}
	return len(rec.Messages), rec.Messages, nil
}

// LoadedSkills 返回 session 已加载 skills（内存活跃 session 或 DB 持久化）。
func (m *Manager) LoadedSkills(sessionID string) ([]skills.LoadedSkill, error) {
	rt := m.getRuntime(sessionID)
	if rt != nil {
		return rt.loadedSkillsSnapshot(), nil
	}
	if m.store == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	if rec.LoadedSkills == nil {
		return []skills.LoadedSkill{}, nil
	}
	return rec.LoadedSkills, nil
}

// CompressContext 对活跃 session 手动触发一次阻塞压缩。
func (m *Manager) CompressContext(ctx context.Context, sessionID string) (compression.ForceResult, error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return compression.ForceResult{}, fmt.Errorf("agent_not_found")
	}
	return rt.compressContext(ctx), nil
}

// ClearContext 清空对话历史；取消在途 turn、未完成命令与临时子 Agent。
func (m *Manager) ClearContext(sessionID string) (cancelled bool, err error) {
	sid := strings.TrimSpace(sessionID)
	if m.children != nil {
		m.children.CancelAllForParent(sid, "context cleared")
	}
	if reg := m.SessionTools(sid); reg != nil {
		_ = reg.CancelAllSessionJobs(sid)
	}
	rt := m.getRuntime(sid)
	if rt != nil {
		cancelled = rt.cancelTurn()
		rt.clearMessages(context.Background())
		return cancelled, nil
	}
	if m.store == nil {
		return false, fmt.Errorf("agent_not_found")
	}
	if err := m.store.ClearMessages(context.Background(), sid); err != nil {
		return false, fmt.Errorf("agent_not_found")
	}
	return false, nil
}

// Delete 释放 session：停止 consumer、移出内存并删除 DB 行。
func (m *Manager) Delete(sessionID string) (bool, error) {
	sid := strings.TrimSpace(sessionID)
	if m.children != nil {
		m.children.CancelAllForParent(sid, "parent session released")
	}
	wasActive := false
	m.mu.Lock()
	rt, ok := m.sessions[sid]
	if ok {
		wasActive = true
		delete(m.sessions, sid)
	}
	m.mu.Unlock()
	if ok {
		rt.stop()
	}
	m.logger.Info("session deleted from memory", "session_id", sid, "was_active", wasActive)
	if wasActive && m.OnReleased != nil {
		m.OnReleased(sid)
	}
	if m.store == nil {
		return wasActive, nil
	}
	deleted, err := m.store.Delete(context.Background(), sid)
	if err != nil {
		return false, err
	}
	return wasActive || deleted, nil
}

// EnqueueMessage 将 message/resume 入队；resume 优先直投等待中的 turn。
// userMessageName 仅对 request_type=message 生效；空串规范为 llm.UserNameHuman。
// contentParts 非空时与 content 合并为多模态 user 消息（image_url + text）。
func (m *Manager) EnqueueMessage(
	_ context.Context,
	sessionID, requestType, content string,
	contentParts []llm.ContentPart,
	resumeValue map[string]any,
	userMessageName string,
) (priority string, err error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		m.logger.Warn("enqueue message session not found", "session_id", sessionID)
		return "", fmt.Errorf("agent_not_found")
	}
	m.logger.Debug("enqueue message",
		"session_id", sessionID,
		"request_type", requestType,
	)
	if requestType == "resume" {
		m.logger.Info("resume enqueue request",
			"session_id", sessionID,
			"resume_value", resumeValue,
		)
		if m.children != nil && m.children.Enabled() {
			targetParent, routeErr := m.children.RouteResume(sessionID, resumeValue)
			if routeErr != nil {
				return "", routeErr
			}
			if !targetParent {
				m.logger.Info("resume enqueue routed",
					"session_id", sessionID,
					"route", "child_runtime",
				)
				return string(queue.PriorityResume), nil
			}
			// 父 session：RouteResume 内 DeliverParentResume 已入队，勿重复 enqueue。
			m.logger.Info("resume enqueue routed",
				"session_id", sessionID,
				"route", "parent_deliver_only",
			)
			return string(queue.PriorityResume), nil
		}
		m.logger.Info("resume enqueue routed",
			"session_id", sessionID,
			"route", "direct_runtime",
		)
		pending := rt.hasPendingHITL()
		state := rt.turnCoordinator.Snapshot()
		// A duplicate resume may race with the first queued resume. Accept it
		// while the logical Turn is still alive; the runtime consumer will treat
		// it as an idempotent stale command after the interaction is resolved.
		// Once the Turn is terminal, retain the strict no_pending_hitl guard.
		resumeMayBeInFlight := state.HasActiveTurn && !state.TurnStatus.Terminal()
		if !pending && !resumeMayBeInFlight {
			m.logger.Warn("resume rejected no pending hitl",
				"session_id", sessionID,
				"resume_value", resumeValue,
			)
			return "", fmt.Errorf("no_pending_hitl")
		}
		env := queue.Envelope{RequestType: "resume", ResumeValue: resumeValue}
		if err := rt.enqueue(env, queue.PriorityResume); err != nil {
			return "", err
		}
		return string(queue.PriorityResume), nil
	}

	p, err := queue.PriorityForRequestType(requestType)
	if err != nil {
		return "", err
	}
	if requestType == "message" {
		if !llm.UserInputValid(content, contentParts) {
			return "", fmt.Errorf("invalid_message")
		}
		if !m.turn.MultimodalEnabled && llm.UserInputHasImages(content, contentParts) {
			return "", fmt.Errorf("multimodal_disabled")
		}
	}
	env := queue.Envelope{
		RequestType:  requestType,
		Content:      content,
		ContentParts: contentParts,
		UserName:     userMessageName,
		ResumeValue:  resumeValue,
	}
	if err := rt.enqueue(env, p); err != nil {
		return "", err
	}
	return string(p), nil
}

// EnqueueAsyncToolResult 将异步工具完成结构化回灌入队（request_type=async_tool_result）。
func (m *Manager) EnqueueAsyncToolResult(sessionID string, payload queue.AsyncToolResultPayload) error {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		m.logger.Warn("async tool result enqueue skipped: session not found", "session_id", sessionID)
		return fmt.Errorf("agent_not_found")
	}
	env := queue.Envelope{
		RequestType:     queue.RequestTypeAsyncToolResult,
		AsyncToolResult: &payload,
	}
	return rt.enqueue(env, queue.PriorityAsyncCompletion)
}

// EnqueueToolResult 将 tool_result 续跑请求入队（同步工具批处理完成后使用）。
func (m *Manager) EnqueueToolResult(sessionID string) error {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		m.logger.Warn("tool result enqueue skipped: session not found", "session_id", sessionID)
		return fmt.Errorf("agent_not_found")
	}
	return rt.enqueueToolResult(nil, sessionID)
}

// ReconcileToolExecution resolves a side effect that was marked unknown after
// restart. The runtime validates Turn/Step fencing and queues the next model
// Step only after every unknown execution has a known terminal result.
func (m *Manager) ReconcileToolExecution(ctx context.Context, sessionID, turnID, stepID, executionID string, status turn.ToolExecutionStatus, content string) error {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return fmt.Errorf("agent_not_found")
	}
	return rt.reconcileToolExecution(ctx, turnID, stepID, executionID, status, content)
}

// CancelTurn 取消 session 当前在途 turn；无在途 turn 时返回 false。
func (m *Manager) CancelTurn(sessionID string) bool {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return false
	}
	// Canceling the composer turn must also cancel bash jobs that already
	// detached from the turn after timeout auto-degradation. Otherwise the UI
	// reports a canceled turn while the shell keeps running in the background.
	turnCancelled := rt.cancelTurn()
	jobsCancelled := false
	if reg := m.SessionTools(sessionID); reg != nil {
		jobsCancelled = reg.CancelAllSessionJobs(sessionID) > 0
	}
	return turnCancelled || jobsCancelled
}

// ListSessionSkills 返回 session 已加载与可用 skills。
func (m *Manager) ListSessionSkills(sessionID string) (loaded, available []skills.LoadedSkill, err error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return nil, nil, fmt.Errorf("agent_not_found")
	}
	loaded = rt.loadedSkillsSnapshot()
	if rt.skillsCatalog != nil {
		available = rt.skillsCatalog.ListMetadata()
	}
	return loaded, available, nil
}

// LoadSessionSkill 向 session 加载单个 skill（追加到 loaded 集合，受 max 限制）。
func (m *Manager) LoadSessionSkill(sessionID, skillName string) ([]skills.LoadedSkill, error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	current := rt.loadedSkillsSnapshot()
	names := make([]string, 0, len(current)+1)
	for _, item := range current {
		names = append(names, item.SkillName)
	}
	names = append(names, skillName)
	return rt.setLoadedSkillsByName(names), nil
}

// UnloadSessionSkill 从 session 卸载 skill。
func (m *Manager) UnloadSessionSkill(sessionID, skillName string) ([]skills.LoadedSkill, error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return nil, fmt.Errorf("agent_not_found")
	}
	return rt.unloadSkillsByName([]string{skillName}), nil
}

// SessionFSRoot 返回指定 session/agent 的有效 FSRoot（测试与调试用）。
func (m *Manager) SessionFSRoot(sessionID string) (string, bool) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return "", false
	}
	return rt.fsRoot, true
}

func (m *Manager) getRuntime(sessionID string) *runtime {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[strings.TrimSpace(sessionID)]
}

func (m *Manager) attachUserChildTools(rt *runtime) {
	if rt == nil || rt.isChildSession() || m.children == nil || !m.children.Enabled() {
		return
	}
	rt.orch.SetChildAgentManager(m.children)
}

func generateSessionID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return "sess-" + hex.EncodeToString(b[:]), nil
}
