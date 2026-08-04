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

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/media"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/sandbox"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/node/internal/webui"
	"github.com/DGS-ai-team/DAgents/node/internal/wecom"
	"github.com/DGS-ai-team/DAgents/node/internal/workgroup"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// Server 承载 Agent Node HTTP 路由与运行时依赖。
type Server struct {
	cfg           *config.Config
	configPath    string
	llmRuntime    *llm.RuntimeSettings
	logger        *slog.Logger
	mux           *http.ServeMux
	sessions      *session.Manager // per-session 队列与 turn consumer（过渡期与 agent_id 1:1）
	agents        *store.AgentStore
	llmConfigs    *store.LLMConfigStore
	nodeSettings  *store.NodeSettingsStore
	stream        *stream.Hub // 进程内 SSE 事件总线
	store         *store.SQLiteStore
	triggerStore  *triggers.Store
	triggerSched  *triggers.Scheduler
	registrar     *manage.Registrar
	updateChecker   *manage.UpdateChecker
	packageUploader *manage.PackageUploader
	control         *manage.ControlClient
	tools           *tools.Registry
	browserMgr      *browser.Manager
	mediaRegister   tools.MediaRegisterFunc
	sandboxPool     *sandbox.Pool
	workgroupWorker *workgroup.Worker
	workgroupDialer *workgroup.Dialer
}

// Option 为 NewServer 可选配置。
type Option func(*serverOptions)

type serverOptions struct {
	llmClient    llm.Client
	tools        *tools.Registry
	policyEngine *policy.Engine
	sqliteStore  *store.SQLiteStore
	nodeSettings *store.NodeSettingsStore
	skipStore    bool
	configPath   string
}

// WithConfigPath 记录 Node 启动时加载的 config.yaml 路径（供 Web UI 保存设置）。
func WithConfigPath(path string) Option {
	return func(o *serverOptions) {
		o.configPath = strings.TrimSpace(path)
	}
}

