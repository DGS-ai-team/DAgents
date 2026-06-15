// Package api 提供 Agent Node 对本地 Client 暴露的 HTTP/SSE 端点。
//
// 职责边界：路由解析、请求/响应 JSON、错误码映射；session 队列与 turn 执行委托 session.Manager。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/a2aclient"
	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// Server 承载 Agent Node HTTP 路由与运行时依赖。
type Server struct {
	cfg           *config.Config
	llmRuntime    *llm.RuntimeSettings
	logger        *slog.Logger
	mux           *http.ServeMux
	sessions      *session.Manager // per-session 队列与 turn consumer
	stream        *stream.Hub      // 进程内 SSE 事件总线
	store         *store.SQLiteStore
	triggerStore  *triggers.Store
	triggerSched  *triggers.Scheduler
	registrar     *manage.Registrar
	inboxPoller   *manage.InboxPoller
	a2aCallerHITL *session.A2ACallerHITLBridge
}

// Option 为 NewServer 可选配置。
type Option func(*serverOptions)

type serverOptions struct {
	llmClient    llm.Client
	tools        *tools.Registry
	policyEngine *policy.Engine
	sqliteStore  *store.SQLiteStore
	skipStore    bool
}

// WithLLM 注入 LLM 客户端（单测/mock 用）。
func WithLLM(client llm.Client) Option {
	return func(o *serverOptions) {
		o.llmClient = client
	}
}

// WithTools 注入工具 registry（单测用）。
func WithTools(registry *tools.Registry) Option {
	return func(o *serverOptions) {
		o.tools = registry
	}
}

// WithPolicy 注入策略引擎（单测用）。
func WithPolicy(engine *policy.Engine) Option {
	return func(o *serverOptions) {
		o.policyEngine = engine
	}
}

// WithStore 注入 SQLite store（单测用）；传 nil 且 WithSkipStore 时禁用持久化。
func WithStore(st *store.SQLiteStore) Option {
	return func(o *serverOptions) {
		o.sqliteStore = st
	}
}

// WithSkipStore 禁用持久化（单测无需落盘时使用）。
func WithSkipStore() Option {
	return func(o *serverOptions) {
		o.skipStore = true
	}
}

