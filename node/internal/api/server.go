// Package api 提供 Agent Node 对本地 Client 暴露的 HTTP/SSE 端点。
//
// 职责边界：路由解析、请求/响应 JSON、错误码映射；session 队列与 turn 执行委托 session.Manager。
package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/autonomy"
	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/desktopbridge"
	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/manage"
	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
	"github.com/DGS-ai-team/DAgents/node/internal/media"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/session"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
	"github.com/DGS-ai-team/DAgents/node/internal/wecom"
	"github.com/DGS-ai-team/DAgents/node/internal/workgroup"
	"github.com/DGS-ai-team/DAgents/node/internal/workspacecoord"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func conditionCompletionCallback(scheduler *triggers.Scheduler, triggerStore *triggers.Store) func(triggers.ConditionRequest, triggers.ConditionResult) error {
	return func(req triggers.ConditionRequest, result triggers.ConditionResult) error {
		_, err := scheduler.CompleteCondition(context.Background(), triggers.ConditionCompletion{TriggerID: req.TriggerID, DeliveryID: req.DeliveryID, SessionID: req.SessionID, AgentID: req.AgentID, Revision: req.Revision, Occurrence: req.Occurrence, Matched: result.Status == triggers.ConditionMatched})
		if err != nil {
			if recoveryErr := triggerStore.MarkConditionRecovery(req.TriggerID, req.DeliveryID, err.Error()); recoveryErr != nil {
				return fmt.Errorf("complete condition: %w; mark recovery: %v", err, recoveryErr)
			}
		}
		return err
	}
}

// Server 承载 Agent Node HTTP 路由与运行时依赖。
type Server struct {
	cfg             *config.Config
	configPath      string
	llmRuntime      *llm.RuntimeSettings
	defaultLLM      llm.Client
	llmInjected     bool
	logger          *slog.Logger
	mux             *http.ServeMux
	sessions        *session.Manager // per-session queue and turn consumer
	agents          *store.AgentStore
	mcpServers      *store.MCPServerStore
	mcpManager      *mcp.Manager
	linuxChannels   *store.LinuxChannelStore
	linuxProvider   *tools.LinuxShellProvider
	llmConfigs      *store.LLMConfigStore
	nodeSettings    *store.NodeSettingsStore
	stream          *stream.Hub // 进程内 SSE 事件总线
	transferStream  *stream.Hub // Linux 文件传输状态 SSE（与对话事件隔离）
	workgroupStream *stream.Hub // Manage 工作组 Timeline + 实时协作事件
	store           *store.SQLiteStore
	triggerStore    *triggers.Store
	triggerSched    *triggers.Scheduler
	dreamingSched   *DreamingScheduler
	startupErr      error
	autonomyStore   *autonomy.Store
	autoConfigMu    sync.Mutex
	registrar       *manage.Registrar
	updateChecker   *manage.UpdateChecker
	packageUploader *manage.PackageUploader
	control         *manage.ControlClient
	feedbackStore   *store.FeedbackStore
	feedbackRateMu  sync.Mutex
	feedbackRate    map[string][]time.Time
	tools           *tools.Registry
	workspaceCoord  *workspacecoord.Coordinator
	transfers       *tools.LinuxTransferManager
	browserMu       sync.RWMutex
	browserMgr      *browser.Manager
	mediaRegister   tools.MediaRegisterFunc
	workgroupWorker *workgroup.Worker
	workgroupDialer *workgroup.Dialer
	workgroupAgents *workgroupAgentBridge
	terminals       *terminalSessionRegistry
	desktopBridge   *desktopbridge.Client

	// manageCtx 在 ListenAndServe 内创建；首配完成前不启动 registrar / dialer。
	manageMu      sync.Mutex
	manageCtx     context.Context
	manageCancel  context.CancelFunc
	manageStarted bool

	// runtimeReloads records catalog-driven rebuilds that were deferred while
	// an Agent was in a Turn. Agent snapshot revisions cover persisted config;
	// this small queue covers external MCP/Skill catalog changes.
	runtimeReloadMu      sync.Mutex
	pendingRuntimeReload map[string]string
}

