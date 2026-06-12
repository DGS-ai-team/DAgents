// Package session 管理 session 表、per-session 队列与 turn 消费循环。
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
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
	FSRoot                   string
	MaxToolLoops             int
	SkillsRoot               string
	SkillsEnabled            bool
	SkillsMaxInPrompt        int
	RuntimeDir               string
	CompressionSilent        int
	CompressionBlocking      int
	RawMessageHistoryEnabled bool
	RawMessageHistoryDir     string
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

	triggerDelivery triggers.DeliveryTracker

	children *childagent.Manager
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
	defer m.mu.Unlock()
	for _, rt := range m.sessions {
		rt.stop()
	}
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
		msgs, loaded, pending, loopCount, err := m.loadSessionData(id)
		if err != nil {
			m.logger.Error("session load failed", "session_id", id, "error", err)
			return nil, false, err
		}
		created := len(msgs) == 0 && !m.sessionExistsInStore(id)
		rt := newRuntime(id, m.agentID, m.hub, m.llm, m.tools, m.policy, m.store, m.logger, msgs, loaded, pending, loopCount, m.turn, m.triggerDelivery)
		m.sessions[id] = rt
		m.attachUserChildTools(rt)
		rt.start(m.ctx)
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
	rt := newRuntime(newID, m.agentID, m.hub, m.llm, m.tools, m.policy, m.store, m.logger, nil, nil, nil, 0, m.turn, m.triggerDelivery)
	m.sessions[newID] = rt
	m.attachUserChildTools(rt)
	rt.start(m.ctx)
	rt.persist(context.Background())
	m.logger.Info("session created", "session_id", newID, "restored", false)
	return &rt.session, true, nil
}