// NewServer 根据已校验配置构造 HTTP 处理器树。
//
// 逻辑：
// 1. 装配 LLM、tools、policy、SQLite（可被 Option 覆盖）；
// 2. 创建 SSE Hub 与 session.Manager，挂载 turn/skills/compression 选项；
// 3. 可选初始化 triggers store/scheduler 与 bash 后台任务回灌；
// 4. 注册 /health、/v1/sessions、/v1/messages、/v1/streams 等路由。
//
// 关键边界：子系统初始化失败时尽量降级（tools/policy/store/triggers 打日志后继续），避免整进程无法启动。
func NewServer(cfg *config.Config, logger *slog.Logger, opts ...Option) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	llmRuntime := llm.NewRuntimeSettings(cfg)
	o := serverOptions{llmClient: llm.NewFromConfig(cfg, llmRuntime)}
	for _, opt := range opts {
		opt(&o)
	}
	// 生产路径：按 FSRoot 注册内置工具；失败时回退 "." 以免 API 完全不可用。
	if o.tools == nil {
		reg, err := tools.NewRegistry(cfg.FSRoot, 30, cfg.Tools.BashOutputEncoding, cfg.Tools.FileEncoding)
		if err != nil {
			logger.Error("tools registry init failed", "error", err)
			reg, _ = tools.NewRegistry(".", 30, cfg.Tools.BashOutputEncoding, cfg.Tools.FileEncoding)
		}
		if err := reg.SetBuiltinEnabled(cfg.Tools.NormalizedBuiltinEnabled()); err != nil {
			logger.Error("tools.enabled_groups invalid", "error", err)
		}
		reg.SetBashCompress(toolsBashCompressFromConfig(cfg.Tools))
		if cfg.Skills.Enabled {
			reg.SetSkillsCatalog(skills.NewCatalog(cfg.SkillsRoot(), true, cfg.Skills.MaxInPrompt))
		}
		o.tools = reg
	}
	if o.policyEngine == nil {
		engine, err := policy.LoadRuntime(cfg.RuntimeDir())
		if err != nil {
			logger.Error("policy load failed", "error", err, "dir", cfg.PolicyDir())
			o.policyEngine, _ = policy.LoadFile("")
		} else {
			o.policyEngine = engine
			logger.Info("policy loaded", "dir", cfg.PolicyDir())
		}
	}
	var st *store.SQLiteStore
	switch {
	case o.sqliteStore != nil:
		st = o.sqliteStore
	case !o.skipStore:
		opened, err := store.Open(cfg.SessionDBPath())
		if err != nil {
			logger.Error("sqlite store init failed", "error", err, "path", cfg.SessionDBPath())
		} else {
			st = opened
		}
	}

	hub := stream.NewHub(256, logger)
	hostsnapshot.CaptureAtStartup()
	// session.Manager 持有 per-session consumer；Publish 的事件经 Hub 广播给 SSE 订阅者。
	mgr := session.NewManager(cfg.AgentID, hub, o.llmClient, o.tools, o.policyEngine, st, session.TurnOptions{
		FSRoot:                   cfg.FSRoot,
		MaxToolLoops:             cfg.LLM.MaxToolLoops,
		SkillsRoot:               cfg.SkillsRoot(),
		SkillsEnabled:            cfg.Skills.Enabled,
		SkillsMaxInPrompt:        cfg.Skills.MaxInPrompt,
		RuntimeDir:               cfg.RuntimeDir(),
		CompressionSilent:        cfg.Compression.SilentTriggerTokens,
		CompressionBlocking:      cfg.Compression.BlockingTriggerTokens,
		RawMessageHistoryEnabled: cfg.RawMessageHistoryEnabled(),
		RawMessageHistoryDir:     cfg.RawMessageHistoryDir(),
		DuplicateToolCall: hooks.DuplicateConfig{
			Enabled:       cfg.DuplicateToolCallHookEnabled(),
			WindowSeconds: cfg.DuplicateToolCallWindowSeconds(),
		},
		ToolResult: hooks.ToolResultConfig{
			Enabled:         cfg.ToolResultHookEnabled(),
			MaxHistoryTokens: cfg.ToolResultMaxHistoryTokens(),
			SpillSubdir:     cfg.ToolResultSpillSubdir(),
			Tools:           cfg.ToolResultHookTools(),
			FSRoot:          cfg.FSRoot,
		},
	}, logger)
	childMgr := childagent.NewManager(childagent.Config{
		Enabled:                   cfg.ChildAgents.Enabled,
		DefaultTTLSeconds:         cfg.ChildAgents.DefaultTTLSeconds,
		MaxTTLSeconds:             cfg.ChildAgents.MaxTTLSeconds,
		DefaultMaxTurns:           cfg.ChildAgents.DefaultMaxTurns,
		MaxMaxTurns:               cfg.ChildAgents.MaxMaxTurns,
		MaxActivePerParent:        cfg.ChildAgents.MaxActivePerParent,
		DefaultWaitTimeoutSeconds: cfg.ChildAgents.DefaultWaitTimeoutSeconds,
	}, hub, cfg.AgentID, logger)
	mgr.SetChildAgentManager(childMgr)
	var triggerStore *triggers.Store
	var triggerSched *triggers.Scheduler
	if opened, err := triggers.OpenStore(cfg.TriggersStorePath(), 200); err != nil {
		logger.Warn("trigger store init failed", "error", err, "path", cfg.TriggersStorePath())
	} else {
		triggerStore = opened
		triggerStore.SetLogger(logger)
		triggerSched = triggers.NewScheduler(triggerStore, &session.TriggerSubmitter{Mgr: mgr}, cfg.Triggers.PollSeconds)
		triggerSched.SetLogger(logger)
		triggerSched.SetSessionResolver(mgr)
		mgr.SetTriggerDeliveryTracker(triggerStore)
		if o.tools != nil {
			o.tools.SetTriggerRuntime(triggerStore, triggerSched, cfg.AgentID)
		}
		if cfg.Triggers.Enabled && triggerSched != nil {
			triggerSched.Start()
		}
	}
	if o.tools != nil {
		// bash 后台任务完成时回灌 session 队列，触发新一轮 turn。
		o.tools.SetBackgroundJobNotifier(func(sessionID string, done tools.BackgroundJobDone) {
			if err := mgr.EnqueueAsyncToolResult(sessionID, queue.AsyncToolResultPayload{
				JobID:                  done.JobID,
				ToolName:               done.ToolName,
				ToolCallID:             done.ToolCallID,
				Status:                 done.Status,
				ResultText:             done.ResultText,
				ErrorText:              done.ErrorText,
				OutputCompressSavedPct: done.OutputCompressSavedPct,
				OutputCompressRawRunes: done.OutputCompressRawRunes,
				OutputCompressOutRunes: done.OutputCompressOutRunes,
			}); err != nil {
				logger.Warn("background tool completion enqueue failed", "session_id", sessionID, "error", err)
			}
		})
	}
	var registrar *manage.Registrar
	var inboxPoller *manage.InboxPoller
	var a2aBridge *session.A2ACallerHITLBridge
	if cfg.Manage.Enabled {
		registrar = manage.NewRegistrar(cfg, logger)
		registrar.SetToolNamesProvider(mgr.ToolNames)
		a2aBridge = session.NewA2ACallerHITLBridge(cfg.AgentID, hub)
		if o.tools != nil {
			compliancePeer := ""
			if card, cardErr := manage.LoadAgentCard(cfg.Manage.Registration.AgentCardPath); cardErr != nil {
				logger.Warn("agent card load failed for agent_invoke defaults", "error", cardErr, "path", cfg.Manage.Registration.AgentCardPath)
			} else if card != nil {
				compliancePeer = card.CompliancePeer()
			}
			o.tools.SetManageRuntime(
				a2aclient.New(cfg),
				cfg.AgentID,
				compliancePeer,
				cfg.Manage.Registration.Team,
				a2aBridge,
			)
		}
		if cfg.ManageA2AEnabled() {
			inboxPoller = manage.NewInboxPoller(cfg, logger)
			if handler := manage.ResolveInboxHandler(cfg, mgr, logger); handler != nil {
				inboxPoller.SetHandler(handler)
			}
		}
	}
	s := &Server{
		cfg:           cfg,
		llmRuntime:    llmRuntime,
		logger:        logger,
		mux:           http.NewServeMux(),
		stream:        hub,
		store:         st,
		sessions:      mgr,
		triggerStore:  triggerStore,
		triggerSched:  triggerSched,
		registrar:     registrar,
		inboxPoller:   inboxPoller,
		a2aCallerHITL: a2aBridge,
	}
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/agent/info", s.handleAgentInfo)
	s.mux.HandleFunc("POST /v1/sessions", s.handleCreateSession)
	s.mux.HandleFunc("GET /v1/sessions", s.handleListSessions)
	s.mux.HandleFunc("DELETE /v1/sessions/{session_id}", s.handleDeleteSession)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/clear-context", s.handleClearContext)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/compress", s.handleCompressContext)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/context", s.handleSessionContext)
	s.mux.HandleFunc("POST /v1/messages", s.handlePostMessage)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/cancel", s.handleCancelSession)
	s.mux.HandleFunc("GET /v1/sessions/{session_id}/skills", s.handleListSessionSkills)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/skills/load", s.handleLoadSessionSkill)
	s.mux.HandleFunc("POST /v1/sessions/{session_id}/skills/unload", s.handleUnloadSessionSkill)
	s.mux.HandleFunc("GET /v1/streams", s.handleStreams)
	s.registerTriggerRoutes()
	s.registerChildAgentRoutes()
	s.registerPolicyRoutes()
	s.registerLLMRoutes()
	return s
}