// Option 为 NewServer 可选配置。
type Option func(*serverOptions)

type serverOptions struct {
	llmClient    llm.Client
	llmInjected  bool
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
		o.llmInjected = true
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
	sharedWorkspaceCoord := workspacecoord.New()
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
	// 生产路径：Node 级工具使用 runtime root；失败时回退 "." 以免 API 完全不可用。
	if o.tools == nil {
		reg, err := tools.NewRegistry(cfg.RuntimeDir(), 30, cfg.Tools.BashOutputEncoding, cfg.Tools.FileEncoding)
		if err != nil {
			logger.Error("tools registry init failed", "error", err)
			reg, _ = tools.NewRegistry(".", 30, cfg.Tools.BashOutputEncoding, cfg.Tools.FileEncoding)
		}
		reg.SetMultimodalEnabled(cfg.MultimodalEnabled())
		reg.SetBashCompress(toolsBashCompressFromConfig(cfg.Tools))
		o.tools = reg
	}
	o.tools.SetWorkspaceCoordinator(sharedWorkspaceCoord)
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
	var mcpServerStore *store.MCPServerStore
	mcpManager := mcp.NewManager(logger)
	if !o.skipStore {
		opened, err := store.OpenAgents(cfg.AgentsDBPath())
		if err != nil {
			logger.Error("agents store init failed", "error", err, "path", cfg.AgentsDBPath())
		} else {
			agentsStore = opened
		}
		openedMCP, err := store.OpenMCPServers(filepath.Join(cfg.RuntimeDir(), "mcp_servers.db"), cfg.RuntimeDir())
		if err != nil {
			logger.Error("mcp server store init failed", "error", err)
		} else {
			mcpServerStore = openedMCP
			if configs, loadErr := mcpServerStore.List(context.Background()); loadErr != nil {
				logger.Error("mcp server config load failed", "error", loadErr)
			} else if configureErr := mcpManager.Configure(configs); configureErr != nil {
				logger.Error("mcp server manager configure failed", "error", configureErr)
			}
		}
	}
	var llmConfigStore *store.LLMConfigStore
	if !o.skipStore {
		opened, err := store.OpenLLMConfigs(cfg.LLMConfigsDBPath(), cfg.RuntimeDir())
		if err != nil {
			logger.Error("llm configs store init failed", "error", err, "path", cfg.LLMConfigsDBPath())
		} else {
			llmConfigStore = opened
			if err := store.EnsureDefaultLLMConfig(context.Background(), llmConfigStore, cfg); err != nil {
				logger.Error("llm default config initialization failed", "error", err)
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
	var linuxChannelStore *store.LinuxChannelStore
	var linuxProvider *tools.LinuxShellProvider
	if !o.skipStore {
		opened, err := store.OpenLinuxChannels(filepath.Join(cfg.RuntimeDir(), "linux_channels.db"), cfg.RuntimeDir())
		if err != nil {
			logger.Error("linux channel store init failed", "error", err)
		} else {
			linuxChannelStore = opened
			linuxProvider = tools.NewLinuxShellProvider(opened, opened.ResolveSecret).
				WithBindingResolver(opened).
				WithHostKeyResolver(tools.DefaultLinuxHostKeyResolver)
		}
	}

	hub := stream.NewHub(256, logger)
	transferHub := stream.NewHub(256, logger)
	var transferManager *tools.LinuxTransferManager
	if linuxProvider != nil {
		transferManager = tools.NewLinuxTransferManager(linuxProvider, tools.DefaultLinuxTransferConcurrency,
			func(agentID, eventType string, data map[string]any, replayable bool) {
				if replayable {
					transferHub.Publish(agentID, eventType, data)
				} else {
					transferHub.PublishEphemeral(agentID, eventType, data)
				}
			})
	}
	workgroupStream := stream.NewHub(1024, logger)
	hostsnapshot.CaptureAtStartup()
	duplicateToolCallEnabled := cfg.DuplicateToolCallHookEnabled()
	injectTodayDateEnabled := cfg.InjectTodayDateHookEnabled()
	toolResultEnabled := cfg.ToolResultHookEnabled()
	var autonomyStore *autonomy.Store
	var autonomyInitErr error
	if opened, err := autonomy.Open(filepath.Join(cfg.RuntimeDir(), "autonomy.json")); err != nil {
		logger.Warn("autonomy store init failed", "error", err)
		autonomyInitErr = err
	} else {
		autonomyStore = opened
	}
	// session.Manager 持有 per-session consumer；Publish 的事件经 Hub 广播给 SSE 订阅者。
	var triggerRoundProvider func(context.Context, string, string, string) (int, bool, error)
	mgr := session.NewManager(cfg.NodeID, hub, o.llmClient, o.tools, o.policyEngine, st, session.TurnOptions{
		TriggerToolRoundProvider: func(ctx context.Context, agentID, triggerID, deliveryID string) (int, bool, error) {
			if triggerRoundProvider == nil {
				return 0, false, nil
			}
			return triggerRoundProvider(ctx, agentID, triggerID, deliveryID)
		},
		WorkspaceRoot: cfg.RuntimeDir(),
		// MaxSteps 由各 Agent config_snapshot（defaults.llm.max_steps）在装入 runtime 时写入。
		SkillsRoot:                  cfg.SkillsRoot(),
		SkillsEnabled:               cfg.Skills.Enabled,
		SkillsMaxInPrompt:           cfg.Skills.MaxInPrompt,
		RuntimeDir:                  cfg.RuntimeDir(),
		CompressionSilent:           cfg.Compression.SilentTriggerTokens,
		CompressionBlocking:         cfg.Compression.BlockingTriggerTokens,
		IdleAutoCompressSeconds:     cfg.Compression.IdleAutoCompressSeconds,
		IdleAutoCompressPollSeconds: cfg.Compression.IdleAutoCompressPollSeconds,
		IdleAutoCompressMinTokens:   cfg.Compression.IdleAutoCompressMinTokens,
		RawMessageHistoryEnabled:    cfg.RawMessageHistoryEnabled(),
		RawMessageHistoryDir:        cfg.RawMessageHistoryDir(),
		DuplicateToolCall: hooks.DuplicateConfig{
			Enabled:       &duplicateToolCallEnabled,
			WindowSeconds: cfg.DuplicateToolCallWindowSeconds(),
		},
		ToolResult: hooks.ToolResultConfig{
			Enabled:              &toolResultEnabled,
			SpillThresholdTokens: cfg.ToolResultSpillThresholdTokens(),
			Tools:                cfg.ToolResultHookTools(),
			WorkspaceRoot:        cfg.RuntimeDir(),
		},
		InjectTodayDate: hooks.InjectTodayDateConfig{Enabled: &injectTodayDateEnabled},
		PluginHooks:     hooks.PluginsConfigFromShared(cfg.Hooks, cfg.RuntimeDir()),
		HookHost: turn.HookHostConfig{
			MaxLLMCalls:   cfg.HooksHostMaxLLMCalls(),
			HistoryWindow: cfg.HooksHostHistoryWindow(),
			RuntimeDir:    cfg.RuntimeDir(),
			SkillsRoot:    cfg.SkillsRoot(),
		},
		MultimodalEnabled:        cfg.MultimodalEnabled(),
		MemoryAutoExtract:        cfg.Memory.AutoExtract,
		MemoryCandidateQueueSize: cfg.Memory.CandidateQueueSize,
		MemoryCandidateMaxItems:  cfg.Memory.MaxCandidates,
		MemoryCoreBudgetTokens:   cfg.Memory.CoreBudgetTokens,
		AgentPromptProvider: func(_ context.Context, agentID string) (turn.AgentPromptSnapshot, error) {
			if autonomyStore == nil {
				return turn.AgentPromptSnapshot{}, nil
			}
			p, _ := autonomyStore.GetProfile(agentID)
			e, _ := autonomyStore.GetExperience(agentID)
			todos := autonomyStore.ListTodos(agentID)
			var todoText strings.Builder
			for _, todo := range todos {
				fmt.Fprintf(&todoText, "- [%s] %s (id: %s, revision: %d)\n", todo.Status, todo.Text, todo.ID, todo.Revision)
			}
			if len(todos) == 0 {
				todoText.WriteString("暂无待办（当前列表为空）")
			}
			return turn.AgentPromptSnapshot{Responsibilities: p.Responsibility, Experience: e.Content, Todo: todoText.String()}, nil
		},
	}, logger)
	childMgr := childagent.NewManager(childagent.Config{
		Enabled:            true,
		DefaultTTLSeconds:  cfg.ChildAgents.DefaultTTLSeconds,
		MaxTTLSeconds:      cfg.ChildAgents.MaxTTLSeconds,
		DefaultMaxTurns:    cfg.ChildAgents.DefaultMaxTurns,
		MaxMaxTurns:        cfg.ChildAgents.MaxMaxTurns,
		MaxActivePerParent: cfg.ChildAgents.MaxActivePerParent,
	}, hub, cfg.NodeID, logger)
	childMgr.SetRunRepository(session.NewChildRunRepository(st))
	mgr.SetChildAgentManager(childMgr)
	if cfg.IdleAutoCompressEnabled() {
		mgr.StartIdleAutoCompressScanner()
	}
	var triggerStore *triggers.Store
	var triggerSched *triggers.Scheduler
	startupErr := autonomyInitErr
	var triggerSubmitter *session.TriggerSubmitter
	if opened, err := triggers.OpenStore(cfg.TriggersStorePath(), 200); err != nil {
		logger.Warn("trigger store init failed", "error", err, "path", cfg.TriggersStorePath())
		startupErr = err
	} else {
		triggerStore = opened
		triggerStore.SetLogger(logger)
		mgr.SetConditionValidator(func(ctx context.Context, meta turn.ConditionApprovalMetadata) error {
			_ = ctx
			def, ok := triggerStore.GetTrigger(meta.TriggerID)
			if !ok {
				return fmt.Errorf("condition trigger not found")
			}
			return triggers.ValidateConditionIdentity(*def, meta.AgentID, meta.SessionID, meta.DeliveryID, meta.TriggerRevision, meta.Occurrence)
		})
		triggerSubmitter = &session.TriggerSubmitter{Mgr: mgr}
		triggerSched = triggers.NewScheduler(triggerStore, triggerSubmitter, cfg.Triggers.PollSeconds)
		triggerSched.SetLogger(logger)
		triggerSched.SetSessionResolver(mgr)
		triggerSched.SetConditionRunner(mgr.ExecuteCondition)
		mgr.SetConditionCompletionCallback(conditionCompletionCallback(triggerSched, triggerStore))
		mgr.SetTriggerDeliveryTracker(triggerStore)
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
		registrar.SetAgentCatalogProvider(func() []manage.AgentCatalogEntry {
			if agentsStore == nil {
				return nil
			}
			records, err := agentsStore.List(context.Background())
			if err != nil {
				return nil
			}
			entries := make([]manage.AgentCatalogEntry, 0, len(records))
			for _, rec := range records {
				if strings.TrimSpace(rec.AgentID) == "" || rec.Archived {
					continue
				}
				entries = append(entries, manage.AgentCatalogEntry{
					ID:          rec.AgentID,
					Name:        rec.DisplayName,
					Description: "registered local Agent",
					Metadata: map[string]any{
						"runtime_revision": rec.RuntimeRevision,
					},
				})
			}
			return entries
		})
		// On Windows Shell owns the periodic schedule and apply operation, but
		// Node still exposes an on-demand check using effective runtime config.
		// This keeps secrets and node_settings.db inside Node.
		updateChecker = manage.NewUpdateChecker(cfg, logger)
		packageUploader = manage.NewPackageUploader(cfg, logger)
	}
	control := manage.NewControlClient(cfg)
	var feedbackStore *store.FeedbackStore
	if !o.skipStore {
		opened, feedbackErr := store.OpenFeedback(store.FeedbackDBPath(cfg.RuntimeDir()))
		if feedbackErr != nil {
			logger.Error("feedback store init failed", "error", feedbackErr)
		} else {
			feedbackStore = opened
		}
	}
	var wgWorker *workgroup.Worker
	var wgDialer *workgroup.Dialer
	var wgAgentBridge *workgroupAgentBridge
	if cfg.ManageWorkgroupEnabled() {
		wgAgentBridge = newWorkgroupAgentBridge(nil)
		wgWorker = workgroup.NewWorker(workgroup.Config{
			NodeID:        cfg.NodeID,
			AgentSessions: wgAgentBridge,
		})
		manageNodeToken := cfg.Manage.NodeToken
		wgDialer = &workgroup.Dialer{
			ManageURL: cfg.Manage.URL,
			// Setup changes require a Node restart; capture the startup token so
			// a concurrent settings PATCH cannot race the reconnect loop.
			ManageTokenProvider: func() string { return manageNodeToken },
			NodeID:              cfg.NodeID,
			Worker:              wgWorker,
			ListWorkgroups: func(ctx context.Context) ([]string, error) {
				seen := map[string]struct{}{}
				ids := make([]string, 0)
				add := func(items []manage.WorkgroupListItem) {
					for _, it := range items {
						id := strings.TrimSpace(it.WorkgroupID)
						if id == "" {
							continue
						}
						if _, ok := seen[id]; ok {
							continue
						}
						seen[id] = struct{}{}
						ids = append(ids, id)
					}
				}
				sub, err1 := control.ListWorkgroups(ctx, manage.WorkgroupListSubscribed)
				if err1 == nil {
					add(sub)
				}
				acl, err2 := control.ListWorkgroups(ctx, manage.WorkgroupListACL)
				if err2 == nil {
					add(acl)
				}
				if err1 != nil && err2 != nil {
					return nil, err1
				}
				return ids, nil
			},
		}
		logger.Info("workgroup dialer enabled", "manage_url", cfg.Manage.URL)
	}
	s := &Server{
		cfg:                  cfg,
		configPath:           o.configPath,
		llmRuntime:           llmRuntime,
		defaultLLM:           o.llmClient,
		llmInjected:          o.llmInjected,
		logger:               logger,
		mux:                  http.NewServeMux(),
		stream:               hub,
		transferStream:       transferHub,
		workgroupStream:      workgroupStream,
		store:                st,
		agents:               agentsStore,
		mcpServers:           mcpServerStore,
		mcpManager:           mcpManager,
		linuxChannels:        linuxChannelStore,
		linuxProvider:        linuxProvider,
		llmConfigs:           llmConfigStore,
		nodeSettings:         o.nodeSettings,
		sessions:             mgr,
		triggerStore:         triggerStore,
		triggerSched:         triggerSched,
		startupErr:           startupErr,
		autonomyStore:        autonomyStore,
		registrar:            registrar,
		updateChecker:        updateChecker,
		packageUploader:      packageUploader,
		control:              control,
		feedbackStore:        feedbackStore,
		feedbackRate:         make(map[string][]time.Time),
		tools:                o.tools,
		workspaceCoord:       sharedWorkspaceCoord,
		transfers:            transferManager,
		browserMgr:           browserMgr,
		mediaRegister:        mediaRegister,
		workgroupWorker:      wgWorker,
		workgroupDialer:      wgDialer,
		workgroupAgents:      wgAgentBridge,
		terminals:            newTerminalSessionRegistry(),
		desktopBridge:        desktopbridge.NewFromEnv(),
		pendingRuntimeReload: make(map[string]string),
	}
	triggerRoundProvider = s.triggerToolRoundProvider
	if s.workgroupAgents != nil {
		s.workgroupAgents.server = s
	}
	if registrar != nil {
		registrar.SetAutoSummaryProvider(s.autoSummaryProvider())
	}
	s.terminals.setOpener(func(ctx context.Context, agentID string, req tools.TerminalRequest) (tools.Terminal, error) {
		registry, err := s.terminalToolsRegistry(agentID)
		if err != nil && s.workgroupAgents != nil {
			if scoped := s.workgroupAgents.registryForSession(req.Context.SessionID); scoped != nil {
				registry = scoped
				err = nil
			}
		}
		if err != nil {
			return nil, err
		}
		return registry.OpenTerminal(ctx, req)
	})
	s.terminals.setChangePublisher(func(agentID, eventType string, data map[string]any) {
		if s.stream != nil {
			s.stream.Publish(agentID, eventType, data)
		}
	})
	s.mcpManager.SetStatusListener(func(event mcp.StatusEvent) {
		if s.stream == nil {
			return
		}
		s.stream.Publish("", "mcp/status-changed", map[string]any{
			"server_id":   event.ServerID,
			"server":      event.View,
			"health":      event.Health,
			"revision":    event.Revision,
			"observed_at": event.ObservedAt,
		})
	})
	if wgWorker != nil {
		wgWorker.OnTimelineEvent = func(env workgroup.WSEnvelope) {
			wid := strings.TrimSpace(env.WorkgroupID)
			if wid == "" {
				wid, _ = env.Payload["workgroup_id"].(string)
			}
			if wid == "" {
				return
			}
			workgroupStream.Publish(wid, "workgroup.timeline", map[string]any{
				"kind":         "timeline",
				"workgroup_id": wid,
				"delivery_seq": env.DeliverySeq,
				"event":        env.Payload,
			})
		}
	}
	if wgDialer != nil {
		wgDialer.OnRealtime = func(payload map[string]any) {
			wid, _ := payload["workgroup_id"].(string)
			wid = strings.TrimSpace(wid)
			if wid == "" {
				return
			}
			workgroupStream.PublishEphemeral(wid, "workgroup.realtime", payload)
		}
	}
	// 默认工具表与后续 per-agent Registry 共用同一套 Node 运行时依赖挂载。
	s.attachNodeRuntimeDeps(s.tools, cfg.NodeID)
	if triggerSubmitter != nil {
		triggerSubmitter.EnsureAgentRuntime = func(agentID string) error {
			return s.ensureAgentRuntime(context.Background(), agentID)
		}
	}
	if client := wecom.NewClientFromConfig(cfg); client != nil {
		logger.Info("wecom webhook tools enabled")
	}
	hub.SetEventListener(func(ev stream.Event) {
		mgr.OnStreamEvent(ev)
	})
	s.registerRoutes()
	if triggerSched != nil {
		valid := map[string]bool{}
		validAuto := map[string]bool{}
		if s.agents != nil {
			if records, err := s.agents.List(context.Background()); err == nil {
				for _, rec := range records {
					if !rec.Archived {
						snap, parseErr := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
						valid[rec.AgentID] = true
						if parseErr == nil && snap.AgentType == "auto" {
							validAuto[rec.AgentID] = true
						}
					}
				}
			} else if s.startupErr == nil {
				s.startupErr = err
			}
		}
		if s.startupErr == nil && s.autonomyStore != nil && s.triggerStore != nil {
			if err := s.reconcileAutoDefaults(validAuto); err != nil {
				s.startupErr = err
			}
		}
		if s.startupErr == nil {
			if err := s.triggerStore.ValidateOwners(valid); err != nil {
				logger.Error("trigger owner validation failed", "error", err)
				triggerSched = nil
				s.triggerSched = nil
				s.startupErr = err
			} else {
				triggerSched.Start()
			}
		} else {
			triggerSched = nil
			s.triggerSched = nil
		}
	}
	if s.startupErr == nil && s.autonomyStore != nil && s.agents != nil {
		s.dreamingSched = NewDreamingScheduler(s.autonomyStore, s.agents, s.sessions, s.ensureAgentRuntime)
		s.dreamingSched.Start(context.Background())
	}
	return s
}
