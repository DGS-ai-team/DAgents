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

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
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
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// Server 承载 Agent Node HTTP 路由与运行时依赖。
type Server struct {
	cfg             *config.Config
	configPath      string
	llmRuntime      *llm.RuntimeSettings
	logger          *slog.Logger
	mux             *http.ServeMux
	sessions        *session.Manager // per-session 队列与 turn consumer（过渡期与 agent_id 1:1）
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
	registrar       *manage.Registrar
	updateChecker   *manage.UpdateChecker
	packageUploader *manage.PackageUploader
	control         *manage.ControlClient
	tools           *tools.Registry
	transfers       *tools.LinuxTransferManager
	backgroundJobs  *tools.BackgroundJobStore
	browserMgr      *browser.Manager
	mediaRegister   tools.MediaRegisterFunc
	workgroupWorker *workgroup.Worker
	workgroupDialer *workgroup.Dialer
	workgroupAgents *workgroupAgentBridge
	terminals       *terminalSessionRegistry

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
		reg.SetMultimodalEnabled(cfg.MultimodalEnabled())
		reg.SetBashCompress(toolsBashCompressFromConfig(cfg.Tools))
		o.tools = reg
	}
	var backgroundJobs *tools.BackgroundJobStore
	if !o.skipStore {
		opened, err := tools.OpenBackgroundJobStore(cfg.BackgroundJobsDBPath())
		if err != nil {
			logger.Error("background job store init failed", "error", err, "path", cfg.BackgroundJobsDBPath())
		} else {
			backgroundJobs = opened
			if err := o.tools.WithBackgroundJobStore(backgroundJobs); err != nil {
				logger.Error("default tools background job store bind failed", "error", err)
			}
		}
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
	var mcpServerStore *store.MCPServerStore
	mcpManager := mcp.NewManager(logger)
	if !o.skipStore {
		opened, err := store.OpenAgents(cfg.AgentsDBPath())
		if err != nil {
			logger.Error("agents store init failed", "error", err, "path", cfg.AgentsDBPath())
		} else {
			agentsStore = opened
			if n, err := policy.MergeMissingSeedIntoRuntimePolicy(cfg.RuntimeDir()); err != nil {
				logger.Error("runtime policy seed merge failed", "error", err, "runtime", cfg.RuntimeDir())
			} else if n > 0 {
				logger.Info("runtime policy seed merge applied", "tools_added", n, "runtime", cfg.RuntimeDir())
			}
			if result, err := agentsStore.MigrateAgentPoliciesMergeSeed(context.Background()); err != nil {
				logger.Error("agent policy seed merge failed", "error", err)
			} else if result.AgentsTouched > 0 {
				logger.Info("agent policy seed merge applied",
					"agents", result.AgentsTouched, "tools_added", result.ToolsAdded)
			}
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
		transferManager = tools.NewLinuxTransferManager(linuxProvider, cfg.FSRoot, tools.DefaultLinuxTransferConcurrency,
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
	injectTodayDateEnabled := cfg.InjectTodayDateHookEnabled()
	// session.Manager 持有 per-session consumer；Publish 的事件经 Hub 广播给 SSE 订阅者。
	mgr := session.NewManager(cfg.NodeID, hub, o.llmClient, o.tools, o.policyEngine, st, session.TurnOptions{
		FSRoot: cfg.FSRoot,
		// MaxToolLoops 由各 Agent config_snapshot（defaults.llm.max_tool_loops）在装入 runtime 时写入。
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
		PluginHooks:     hooks.PluginsConfigFromShared(cfg.Hooks, cfg.RuntimeDir()),
		HookHost: turn.HookHostConfig{
			MaxLLMCalls:   cfg.HooksHostMaxLLMCalls(),
			HistoryWindow: cfg.HooksHostHistoryWindow(),
			RuntimeDir:    cfg.RuntimeDir(),
			SkillsRoot:    cfg.SkillsRoot(),
		},
		MultimodalEnabled: cfg.MultimodalEnabled(),
	}, logger)
	childMgr := childagent.NewManager(childagent.Config{
		Enabled:                   true,
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
		if triggerSched != nil {
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
		if !manage.UpdateDelegatedToShell() {
			updateChecker = manage.NewUpdateChecker(cfg, logger)
		}
		packageUploader = manage.NewPackageUploader(cfg, logger)
	}
	control := manage.NewControlClient(cfg)
	var wgWorker *workgroup.Worker
	var wgDialer *workgroup.Dialer
	var wgAgentBridge *workgroupAgentBridge
	if cfg.ManageWorkgroupEnabled() {
		wgAgentBridge = newWorkgroupAgentBridge(nil)
		toolNames := []string{}
		if mgr != nil {
			toolNames = mgr.ToolNames()
		}
		wgWorker = workgroup.NewWorker(workgroup.Config{
			NodeID:             cfg.NodeID,
			AgentSessions:      wgAgentBridge,
			NodeToolNames:      toolNames,
			DataDir:            filepath.Join(cfg.RuntimeDir(), "workgroup-workers", "state"),
			BackgroundJobStore: backgroundJobs,
		})
		wgDialer = &workgroup.Dialer{
			ManageURL: cfg.Manage.URL,
			NodeID:    cfg.NodeID,
			Worker:    wgWorker,
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
		registrar:            registrar,
		updateChecker:        updateChecker,
		packageUploader:      packageUploader,
		control:              control,
		tools:                o.tools,
		transfers:            transferManager,
		backgroundJobs:       backgroundJobs,
		browserMgr:           browserMgr,
		mediaRegister:        mediaRegister,
		workgroupWorker:      wgWorker,
		workgroupDialer:      wgDialer,
		workgroupAgents:      wgAgentBridge,
		terminals:            newTerminalSessionRegistry(),
		pendingRuntimeReload: make(map[string]string),
	}
	if s.workgroupAgents != nil {
		s.workgroupAgents.server = s
	}
	s.terminals.setOpener(func(ctx context.Context, agentID string, req tools.TerminalRequest) (tools.Terminal, error) {
		registry, err := s.terminalToolsRegistry(agentID)
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
	if client := wecom.NewClientFromConfig(cfg); client != nil {
		logger.Info("wecom webhook tools enabled")
	}
	hub.SetEventListener(func(ev stream.Event) {
		mgr.OnStreamEvent(ev)
	})
	s.registerRoutes()
	return s
}