// Handler 返回可用于 http.Server 的根 Handler（含 access log 中间件）。
func (s *Server) Handler() http.Handler {
	return accessLogMiddleware(s.logger, s.mux)
}

// ListenAndServe 在配置的 listen 地址启动 HTTP 服务；ctx 取消时触发优雅关闭。
//
// 逻辑：
// 1. 后台 goroutine 调用 http.Server.ListenAndServe；
// 2. ctx 取消时依次停止 trigger scheduler、session consumer、SQLite，再 Shutdown HTTP；
// 3. 监听异常（非 ErrServerClosed）向上返回。
//
// 副作用：关闭 store、停止全部 session consumer。
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := s.cfg.ListenAddr()
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	regCtx, regCancel := context.WithCancel(ctx)
	defer regCancel()
	if s.registrar != nil {
		s.registrar.Start(regCtx)
	}
	if s.inboxPoller != nil {
		s.inboxPoller.Start(regCtx)
	}
	go func() {
		s.logger.Info("agent node listening", "addr", addr, "agent_id", s.cfg.AgentID)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("agent node shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		regCancel()
		if s.registrar != nil {
			s.registrar.Stop(shutdownCtx)
		}
		// 与启动顺序相反：先停后台任务与会话，再关 HTTP 监听。
		if s.triggerSched != nil {
			s.triggerSched.Stop()
		}
		s.sessions.Stop()
		if s.store != nil {
			_ = s.store.Close()
		}
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return <-errCh
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("listen %s: %w", addr, err)
		}
		return nil
	}
}