func (m *Manager) loadSessionData(sessionID string) ([]llm.Message, []skills.LoadedSkill, *turn.PendingHITL, int, error) {
	if m.store == nil {
		return nil, nil, nil, 0, nil
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, nil, nil, 0, err
	}
	if rec == nil {
		return nil, nil, nil, 0, nil
	}
	var pending *turn.PendingHITL
	if rec.RuntimeState.Pending != nil {
		pending = rec.RuntimeState.Pending
	}
	return rec.Messages, rec.LoadedSkills, pending, rec.RuntimeState.ToolLoopCount, nil
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

// ReloadPolicy 热更新策略引擎并同步到全部活跃 session orchestrator。
func (m *Manager) ReloadPolicy(engine *policy.Engine) {
	if engine == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.policy = engine
	for _, rt := range m.sessions {
		rt.setPolicy(engine)
	}
}

// ReloadPolicyFromRuntime 从 runtime 目录重新加载策略并热更新。
func (m *Manager) ReloadPolicyFromRuntime(runtimeDir string) error {
	engine, err := policy.LoadRuntime(runtimeDir)
	if err != nil {
		return err
	}
	m.ReloadPolicy(engine)
	return nil
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

// RuntimeInfo 返回 session 运行时观测（队列深度、turn 状态）。
func (m *Manager) RuntimeInfo(sessionID string) (queuePending int, hasActiveTurn bool, turnState turn.State, err error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return 0, false, "", fmt.Errorf("session_not_found")
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
		return nil, fmt.Errorf("session_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("session_not_found")
	}
	pending := rec.RuntimeState.Pending
	view := &ContextView{
		SessionID:             sessionID,
		MessagesCount:         len(rec.Messages),
		MessagesTotalTokens:   estimateMessageTokens(rec.Messages),
		PendingToolCallsCount: pendingToolCallsCount(pending),
		ToolLoopCount:         rec.RuntimeState.ToolLoopCount,
		LoadedSkills:          rec.LoadedSkills,
		Messages:              rec.Messages,
	}
	if pending != nil {
		view.HasActiveTurn = true
		view.TurnState = turn.StateAwaitingTool
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
		return 0, nil, fmt.Errorf("session_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return 0, nil, err
	}
	if rec == nil {
		return 0, nil, fmt.Errorf("session_not_found")
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
		return nil, fmt.Errorf("session_not_found")
	}
	rec, err := m.store.Load(context.Background(), sessionID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, fmt.Errorf("session_not_found")
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
		return compression.ForceResult{}, fmt.Errorf("session_not_found")
	}
	return rt.compressContext(ctx), nil
}

// ClearContext 清空对话历史；若 turn 在途则先 cancel。
func (m *Manager) ClearContext(sessionID string) (cancelled bool, err error) {
	rt := m.getRuntime(sessionID)
	if rt != nil {
		cancelled = rt.cancelTurn()
		rt.clearMessages(context.Background())
		return cancelled, nil
	}
	if m.store == nil {
		return false, fmt.Errorf("session_not_found")
	}
	if err := m.store.ClearMessages(context.Background(), sessionID); err != nil {
		return false, fmt.Errorf("session_not_found")
	}
	return false, nil
}

// Delete 释放 session：停止 consumer、移出内存并删除 DB 行。
func (m *Manager) Delete(sessionID string) (bool, error) {
	sid := strings.TrimSpace(sessionID)
	if m.children != nil {
		m.children.CancelAllForParent(sid)
	}
	wasActive := false
	m.mu.Lock()
	if rt, ok := m.sessions[sid]; ok {
		wasActive = true
		rt.stop()
		delete(m.sessions, sid)
	}
	m.mu.Unlock()
	m.logger.Info("session deleted from memory", "session_id", sid, "was_active", wasActive)
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
func (m *Manager) EnqueueMessage(
	_ context.Context,
	sessionID, requestType, content string,
	resumeValue map[string]any,
) (priority string, err error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		m.logger.Warn("enqueue message session not found", "session_id", sessionID)
		return "", fmt.Errorf("session_not_found")
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
		if !rt.hasPendingHITL() {
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
		if strings.TrimSpace(content) == "" {
			return "", fmt.Errorf("invalid_message")
		}
	}
	env := queue.Envelope{RequestType: requestType, Content: content, ResumeValue: resumeValue}
	if err := rt.enqueue(env, p); err != nil {
		return "", err
	}
	return string(p), nil
}

// EnqueueAsyncToolResult 将异步工具完成结构化回灌入队（request_type=async_tool_result）。
func (m *Manager) EnqueueAsyncToolResult(sessionID string, payload queue.AsyncToolResultPayload) error {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return nil
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
		return nil
	}
	env := queue.Envelope{RequestType: queue.RequestTypeToolResult}
	return rt.enqueue(env, queue.PriorityToolResult)
}

// EnqueueBackgroundToolResult 兼容旧接口；已废弃，请使用 EnqueueAsyncToolResult。
func (m *Manager) EnqueueBackgroundToolResult(sessionID, summary string) error {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	return m.EnqueueAsyncToolResult(sessionID, queue.AsyncToolResultPayload{
		ToolName:   "background_job",
		Status:     "succeeded",
		ResultText: summary,
	})
}

// CancelTurn 取消 session 当前在途 turn；无在途 turn 时返回 false。
func (m *Manager) CancelTurn(sessionID string) bool {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return false
	}
	return rt.cancelTurn()
}

// ListSessionSkills 返回 session 已加载与可用 skills。
func (m *Manager) ListSessionSkills(sessionID string) (loaded, available []skills.LoadedSkill, err error) {
	rt := m.getRuntime(sessionID)
	if rt == nil {
		return nil, nil, fmt.Errorf("session_not_found")
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
		return nil, fmt.Errorf("session_not_found")
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
		return nil, fmt.Errorf("session_not_found")
	}
	return rt.unloadSkillsByName([]string{skillName}), nil
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
	rt.orch.SetChildAgentTools(m.children, false)
}

func generateSessionID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return "sess-" + hex.EncodeToString(b[:]), nil
}