// WithNodeSettings 注入 Node 设置库（由 main BootstrapNodeSettings 打开）。
func WithNodeSettings(ns *store.NodeSettingsStore) Option {
	return func(o *serverOptions) {
		o.nodeSettings = ns
	}
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
// 4. 注册 /health、/v1/agents、/v1/messages、/v1/streams 等路由。
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
	if o.nodeSettings == nil && !o.skipStore && cfg != nil {
		ns, err := store.BootstrapNodeSettings(context.Background(), cfg, o.configPath, logger)
		if err != nil {
			logger.Error("node settings bootstrap failed", "error", err, "path", cfg.NodeSettingsDBPath())
		} else {
			o.nodeSettings = ns
			llmRuntime.SyncFromConfig(cfg)
		}
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
		reg.SetMultimodalEnabled(cfg.MultimodalEnabled())
		reg.SetBashCompress(toolsBashCompressFromConfig(cfg.Tools))
		if cfg.Skills.Enabled {
			reg.SetSkillsCatalog(skills.NewCatalog(cfg.SkillsRoot(), true, cfg.Skills.MaxInPrompt))
		}
		o.tools = reg
	}
	if o.policyEngine == nil {
		o.policyEngine = policy.NewEngineFromMaps(policy.LoadSeedMaps())
		logger.Info("policy default engine seeded (per-agent policy stored in agents.db)")
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
	var agentsStore *store.AgentStore
	if !o.skipStore {
		opened, err := store.OpenAgents(cfg.AgentsDBPath())
		if err != nil {
			logger.Error("agents store init failed", "error", err, "path", cfg.AgentsDBPath())
		} else {
			agentsStore = opened
		}
	}
	var llmConfigStore *store.LLMConfigStore
	if !o.skipStore {
		opened, err := store.OpenLLMConfigs(cfg.LLMConfigsDBPath(), cfg.RuntimeDir())
		if err != nil {
			logger.Error("llm configs store init failed", "error", err, "path", cfg.LLMConfigsDBPath())
		} else {
			llmConfigStore = opened
			if err := store.MigrateLLMConfigsFromConfig(context.Background(), llmConfigStore, cfg); err != nil {
				logger.Error("llm configs migrate failed", "error", err)
			} else if records, err := llmConfigStore.List(context.Background()); err != nil {
				logger.Error("llm configs list failed", "error", err)
			} else if len(records) > 0 {
				active := cfg.LLM.ActiveProfileID()
				if active == "" {
					active = records[0].ID
				}
				store.ApplyLLMConfigsToConfig(cfg, records, active)
				llmRuntime.SyncFromConfig(cfg)
				if key, err := llmConfigStore.ResolveAPIKey(context.Background(), cfg.LLM.ActiveProfileID()); err == nil {
					llmRuntime.SetAPIKey(key)
				}
			}
		}
	}

	hub := stream.NewHub(256, logger)
	hostsnapshot.CaptureAtStartup()
	injectTodayDateEnabled := cfg.InjectTodayDateHookEnabled()
	// session.Manager 持有 per-session consumer；Publish 的事件经 Hub 广播给 SSE 订阅者。
	mgr := session.NewManager(cfg.NodeID, hub, o.llmClient, o.tools, o.policyEngine, st, session.TurnOptions{
		FSRoot:                   cfg.FSRoot,
		// MaxToolLoops 由各 Agent config_snapshot（defaults.llm.max_tool_loops）在装入 runtime 时写入。
		SkillsRoot:               cfg.SkillsRoot(),
		SkillsEnabled:            cfg.Skills.Enabled,
		SkillsMaxInPrompt:        cfg.Skills.MaxInPrompt,
		RuntimeDir:               cfg.RuntimeDir(),
		CompressionSilent:           cfg.Compression.SilentTriggerTokens,
		CompressionBlocking:         cfg.Compression.BlockingTriggerTokens,
		IdleAutoCompressSeconds:     cfg.Compression.IdleAutoCompressSeconds,
		IdleAutoCompressPollSeconds: cfg.Compression.IdleAutoCompressPollSeconds,
		IdleAutoCompressMinTokens:   cfg.Compression.IdleAutoCompressMinTokens,
		RawMessageHistoryEnabled:    cfg.RawMessageHistoryEnabled(),
		RawMessageHistoryDir:     cfg.RawMessageHistoryDir(),
		DuplicateToolCall: hooks.DuplicateConfig{
			Enabled:       cfg.DuplicateToolCallHookEnabled(),
			WindowSeconds: cfg.DuplicateToolCallWindowSeconds(),
		},
		ToolResult: hooks.ToolResultConfig{
			Enabled:              cfg.ToolResultHookEnabled(),
			SpillThresholdTokens: cfg.ToolResultSpillThresholdTokens(),
			Tools:                cfg.ToolResultHookTools(),
			FSRoot:               cfg.FSRoot,
		},
		InjectTodayDate: hooks.InjectTodayDateConfig{Enabled: &injectTodayDateEnabled},
		PluginHooks: hooks.PluginsConfigFromShared(cfg.Hooks, cfg.RuntimeDir()),
		HookHost: turn.HookHostConfig{
			MaxLLMCalls:   cfg.HooksHostMaxLLMCalls(),
			HistoryWindow: cfg.HooksHostHistoryWindow(),
			RuntimeDir:    cfg.RuntimeDir(),
			SkillsRoot:    cfg.SkillsRoot(),
		},
		MultimodalEnabled: cfg.MultimodalEnabled(),
	}, logger)
	sandboxPool := sandbox.NewPool(sandbox.DefaultIdleTimeout, logger)
	sandboxPool.StartIdleReaper(time.Minute)
	mgr.OnReleased = func(sessionID string) {
		sandboxPool.Release(sessionID)
	}
	childMgr := childagent.NewManager(childagent.Config{
		Enabled:                   cfg.ChildAgents.Enabled,
		DefaultTTLSeconds:         cfg.ChildAgents.DefaultTTLSeconds,
		MaxTTLSeconds:             cfg.ChildAgents.MaxTTLSeconds,
		DefaultMaxTurns:           cfg.ChildAgents.DefaultMaxTurns,
		MaxMaxTurns:               cfg.ChildAgents.MaxMaxTurns,
		MaxActivePerParent:        cfg.ChildAgents.MaxActivePerParent,
		DefaultWaitTimeoutSeconds: cfg.ChildAgents.DefaultWaitTimeoutSeconds,
	}, hub, cfg.NodeID, logger)
	mgr.SetChildAgentManager(childMgr)
	if cfg.IdleAutoCompressEnabled() {
		mgr.StartIdleAutoCompressScanner()
	}
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
		if cfg.Triggers.Enabled && triggerSched != nil {
			triggerSched.Start()
		}
	}
	mediaRegister := tools.MediaRegisterFunc(func(ctx context.Context, toolCallID, relPath, source, label, caption string) (*tools.MediaArtifactRef, error) {
		sid := tools.SessionIDFromContext(ctx)
		if sid == "" {
			return nil, fmt.Errorf("session required for media register")
		}
		art, err := mgr.RegisterSessionMedia(sid, media.RegisterOpts{
			Path:       relPath,
			Source:     source,
			ToolCallID: toolCallID,
			Label:      label,
			Caption:    caption,
		})
		if err != nil {
			return nil, err
		}
		return &tools.MediaArtifactRef{
			ID:      art.ID,
			Kind:    art.Kind,
			MIME:    art.MIME,
			URL:     art.PublicURL(),
			Label:   art.Label,
			Caption: art.Caption,
		}, nil
	})
	var browserMgr *browser.Manager
	if cfg.BrowserEnabled() {
		bm, err := browser.NewManager(cfg, nil)
		if err != nil {
			logger.Error("browser manager init failed", "error", err)
		} else {
			browserMgr = bm
			logger.Info("browser tools enabled", "headed", cfg.BrowserHeaded())
		}
	}
	var registrar *manage.Registrar
	var updateChecker *manage.UpdateChecker
	var packageUploader *manage.PackageUploader
	if cfg.Manage.Enabled {
		registrar = manage.NewRegistrar(cfg, logger)
		registrar.SetToolNamesProvider(mgr.ToolNames)
		if !manage.UpdateDelegatedToShell() {
			updateChecker = manage.NewUpdateChecker(cfg, logger)
		}
		packageUploader = manage.NewPackageUploader(cfg, logger)
	}
	control := manage.NewControlClient(cfg)
	var wgWorker *workgroup.Worker
	var wgDialer *workgroup.Dialer
	if cfg.ManageWorkgroupEnabled() {
		toolNames := []string{}
		if mgr != nil {
			toolNames = mgr.ToolNames()
		}
		wgWorker = workgroup.NewWorker(workgroup.Config{
			NodeID:        cfg.NodeID,
			NodeToolNames: toolNames,
		})
		wgDialer = &workgroup.Dialer{
			ManageURL: cfg.Manage.URL,
			NodeID:    cfg.NodeID,
			Token:     cfg.Manage.NodeToken,
			Worker:    wgWorker,
			ListWorkgroups: func(ctx context.Context) ([]string, error) {
				items, err := control.ListWorkgroups(ctx, manage.WorkgroupListSubscribed)
				if err != nil {
					return nil, err
				}
				ids := make([]string, 0, len(items))
				for _, it := range items {
					if id := strings.TrimSpace(it.WorkgroupID); id != "" {
						ids = append(ids, id)
					}
				}
				return ids, nil
			},
		}
		logger.Info("workgroup dialer enabled", "manage_url", cfg.Manage.URL)
	}
	s := &Server{
		cfg:           cfg,
		configPath:    o.configPath,
		llmRuntime:    llmRuntime,
		logger:        logger,
		mux:           http.NewServeMux(),
		stream:        hub,
		store:         st,
		agents:        agentsStore,
		llmConfigs:    llmConfigStore,
		nodeSettings:  o.nodeSettings,
		sessions:      mgr,
		triggerStore:  triggerStore,
		triggerSched:  triggerSched,
		registrar:       registrar,
		updateChecker:   updateChecker,
		packageUploader: packageUploader,
		control:         control,
		tools:           o.tools,
		browserMgr:      browserMgr,
		mediaRegister:   mediaRegister,
		sandboxPool:     sandboxPool,
		workgroupWorker: wgWorker,
		workgroupDialer: wgDialer,
	}
	// 默认工具表与后续 per-agent Registry 共用同一套 Node 运行时依赖挂载。
	s.attachNodeRuntimeDeps(s.tools, cfg.NodeID)
	if client := wecom.NewClientFromConfig(cfg); client != nil {
		logger.Info("wecom webhook tools enabled")
	}
	hub.SetEventListener(func(ev stream.Event) {
		mgr.OnStreamEvent(ev)
	})
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("GET /v1/agent/info", s.handleAgentInfo)
	s.mux.HandleFunc("GET /v1/agent/update", s.handleAgentUpdate)
	s.mux.HandleFunc("GET /v1/agent/upgrade-readiness", s.handleAgentUpgradeReadiness)
	s.registerAgentRoutes()
	s.registerWorkgroupRoutes()
	s.registerScreenRoutes()
	s.registerToolCallControlRoutes()
	s.registerUIAggregateRoutes()
	s.mux.HandleFunc("POST /v1/messages", s.handlePostMessage)
	s.mux.HandleFunc("GET /v1/streams", s.handleStreams)
	s.registerTriggerRoutes()
	s.registerMediaRoutes()
	s.registerPolicyRoutes()
	s.registerLLMRoutes()
	s.registerSetupRoutes()
	s.registerManageUploadRoutes()
	s.mux.HandleFunc("GET /v1/skills/catalog", s.handleNodeSkillsCatalog)
	if cfg.UIEnabled() {
		s.mux.Handle("GET /ui/", webui.Handler())
		s.mux.HandleFunc("GET /ui", webui.RedirectHandler())
	}
	return s
}