type healthResponse struct {
	Status  string `json:"status"`
	AgentID string `json:"agent_id"`
	Version string `json:"version"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// 探活：Client 启动前与运维脚本使用；无鉴权。
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		AgentID: s.cfg.AgentID,
		Version: version.Version,
	})
}

type agentInfoResponse struct {
	AgentID          string              `json:"agent_id"`
	ExposeToPeers    bool                `json:"expose_to_peers"`
	Capabilities     []string            `json:"capabilities"`
	ManageRegistered bool                `json:"manage_registered"`
	LLM              llm.LLMSettingsView `json:"llm"`
}

func (s *Server) handleAgentInfo(w http.ResponseWriter, _ *http.Request) {
	registered := false
	if s.registrar != nil {
		registered = s.registrar.Registered()
	}
	llmView := llm.LLMSettingsView{}
	if s.llmRuntime != nil {
		llmView = s.llmRuntime.Snapshot()
	}
	writeJSON(w, http.StatusOK, agentInfoResponse{
		AgentID:          s.cfg.AgentID,
		ExposeToPeers:    s.cfg.ExposeToPeers,
		Capabilities:     s.cfg.Capabilities(),
		ManageRegistered: registered,
		LLM:              llmView,
	})
}

type createSessionRequest struct {
	SessionID *string `json:"session_id"`
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
	AgentID   string `json:"agent_id"`
	Created   bool   `json:"created"`
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	// POST /v1/sessions：可选 session_id；省略则 Node 生成并启动 consumer。
	var req createSessionRequest
	if err := decodeJSON(r, &req); err != nil && r.ContentLength > 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	requested := ""
	if req.SessionID != nil {
		requested = *req.SessionID
	}
	sess, created, err := s.sessions.Create(requested)
	if err != nil {
		s.logger.Error("create session failed", "requested_id", requested, "error", err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	s.logger.Debug("create session ok", "session_id", sess.ID, "created", created)
	writeJSON(w, http.StatusOK, createSessionResponse{
		SessionID: sess.ID,
		AgentID:   sess.AgentID,
		Created:   created,
	})
}

type sessionSummary struct {
	SessionID        string `json:"session_id"`
	AgentID          string `json:"agent_id"`
	Active           bool   `json:"active"`
	MessageCount     int    `json:"message_count,omitempty"`
	FirstUserMessage string `json:"first_user_message,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
	// 以下字段仅在 active=true（内存活跃 session）时有意义。
	QueuePending  int    `json:"queue_pending,omitempty"`
	HasActiveTurn bool   `json:"has_active_turn,omitempty"`
	RunTurnPhase  string `json:"run_turn_phase,omitempty"`
}