// attachNodeRuntimeDeps 将 Node 级运行时依赖挂到工具 Registry（默认表与 per-agent 共用）。
func (s *Server) attachNodeRuntimeDeps(reg *tools.Registry, targetAgentID string) {
	if s == nil || reg == nil {
		return
	}
	attachTriggerRuntime(reg, s.triggerStore, s.triggerSched, targetAgentID)
	attachWeComRuntime(reg, s.cfg)
	attachBackgroundJobNotifier(reg, s.sessions, s.logger)
	if s.mediaRegister != nil {
		reg.SetMediaRegister(s.mediaRegister)
	}
	if s.browserMgr != nil {
		reg.SetBrowserManager(s.browserMgr)
	}
}

// attachTriggerRuntime 为工具 Registry 注入触发器 store；targetAgentID 为空时用 node_id。
func attachTriggerRuntime(reg *tools.Registry, store *triggers.Store, sched *triggers.Scheduler, targetAgentID string) {
	if reg == nil || store == nil {
		return
	}
	agentID := strings.TrimSpace(targetAgentID)
	reg.SetTriggerRuntime(store, sched, agentID)
}

// attachWeComRuntime 按 Node 配置注入企业微信 webhook 客户端。
func attachWeComRuntime(reg *tools.Registry, cfg *config.Config) {
	if reg == nil {
		return
	}
	reg.SetWeComClient(wecom.NewClientFromConfig(cfg))
}