type listSessionsResponse struct {
	Sessions []sessionSummary `json:"sessions"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	// GET /v1/sessions：合并内存活跃 session 与 SQLite 持久化摘要（去重）。
	active := s.sessions.ListActiveUser()
	persisted, err := s.sessions.ListPersisted(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}

	seen := make(map[string]struct{}, len(active))
	items := make([]sessionSummary, 0, len(active)+len(persisted))
	for _, sess := range active {
		seen[sess.ID] = struct{}{}
		queuePending, hasActiveTurn, turnState, _ := s.sessions.RuntimeInfo(sess.ID)
		count, _, _ := s.sessions.ContextSummary(sess.ID)
		items = append(items, sessionSummary{
			SessionID:     sess.ID,
			AgentID:       sess.AgentID,
			Active:        true,
			MessageCount:  count,
			QueuePending:  queuePending,
			HasActiveTurn: hasActiveTurn,
			RunTurnPhase:  turn.RunTurnPhase(turnState),
		})
	}
	for _, sum := range persisted {
		if _, ok := seen[sum.SessionID]; ok {
			continue // 已在内存活跃列表中出现过
		}
		items = append(items, sessionSummary{
			SessionID:        sum.SessionID,
			AgentID:          sum.AgentID,
			Active:           false,
			MessageCount:     sum.MessageCount,
			FirstUserMessage: sum.FirstUserMessage,
			UpdatedAt:        sum.UpdatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, listSessionsResponse{Sessions: items})
}

type deleteSessionResponse struct {
	SessionID string `json:"session_id"`
	Released  bool   `json:"released"`
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	// DELETE /v1/sessions/{id}：停止 consumer 并删除 SQLite 行。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_session", "session_id is required", nil)
		return
	}
	released, err := s.sessions.Delete(sessionID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	if !released {
		writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		return
	}
	writeJSON(w, http.StatusOK, deleteSessionResponse{
		SessionID: sessionID,
		Released:  true,
	})
}

type clearContextResponse struct {
	SessionID     string `json:"session_id"`
	Cleared       bool   `json:"cleared"`
	CancelledTurn bool   `json:"cancelled_turn"`
}

func (s *Server) handleClearContext(w http.ResponseWriter, r *http.Request) {
	// POST clear-context：清空 messages；在途 turn 会先 cancel。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_session", "session_id is required", nil)
		return
	}
	cancelled, err := s.sessions.ClearContext(sessionID)
	if err != nil {
		if err.Error() == "session_not_found" {
			writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, clearContextResponse{
		SessionID:     sessionID,
		Cleared:       true,
		CancelledTurn: cancelled,
	})
}

type contextMessagePreview struct {
	Role                string `json:"role"`
	Content             string `json:"content,omitempty"`
	ToolCallID          string `json:"tool_call_id,omitempty"`
	ToolCallsCount      int    `json:"tool_calls_count,omitempty"`
	HasReasoningContent bool   `json:"has_reasoning_content,omitempty"`
}

type sessionContextResponse struct {
	SessionID             string                  `json:"session_id"`
	MessagesCount         int                     `json:"messages_count"`
	PendingToolCallsCount int                     `json:"pending_tool_calls_count"`
	MessagesTotalTokens   int                     `json:"messages_total_tokens"`
	ToolLoopCount         int                     `json:"tool_loop_count"`
	QueuePending          int                     `json:"queue_pending"`
	HasActiveTurn         bool                    `json:"has_active_turn"`
	TurnState             string                  `json:"turn_state,omitempty"`
	RunTurnPhase                   string                  `json:"run_turn_phase"`
	SystemPrompt                   string                  `json:"system_prompt,omitempty"`
	SystemPromptEstimatedTokens    int                     `json:"system_prompt_estimated_tokens"`
	SkillsCatalogEstimatedTokens   int                     `json:"skills_catalog_estimated_tokens"`
	SkillsCatalogBloatThreshold    int                     `json:"skills_catalog_bloat_threshold"`
	LoadedSkills                   []skills.LoadedSkill    `json:"loaded_skills"`
	RecentMessages                 []contextMessagePreview `json:"recent_messages"`
	LastCompression                *compression.LastCompressionSnapshot `json:"last_compression,omitempty"`
}

func (s *Server) handleSessionContext(w http.ResponseWriter, r *http.Request) {
	// GET context：只读快照；recent_messages 最多 10 条、content 截断 200 字符。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_session", "session_id is required", nil)
		return
	}
	view, err := s.sessions.GetContextView(sessionID)
	if err != nil {
		if err.Error() == "session_not_found" {
			writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	const previewLimit = 10
	start := 0
	if len(view.Messages) > previewLimit {
		start = len(view.Messages) - previewLimit
	}
	recent := make([]contextMessagePreview, 0, len(view.Messages)-start)
	for _, m := range view.Messages[start:] {
		content := m.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		recent = append(recent, contextMessagePreview{
			Role:                m.Role,
			Content:             content,
			ToolCallID:          m.ToolCallID,
			ToolCallsCount:      len(m.ToolCalls),
			HasReasoningContent: strings.TrimSpace(m.ReasoningContent) != "",
		})
	}
	resp := sessionContextResponse{
		SessionID:             view.SessionID,
		MessagesCount:         view.MessagesCount,
		PendingToolCallsCount: view.PendingToolCallsCount,
		MessagesTotalTokens:   view.MessagesTotalTokens,
		ToolLoopCount:         view.ToolLoopCount,
		QueuePending:          view.QueuePending,
		HasActiveTurn:         view.HasActiveTurn,
		SystemPrompt:                 view.SystemPrompt,
		SystemPromptEstimatedTokens:  view.SystemPromptEstimatedTokens,
		SkillsCatalogEstimatedTokens: view.SkillsCatalogEstimatedTokens,
		SkillsCatalogBloatThreshold:  view.SkillsCatalogBloatThreshold,
		LoadedSkills:                 view.LoadedSkills,
		RecentMessages:               recent,
		LastCompression:              view.LastCompression,
		RunTurnPhase:                 turn.RunTurnPhase(view.TurnState),
	}
	if view.TurnState != "" {
		resp.TurnState = string(view.TurnState)
	}
	if resp.LoadedSkills == nil {
		resp.LoadedSkills = []skills.LoadedSkill{}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCompressContext(w http.ResponseWriter, r *http.Request) {
	// POST compress：手动触发一次阻塞压缩（忽略 token 阈值）。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_session", "session_id is required", nil)
		return
	}
	result, err := s.sessions.CompressContext(r.Context(), sessionID)
	if err != nil {
		if err.Error() == "session_not_found" {
			writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	if result.Status == "busy" {
		writeAPIError(w, http.StatusConflict, "turn_busy", "当前 turn 进行中，请稍后再试", map[string]any{
			"session_id": sessionID,
			"status":     result.Status,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type postMessageRequest struct {
	SessionID       string         `json:"session_id"`
	RequestType     string         `json:"request_type"`
	Content         string         `json:"content"`
	UserMessageName string         `json:"user_message_name,omitempty"`
	ResumeValue     map[string]any `json:"resume_value"`
}

type postMessageResponse struct {
	Accepted  bool   `json:"accepted"`
	SessionID string `json:"session_id"`
	Priority  string `json:"priority"`
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	// POST /v1/messages：message 入队 human 优先级；resume 用于 HITL 续跑。
	var req postMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_session", "session_id is required", nil)
		return
	}
	requestType := strings.TrimSpace(req.RequestType)
	if requestType == "" {
		requestType = "message"
	}
	if requestType == "resume" && s.a2aCallerHITL != nil && s.a2aCallerHITL.DeliverA2ACallerResume(sessionID, req.ResumeValue) {
		writeJSON(w, http.StatusOK, postMessageResponse{
			Accepted:  true,
			SessionID: sessionID,
			Priority:  string(queue.PriorityHuman),
		})
		return
	}

	priority, err := s.sessions.EnqueueMessage(r.Context(), sessionID, requestType, req.Content, req.ResumeValue, req.UserMessageName)
	if err != nil {
		// 业务错误映射为 HTTP 状态 + 统一 error 体（见 errors.go）。
		switch err.Error() {
		case "session_not_found":
			writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		case "invalid_message":
			writeAPIError(w, http.StatusBadRequest, "invalid_message", "content 不能为空", nil)
		case "invalid_request_type":
			writeAPIError(w, http.StatusBadRequest, "invalid_request_type", "不支持的 request_type", nil)
		case "no_pending_hitl":
			writeAPIError(w, http.StatusConflict, "no_pending_hitl", "当前无等待中的 HITL", nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, postMessageResponse{
		Accepted:  true,
		SessionID: sessionID,
		Priority:  priority,
	})
}

type cancelTurnResponse struct {
	SessionID string `json:"session_id"`
	Cancelled bool   `json:"cancelled"`
}

func (s *Server) handleCancelSession(w http.ResponseWriter, r *http.Request) {
	// POST cancel：取消在途 turn；无在途任务时 cancelled=false。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_session", "session_id is required", nil)
		return
	}
	if s.sessions.Get(sessionID) == nil {
		writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		return
	}
	cancelled := s.sessions.CancelTurn(sessionID)
	writeJSON(w, http.StatusOK, cancelTurnResponse{
		SessionID: sessionID,
		Cancelled: cancelled,
	})
}

func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	// GET /v1/streams：SSE 长连接；Client 用 session_id 查询参数在本地过滤事件。
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming not supported", nil)
		return
	}

	sessionFilter := strings.TrimSpace(r.URL.Query().Get("session_id"))
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
	live := strings.TrimSpace(r.URL.Query().Get("live")) == "1"
	// live=1：TUI 重连时只收增量，避免 replay 历史 done 干扰 wait_user_turn。
	if live {
		lastSeq = s.stream.CurrentSeq()
	}
	s.logger.Info("sse subscribe",
		"session_id", sessionFilter,
		"live", live,
		"after_seq", lastSeq,
		"remote", r.RemoteAddr,
	)
	defer s.logger.Debug("sse unsubscribe", "session_id", sessionFilter, "remote", r.RemoteAddr)

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events := s.stream.Subscribe(lastSeq)
	defer s.stream.Unsubscribe(events)

	// 首包注释行立即 flush，使 Client 立刻收到 HTTP 200；否则 idle 连接要等 15s heartbeat。
	if _, err := fmt.Fprintf(w, ": connected\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			// Client 断开连接；Unsubscribe 在 defer 中执行。
			return
		case <-ticker.C:
			// SSE 注释行保活，避免代理/防火墙空闲断连。
			if _, err := fmt.Fprintf(w, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if sessionFilter != "" && ev.SessionID != sessionFilter {
				continue // Hub 为全局广播；按 session 过滤在 handler 内完成
			}
			if _, err := w.Write([]byte(ev.FormatSSE())); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleListSessionSkills(w http.ResponseWriter, r *http.Request) {
	// GET skills：返回 session 已加载与磁盘可用 skill 元数据。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	loaded, available, err := s.sessions.ListSessionSkills(sessionID)
	if err != nil {
		if err.Error() == "session_not_found" {
			writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":       sessionID,
		"loaded_skills":    loaded,
		"available_skills": available,
	})
}

type skillNameRequest struct {
	SkillName string `json:"skill_name"`
}

func (s *Server) handleLoadSessionSkill(w http.ResponseWriter, r *http.Request) {
	// POST skills/load：与 load_skills 工具语义一致，供 Client 设置页调用。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	var req skillNameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	loaded, err := s.sessions.LoadSessionSkill(sessionID, req.SkillName)
	if err != nil {
		if err.Error() == "session_not_found" {
			writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":    sessionID,
		"loaded_skills": loaded,
	})
}

func (s *Server) handleUnloadSessionSkill(w http.ResponseWriter, r *http.Request) {
	// POST skills/unload：从 session 移除指定 skill。
	sessionID := strings.TrimSpace(r.PathValue("session_id"))
	var req skillNameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	loaded, err := s.sessions.UnloadSessionSkill(sessionID, req.SkillName)
	if err != nil {
		if err.Error() == "session_not_found" {
			writeAPIError(w, http.StatusNotFound, "session_not_found", "session 不存在", map[string]any{"session_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":    sessionID,
		"loaded_skills": loaded,
	})
}

// writeJSON 写入 JSON 响应并设置 Content-Type；编码失败时静默（极少发生）。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func toolsBashCompressFromConfig(toolsCfg config.ToolsConfig) tools.BashCompressConfig {
	out := tools.DefaultBashCompressConfig()
	if toolsCfg.BashCompress.Enabled != nil {
		out.Enabled = *toolsCfg.BashCompress.Enabled
	}
	if toolsCfg.BashCompress.MaxOutputChars > 0 {
		out.MaxOutputChars = toolsCfg.BashCompress.MaxOutputChars
	}
	if toolsCfg.BashCompress.MaxOutputCharsStderr > 0 {
		out.MaxOutputCharsStderr = toolsCfg.BashCompress.MaxOutputCharsStderr
	}
	return out
}