// attachBackgroundJobNotifier 将后台 bash 完成回调挂到 Registry（默认工具表与 per-agent Registry 均需挂载）。
func attachBackgroundJobNotifier(reg *tools.Registry, mgr *session.Manager, logger *slog.Logger) {
	if reg == nil || mgr == nil {
		return
	}
	reg.SetBackgroundJobNotifier(func(sessionID string, done tools.BackgroundJobDone) {
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
			if logger != nil {
				logger.Warn("background tool completion enqueue failed", "session_id", sessionID, "error", err)
			}
		}
	})
}

// Handler 返回可用于 http.Server 的根 Handler（含 access log）。
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
	if s.workgroupDialer != nil {
		go func() {
			if err := s.workgroupDialer.ConnectAndServe(regCtx); err != nil && regCtx.Err() == nil {
				s.logger.Warn("workgroup dialer stopped", "error", err)
			}
		}()
	}
	if s.updateChecker != nil {
		s.updateChecker.Start(regCtx)
	}
	go func() {
		s.logger.Info("agent node listening", "addr", addr, "agent_id", s.cfg.NodeID)
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
		if s.workgroupDialer != nil {
			s.workgroupDialer.Close()
		}
		if s.registrar != nil {
			s.registrar.Stop(shutdownCtx)
		}
		// 与启动顺序相反：先停后台任务与会话，再关 HTTP 监听。
		if s.triggerSched != nil {
			s.triggerSched.Stop()
		}
		s.sessions.Stop()
		if s.tools != nil {
			_ = s.tools.CloseBrowser()
		}
		if s.store != nil {
			_ = s.store.Close()
		}
		if s.agents != nil {
			_ = s.agents.Close()
		}
		if s.llmConfigs != nil {
			_ = s.llmConfigs.Close()
		}
		if s.nodeSettings != nil {
			_ = s.nodeSettings.Close()
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

// Close 释放 SQLite / 会话等资源。ListenAndServe 退出路径会调用；httptest 测试需显式 Cleanup。
func (s *Server) Close() {
	if s == nil {
		return
	}
	if s.triggerSched != nil {
		s.triggerSched.Stop()
	}
	if s.sessions != nil {
		s.sessions.Stop()
	}
	if s.tools != nil {
		_ = s.tools.CloseBrowser()
	}
	if s.store != nil {
		_ = s.store.Close()
	}
	if s.agents != nil {
		_ = s.agents.Close()
	}
	if s.llmConfigs != nil {
		_ = s.llmConfigs.Close()
	}
	if s.nodeSettings != nil {
		_ = s.nodeSettings.Close()
	}
}

type healthResponse struct {
	Status  string `json:"status"`
	NodeID  string `json:"node_id"`
	Version string `json:"version"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	// 探活：Client 启动前与运维脚本使用；无鉴权。
	writeJSON(w, http.StatusOK, healthResponse{
		Status:  "ok",
		NodeID:  s.cfg.NodeID,
		Version: version.Version,
	})
}

type agentInfoResponse struct {
	NodeID            string              `json:"node_id"`
	ExposeToPeers     bool                `json:"expose_to_peers"`
	Capabilities      []string            `json:"capabilities"`
	MultimodalEnabled bool                `json:"multimodal_enabled"`
	ManageRegistered  bool                `json:"manage_registered"`
	LLM               llm.LLMSettingsView `json:"llm"`
	Compression       compressionInfo     `json:"compression"`
}

type compressionInfo struct {
	SilentTriggerTokens   int `json:"silent_trigger_tokens"`
	BlockingTriggerTokens int `json:"blocking_trigger_tokens"`
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
	comp := compressionInfo{}
	if s.cfg != nil {
		comp.SilentTriggerTokens = s.cfg.Compression.SilentTriggerTokens
		comp.BlockingTriggerTokens = s.cfg.Compression.BlockingTriggerTokens
	}
	writeJSON(w, http.StatusOK, agentInfoResponse{
		NodeID:            s.cfg.NodeID,
		ExposeToPeers:     false,
		Capabilities:      s.cfg.Capabilities(),
		MultimodalEnabled: s.cfg.MultimodalEnabled(),
		ManageRegistered:  registered,
		LLM:               llmView,
		Compression:       comp,
	})
}

func (s *Server) handleAgentUpdate(w http.ResponseWriter, _ *http.Request) {
	if manage.UpdateDelegatedToShell() {
		channel := "stable"
		if s.cfg != nil {
			channel = strings.TrimSpace(s.cfg.Manage.Update.Channel)
		}
		writeJSON(w, http.StatusOK, manage.ShellDelegateUpdateStatus(channel))
		return
	}
	if s.updateChecker == nil {
		writeJSON(w, http.StatusOK, manage.UpdateStatus{
			CurrentVersion:  version.Version,
			LatestVersion:   version.Version,
			ManageReachable: false,
			Platform:        manage.ReleasePlatform(),
			Channel:         "stable",
			ApplyCommand:    "dagents update",
			Message:         "Manage 未启用，无法检查更新",
		})
		return
	}
	writeJSON(w, http.StatusOK, s.updateChecker.Snapshot())
}

func (s *Server) handleAgentUpgradeReadiness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.sessions.UpgradeReadiness())
}

type clearContextResponse struct {
	AgentID       string `json:"agent_id"`
	Cleared       bool   `json:"cleared"`
	CancelledTurn bool   `json:"cancelled_turn"`
}

func (s *Server) handleAgentClearContextImpl(w http.ResponseWriter, r *http.Request) {
	// POST clear-context：清空 messages；在途 turn 会先 cancel。
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	cancelled, err := s.sessions.ClearContext(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, clearContextResponse{
		AgentID:       sessionID,
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
	AgentID               string                  `json:"agent_id"`
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
	SkillsCatalogEstimatedTokens        int                     `json:"skills_catalog_estimated_tokens"`
	SkillsCatalogMaxBodyEstimatedTokens int                     `json:"skills_catalog_max_body_estimated_tokens"`
	SkillsCatalogBloatThreshold         int                     `json:"skills_catalog_bloat_threshold"`
	LoadedSkills                   []skills.LoadedSkill    `json:"loaded_skills"`
	RecentMessages                 []contextMessagePreview `json:"recent_messages"`
	Messages                       *[]contextMessagePreview `json:"messages,omitempty"`
	LastCompression                *compression.LastCompressionSnapshot `json:"last_compression,omitempty"`
}

func buildContextMessagePreviews(messages []llm.Message, maxRunes int) []contextMessagePreview {
	out := make([]contextMessagePreview, 0, len(messages))
	for _, m := range messages {
		content := truncateContextPreview(llm.MessageTextSummary(m), maxRunes)
		out = append(out, contextMessagePreview{
			Role:                m.Role,
			Content:             content,
			ToolCallID:          m.ToolCallID,
			ToolCallsCount:      len(m.ToolCalls),
			HasReasoningContent: strings.TrimSpace(m.ReasoningContent) != "",
		})
	}
	return out
}

func queryBoolParam(r *http.Request, key string) bool {
	v := strings.TrimSpace(r.URL.Query().Get(key))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func (s *Server) handleAgentContextImpl(w http.ResponseWriter, r *http.Request) {
	// GET context：只读快照；默认 recent_messages 最多 10 条；full_messages=1 返回完整 messages 列表。
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	view, err := s.sessions.GetContextView(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	const previewLimit = 10
	const contextMessagePreviewRunes = 8000
	start := 0
	if len(view.Messages) > previewLimit {
		start = len(view.Messages) - previewLimit
	}
	recent := buildContextMessagePreviews(view.Messages[start:], contextMessagePreviewRunes)
	resp := sessionContextResponse{
		AgentID:               view.SessionID,
		MessagesCount:         view.MessagesCount,
		PendingToolCallsCount: view.PendingToolCallsCount,
		MessagesTotalTokens:   view.MessagesTotalTokens,
		ToolLoopCount:         view.ToolLoopCount,
		QueuePending:          view.QueuePending,
		HasActiveTurn:         view.HasActiveTurn,
		SystemPrompt:                 view.SystemPrompt,
		SystemPromptEstimatedTokens:  view.SystemPromptEstimatedTokens,
		SkillsCatalogEstimatedTokens:        view.SkillsCatalogEstimatedTokens,
		SkillsCatalogMaxBodyEstimatedTokens: view.SkillsCatalogMaxBodyEstimatedTokens,
		SkillsCatalogBloatThreshold:         view.SkillsCatalogBloatThreshold,
		LoadedSkills:                 view.LoadedSkills,
		RecentMessages:               recent,
		LastCompression:              view.LastCompression,
		RunTurnPhase:                 turn.RunTurnPhase(view.TurnState),
	}
	if queryBoolParam(r, "full_messages") {
		msgs := buildContextMessagePreviews(view.Messages, contextMessagePreviewRunes)
		resp.Messages = &msgs
	}
	if view.TurnState != "" {
		resp.TurnState = string(view.TurnState)
	}
	if resp.LoadedSkills == nil {
		resp.LoadedSkills = []skills.LoadedSkill{}
	}
	writeJSON(w, http.StatusOK, resp)
}

type sessionHydrateResponse struct {
	AgentID       string                    `json:"agent_id"`
	RunTurnPhase  string                    `json:"run_turn_phase"`
	HasActiveTurn bool                      `json:"has_active_turn"`
	QueuePending  int                       `json:"queue_pending"`
	Transcript    []session.TranscriptEntry `json:"transcript"`
	PendingHITL   map[string]any            `json:"pending_hitl"`
	SSESeqHint    int                       `json:"sse_seq_hint"`
	NotifySeq     int                       `json:"notify_seq"`
	AckSeq        int                       `json:"ack_seq"`
	HasUnread     bool                      `json:"has_unread"`
	ToolJobs      map[string]int            `json:"tool_jobs,omitempty"`
}

func (s *Server) handleAgentHydrateImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	view, err := s.sessions.GetHydrateView(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	transcript := view.Transcript
	if transcript == nil {
		transcript = []session.TranscriptEntry{}
	}
	runPhase := view.RunTurnPhase
	toolJobs := map[string]int{"running": 0, "background": 0}
	if reg := s.sessions.SessionTools(sessionID); reg != nil {
		c := reg.SessionToolJobCounts(sessionID)
		toolJobs["running"] = c.Running
		toolJobs["background"] = c.Background
	}
	writeJSON(w, http.StatusOK, sessionHydrateResponse{
		AgentID:       view.SessionID,
		RunTurnPhase:  runPhase,
		HasActiveTurn: view.HasActiveTurn,
		QueuePending:  view.QueuePending,
		Transcript:    transcript,
		PendingHITL:   view.PendingHITL,
		SSESeqHint:    s.stream.CurrentSeq(),
		NotifySeq:     view.NotifySeq,
		AckSeq:        view.AckSeq,
		HasUnread:     view.HasUnread,
		ToolJobs:      toolJobs,
	})
}

type sessionAckRequest struct {
	SSESeq int `json:"sse_seq"`
}

type sessionAckResponse struct {
	AgentID   string `json:"agent_id"`
	NotifySeq int    `json:"notify_seq"`
	AckSeq    int    `json:"ack_seq"`
	HasUnread bool   `json:"has_unread"`
}

func (s *Server) handleAgentAckImpl(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	var req sessionAckRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if req.SSESeq <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "sse_seq must be positive", nil)
		return
	}
	state, err := s.sessions.AckSession(r.Context(), sessionID, req.SSESeq)
	if err != nil {
		switch err.Error() {
		case "agent_not_found":
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		case "agent_id is required", "sse_seq must be positive":
			writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		default:
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, sessionAckResponse{
		AgentID:   sessionID,
		NotifySeq: state.NotifySeq,
		AckSeq:    state.AckSeq,
		HasUnread: state.HasUnread,
	})
}

func (s *Server) handleAgentCompressImpl(w http.ResponseWriter, r *http.Request) {
	// POST compress：手动触发一次阻塞压缩（忽略 token 阈值）。
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	result, err := s.sessions.CompressContext(r.Context(), sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	if result.Status == "busy" {
		writeAPIError(w, http.StatusConflict, "turn_busy", "当前 turn 进行中，请稍后再试", map[string]any{
			"agent_id": sessionID,
			"status":   result.Status,
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type postMessageRequest struct {
	AgentID         string            `json:"agent_id"`
	RequestType     string            `json:"request_type"`
	Content         string            `json:"content"`
	ContentParts    []llm.ContentPart `json:"content_parts,omitempty"`
	UserMessageName string            `json:"user_message_name,omitempty"`
	ResumeValue     map[string]any    `json:"resume_value"`
}

type postMessageResponse struct {
	Accepted bool   `json:"accepted"`
	AgentID  string `json:"agent_id"`
	Priority string `json:"priority"`
}

func resolveAgentID(agentID string) (string, error) {
	aid := strings.TrimSpace(agentID)
	if aid == "" {
		return "", fmt.Errorf("agent_id is required")
	}
	return aid, nil
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	// POST /v1/messages：message 入队 human 优先级；resume 用于 HITL 续跑。仅接受 agent_id。
	var req postMessageRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	sessionID, err := resolveAgentID(req.AgentID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", err.Error(), nil)
		return
	}
	// 若该 id 是 Agent 实例，先按快照装入 runtime（避免重启后落到默认沙箱配置）。
	if s.agents != nil {
		if rec, getErr := s.agents.Get(r.Context(), sessionID); getErr == nil && rec != nil && !rec.Archived {
			if store.NormalizeAgentOrigin(rec.Origin) == store.AgentOriginRemote {
				writeRemotePlacementDeprecated(w, sessionID)
				return
			}
			if err := s.ensureAgentRuntime(r.Context(), sessionID); err != nil {
				writeAPIError(w, http.StatusInternalServerError, "agent_ensure_failed", err.Error(), map[string]any{"agent_id": sessionID})
				return
			}
		}
	}
	requestType := strings.TrimSpace(req.RequestType)
	if requestType == "" {
		requestType = "message"
	}

	priority, err := s.sessions.EnqueueMessage(r.Context(), sessionID, requestType, req.Content, req.ContentParts, req.ResumeValue, req.UserMessageName)
	if err != nil {
		switch err.Error() {
		case "agent_not_found":
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		case "invalid_message":
			writeAPIError(w, http.StatusBadRequest, "invalid_message", "content 不能为空", nil)
		case "multimodal_disabled":
			writeAPIError(w, http.StatusBadRequest, "multimodal_disabled", "多模态未启用（config multimodal.enabled）", nil)
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
		Accepted: true,
		AgentID:  sessionID,
		Priority: priority,
	})
}

type cancelTurnResponse struct {
	AgentID   string `json:"agent_id"`
	Cancelled bool   `json:"cancelled"`
}

func (s *Server) handleAgentCancelImpl(w http.ResponseWriter, r *http.Request) {
	// POST cancel：取消在途 turn；无在途任务时 cancelled=false。
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	if sessionID == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_agent", "agent_id is required", nil)
		return
	}
	if s.sessions.Get(sessionID) == nil {
		writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		return
	}
	cancelled := s.sessions.CancelTurn(sessionID)
	writeJSON(w, http.StatusOK, cancelTurnResponse{
		AgentID:   sessionID,
		Cancelled: cancelled,
	})
}

func (s *Server) handleStreams(w http.ResponseWriter, r *http.Request) {
	// GET /v1/streams：SSE 长连接；Client 用 agent_id 查询参数过滤事件。
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "streaming not supported", nil)
		return
	}

	agentFilter := strings.TrimSpace(r.URL.Query().Get("agent_id"))
	// 远端引用若未走 Edge upgrade，禁止订阅本机 hub（否则永远无事件且误导）。
	if agentFilter != "" && s.agents != nil {
		if rec, err := s.agents.Get(r.Context(), agentFilter); err == nil && rec != nil && !rec.Archived {
			if store.NormalizeAgentOrigin(rec.Origin) == store.AgentOriginRemote {
				writeRemotePlacementDeprecated(w, agentFilter)
				return
			}
		}
	}
	lastSeq := parseLastEventID(r.Header.Get("Last-Event-ID"))
	live := strings.TrimSpace(r.URL.Query().Get("live")) == "1"
	// live=1：TUI 重连时只收增量，避免 replay 历史 done 干扰 wait_user_turn。
	if live {
		lastSeq = s.stream.CurrentSeq()
	}
	s.logger.Info("sse subscribe",
		"agent_id", agentFilter,
		"live", live,
		"after_seq", lastSeq,
		"remote", r.RemoteAddr,
	)
	defer s.logger.Debug("sse unsubscribe", "agent_id", agentFilter, "remote", r.RemoteAddr)

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
			if agentFilter != "" && ev.AgentID != agentFilter && ev.SessionID != agentFilter {
				continue // Hub 为全局广播；按 agent 过滤在 handler 内完成
			}
			if _, err := w.Write([]byte(ev.FormatSSE())); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleAgentListSkillsImpl(w http.ResponseWriter, r *http.Request) {
	// GET skills：返回 session 已加载与磁盘可用 skill 元数据。
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	loaded, available, err := s.sessions.ListSessionSkills(sessionID)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":         sessionID,
		"loaded_skills":    loaded,
		"available_skills": available,
	})
}

// handleNodeSkillsCatalog 返回 Node 级 skills 目录（不受 Agent 可见性白名单过滤），供创建/编辑 Agent 勾选。
func (s *Server) handleNodeSkillsCatalog(w http.ResponseWriter, r *http.Request) {
	if s.cfg == nil || !s.cfg.Skills.Enabled {
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled":          false,
			"available_skills": []skills.LoadedSkill{},
		})
		return
	}
	catalog := skills.NewCatalog(s.cfg.SkillsRoot(), true, s.cfg.Skills.MaxInPrompt)
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":          true,
		"skills_root":      catalog.Root(),
		"available_skills": catalog.ListMetadata(),
	})
}

type skillNameRequest struct {
	SkillName string `json:"skill_name"`
}

func (s *Server) handleAgentLoadSkillImpl(w http.ResponseWriter, r *http.Request) {
	// POST skills/load：与 load_skills 工具语义一致，供 Client 设置页调用。
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	var req skillNameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	loaded, err := s.sessions.LoadSessionSkill(sessionID, req.SkillName)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":      sessionID,
		"loaded_skills": loaded,
	})
}

func (s *Server) handleAgentUnloadSkillImpl(w http.ResponseWriter, r *http.Request) {
	// POST skills/unload：从 session 移除指定 skill。
	sessionID := strings.TrimSpace(r.PathValue("agent_id"))
	var req skillNameRequest
	if err := decodeJSON(r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		return
	}
	loaded, err := s.sessions.UnloadSessionSkill(sessionID, req.SkillName)
	if err != nil {
		if err.Error() == "agent_not_found" {
			writeAPIError(w, http.StatusNotFound, "agent_not_found", "agent 不存在", map[string]any{"agent_id": sessionID})
		} else {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agent_id":      sessionID,
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
