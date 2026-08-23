package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
	"github.com/DGS-ai-team/DAgents/node/internal/wecom"
)

// Registry 注册内置工具并在 FS_ROOT 内执行。
type Registry struct {
	fsRoot                 string
	bashTimeout            int
	bashHardLimitSec       int // 未传 timeout_seconds 时的硬上限（超时杀进程，不转后台）
	shellOutputEncoding    string
	fileEncoding           string
	bashCompress           BashCompressConfig
	compressMu             sync.Mutex
	bashCompressStats      map[string]*OutputCompressStats
	visionMu               sync.Mutex
	readImageVision        map[string]*ReadImageVisionPayload
	bgJobs                 *backgroundJobRegistry
	syncShells             *syncShellTracker
	shellProvider          ShellProvider
	localTerminalProvider  TerminalProvider
	linuxProvider          *LinuxShellProvider
	linuxTransferManager   *LinuxTransferManager
	legacyLinuxTools       bool
	terminalConfigResolver TerminalConfigResolver
	processEventSink       ProcessEventSink
	terminalBroker         TerminalSessionBroker
	triggerStore           *triggers.Store
	triggerSched           *triggers.Scheduler
	agentID                string
	enabledOnly            map[string]struct{}
	multimodalEnabled      bool
	browser                *browser.Manager
	browserCompanionExists BrowserCompanionExistsFunc
	wecom                  *wecom.Client
	handlers               map[string]handler
	pathEncMu              sync.Mutex
	pathEncCache           map[string]pathEncodingEntry
	mediaMu                sync.Mutex
	mediaRegister          MediaRegisterFunc
	toolResultMedia        map[string][]map[string]any
	mcpTools               map[string]MCPTool
}

// WithBackgroundJobStore binds a persistent job store to a Registry. It is
// intended for Node runtime construction; tests and embedded callers may omit
// it to keep jobs in memory only.
func (r *Registry) WithBackgroundJobStore(st *BackgroundJobStore) error {
	return r.withBackgroundJobStore(st, "")
}

// WithShellProvider replaces the execution backend used by shell tools. It
// is intentionally an internal seam for the current Local provider and
// future SSH/container/exec-server providers; tool policy remains in the
// Registry and is not bypassed by changing the provider.
func (r *Registry) WithShellProvider(provider ShellProvider) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	if provider == nil {
		return fmt.Errorf("shell provider is nil")
	}
	r.shellProvider = provider
	if terminalProvider, ok := provider.(TerminalProvider); ok {
		r.localTerminalProvider = terminalProvider
	}
	return nil
}

// WithLinuxShellProvider enables Linux-channel terminal targets. Legacy
// linux_exec/file-transfer definitions are only exposed when an old Agent
// snapshot explicitly enables those names; new snapshots use terminal_*.
func (r *Registry) WithLinuxShellProvider(provider *LinuxShellProvider) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	if provider == nil {
		return fmt.Errorf("linux shell provider is nil")
	}
	r.linuxProvider = provider
	return nil
}

// WithLinuxTransferManager binds the Node-level Linux file transfer queue.
// The manager is shared by all Agent registries so one Agent cannot exhaust
// the Node's SSH/SFTP transfer capacity.
func (r *Registry) WithLinuxTransferManager(manager *LinuxTransferManager) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	if manager == nil {
		return fmt.Errorf("linux transfer manager is nil")
	}
	r.linuxTransferManager = manager
	return nil
}

// SetTerminalConfigResolver binds the Agent-scoped terminal config view. The
// resolver is consulted both when listing configs and when opening one, so a
// model cannot bypass the Agent's binding by guessing a channel ID.
func (r *Registry) SetTerminalConfigResolver(resolver TerminalConfigResolver) {
	if r == nil {
		return
	}
	r.terminalConfigResolver = resolver
}

// resolveLinuxChannelID accepts the model-facing terminal config ID and the
// legacy raw channel ID. New tools should pass the prefixed config ID returned
// by terminal_config_list so the Agent binding is checked before execution.
// Raw IDs remain accepted for compatibility, while the Linux provider still
// performs its own live channel and binding checks immediately before use.
func (r *Registry) resolveLinuxChannelID(ctx context.Context, requested string) (string, error) {
	id := strings.TrimSpace(requested)
	if id == "" {
		return "", fmt.Errorf("config_id is required; call terminal_config_list first")
	}
	if !strings.HasPrefix(id, TerminalConfigLinuxPrefix) {
		return id, nil
	}
	if r == nil || r.terminalConfigResolver == nil {
		return "", fmt.Errorf("terminal config resolver is unavailable")
	}
	config, err := r.resolveTerminalConfig(ctx, id)
	if err != nil {
		return "", err
	}
	if config.TargetKind != executionTargetLinuxChannel {
		return "", fmt.Errorf("terminal config %q is not a Linux channel config", id)
	}
	channelID := strings.TrimSpace(config.TargetID)
	if channelID == "" {
		return "", fmt.Errorf("terminal config %q has no Linux channel target", id)
	}
	return channelID, nil
}

func resolveLinuxToolID(configID, legacyChannelID string) (string, error) {
	configID = strings.TrimSpace(configID)
	legacyChannelID = strings.TrimSpace(legacyChannelID)
	if configID != "" && legacyChannelID != "" && configID != legacyChannelID {
		return "", fmt.Errorf("config_id and channel_id refer to different targets")
	}
	if configID != "" {
		return configID, nil
	}
	if legacyChannelID != "" {
		return legacyChannelID, nil
	}
	return "", fmt.Errorf("config_id is required; call terminal_config_list first")
}

func toolArgString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	value, ok := args[key]
	if !ok || value == nil {
		return ""
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "<nil>" {
		return ""
	}
	return text
}

// OpenTerminal routes local and Linux-channel terminals through the same
// registry that owns model-facing tools. The registry fills in the owning
// agent and runtime event sink so a terminal API cannot accidentally bypass
// per-agent channel binding or execution observability.
func (r *Registry) OpenTerminal(ctx context.Context, req TerminalRequest) (Terminal, error) {
	if r == nil {
		return nil, fmt.Errorf("terminal registry is unavailable")
	}
	if strings.TrimSpace(req.Context.AgentID) == "" {
		req.Context.AgentID = r.agentID
	}
	if req.EventSink == nil {
		req.EventSink = r.processEventSink
	}
	switch req.Target.Kind {
	case "", executionTargetLocal:
		if r.localTerminalProvider == nil {
			return nil, fmt.Errorf("local terminal provider is unavailable")
		}
		return r.localTerminalProvider.OpenTerminal(ctx, req)
	case executionTargetLinuxChannel:
		if r.linuxProvider == nil {
			return nil, fmt.Errorf("linux terminal provider is unavailable")
		}
		return r.linuxProvider.OpenTerminal(ctx, req)
	default:
		return nil, fmt.Errorf("unsupported terminal target %q", req.Target.Kind)
	}
}

// SetProcessEventSink attaches the runtime execution-event bridge. The sink
// must be non-blocking because stdout/stderr collection calls it on the
// process IO path.
func (r *Registry) SetProcessEventSink(sink ProcessEventSink) {
	if r == nil {
		return
	}
	r.processEventSink = sink
}

// SetTerminalSessionBroker binds the API-owned long-lived terminal registry.
// Keeping the interface in tools avoids importing the HTTP layer into Agent
// execution while allowing tools and WebSocket clients to share sessions.
func (r *Registry) SetTerminalSessionBroker(broker TerminalSessionBroker) {
	if r == nil {
		return
	}
	r.terminalBroker = broker
}

// SetAgentID attaches the owning Agent identity to execution requests and
// lifecycle events. It is also used by per-agent channel binding checks.
func (r *Registry) SetAgentID(agentID string) {
	if r == nil {
		return
	}
	r.agentID = strings.TrimSpace(agentID)
}

// PreflightTool evaluates live provider policy before the orchestrator queues
// a tool. The provider still repeats the check immediately before connecting.
func (r *Registry) PreflightTool(ctx context.Context, name string, args map[string]any) (ToolPreflightDecision, bool) {
	if r == nil || r.linuxProvider == nil {
		return ToolPreflightDecision{}, false
	}
	name = strings.TrimSpace(name)
	configID := toolArgString(args, "config_id")
	legacyChannelID := toolArgString(args, "channel_id")
	terminalID := toolArgString(args, "terminal_id")
	requestedID := ""
	command := ""
	switch name {
	case "linux_exec":
		command = toolArgString(args, "command")
		var err error
		requestedID, err = resolveLinuxToolID(configID, legacyChannelID)
		if err != nil || command == "" {
			return ToolPreflightDecision{}, false
		}
	case "linux_file_upload", "linux_file_download":
		var err error
		requestedID, err = resolveLinuxToolID(configID, legacyChannelID)
		if err != nil {
			return ToolPreflightDecision{}, false
		}
	case "terminal_command", "terminal_upload", "terminal_download":
		if terminalID == "" || r.terminalBroker == nil {
			return ToolPreflightDecision{}, false
		}
		info, err := r.terminalBroker.Lookup(r.agentID, terminalID)
		if err != nil {
			return ToolPreflightDecision{Action: policy.ActionDeny, ApprovalReason: err.Error()}, true
		}
		if info.TargetKind != executionTargetLinuxChannel || info.TargetID == "" {
			if name == "terminal_command" && info.TargetKind == executionTargetLocal {
				return ToolPreflightDecision{}, false
			}
			return ToolPreflightDecision{Action: policy.ActionDeny, ApprovalReason: "terminal session is not a Linux channel"}, true
		}
		requestedID = info.TargetID
		if name == "terminal_command" {
			command = toolArgString(args, "command")
		}
	case "terminal_open":
		if configID == "" || r.terminalConfigResolver == nil {
			return ToolPreflightDecision{}, false
		}
		config, err := r.resolveTerminalConfig(ctx, configID)
		if err != nil {
			return ToolPreflightDecision{Action: policy.ActionDeny, ApprovalReason: err.Error()}, true
		}
		if config.TargetKind != executionTargetLinuxChannel {
			return ToolPreflightDecision{}, false
		}
		requestedID = config.TargetID
	default:
		return ToolPreflightDecision{}, false
	}
	channelID, err := r.resolveLinuxChannelID(ctx, requestedID)
	if err != nil {
		return ToolPreflightDecision{Action: policy.ActionDeny, ApprovalReason: err.Error()}, true
	}
	action, reason, err := r.linuxProvider.Preflight(ctx, r.agentID, channelID, command)
	if err != nil {
		return ToolPreflightDecision{Action: policy.ActionDeny, ApprovalReason: err.Error()}, true
	}
	return ToolPreflightDecision{Action: action, ApprovalReason: reason}, true
}

// WithBackgroundJobStoreForSession restores only jobs belonging to one
// agent/session runtime, preventing per-agent registries from leaking job
// metadata across agents.
func (r *Registry) WithBackgroundJobStoreForSession(st *BackgroundJobStore, sessionID string) error {
	return r.withBackgroundJobStore(st, sessionID)
}

func (r *Registry) withBackgroundJobStore(st *BackgroundJobStore, sessionID string) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	jobs, err := newBackgroundJobRegistryWithStore(st, sessionID)
	if err != nil {
		return err
	}
	// Rebinding can happen while an Agent runtime is being rebuilt. Preserve
	// in-process jobs (and their cancellation handles) instead of replacing
	// them with only the rows loaded from SQLite.
	if current := r.bgJobs; current != nil {
		current.mu.RLock()
		for id, job := range current.jobs {
			if job == nil {
				continue
			}
			job.mu.Lock()
			jobSessionID := strings.TrimSpace(job.sessionID)
			job.mu.Unlock()
			if sessionID != "" && jobSessionID != "" && jobSessionID != sessionID {
				continue
			}
			if _, exists := jobs.jobs[id]; !exists {
				jobs.jobs[id] = job
				jobs.persist(job)
			}
		}
		jobs.onDone = current.onDone
		current.mu.RUnlock()
	}
	r.bgJobs = jobs
	return nil
}

// NewRegistry 创建工具表；fsRoot 为空时用当前目录。
// encodings[0]=tools.bash_output_encoding，encodings[1]=tools.file_encoding；空串表示按平台/shell 自动选择。
func NewRegistry(fsRoot string, bashTimeoutSeconds int, encodings ...string) (*Registry, error) {
	root, err := resolveFSRoot(fsRoot)
	if err != nil {
		return nil, err
	}
	if bashTimeoutSeconds <= 0 {
		bashTimeoutSeconds = 30
	}
	shellEnc := ""
	fileEnc := ""
	if len(encodings) > 0 {
		shellEnc = strings.TrimSpace(encodings[0])
	}
	if len(encodings) > 1 {
		fileEnc = strings.TrimSpace(encodings[1])
	}
	localProvider := NewLocalShellProvider()
	r := &Registry{
		fsRoot:                root,
		bashTimeout:           bashTimeoutSeconds,
		bashHardLimitSec:      maxBashTimeoutSec,
		shellOutputEncoding:   shellEnc,
		fileEncoding:          fileEnc,
		bashCompress:          DefaultBashCompressConfig(),
		bgJobs:                newBackgroundJobRegistry(),
		syncShells:            newSyncShellTracker(),
		shellProvider:         localProvider,
		localTerminalProvider: localProvider,
		handlers:              make(map[string]handler),
		mcpTools:              make(map[string]MCPTool),
	}
	r.registerBuiltins()
	return r, nil
}

// Definitions 返回传给 LLM 的 tools 列表。
func (r *Registry) Definitions() []ToolDef {
	base := []ToolDef{
		readFileToolDef(),
		showImageToolDef(),
	}
	if r.multimodalEnabled {
		base = append(base, readImageToolDef())
	}
	base = append(base,
		writeFileToolDef(),
		globFilesToolDef(),
		grepFileToolDef(),
		grepFilesToolDef(),
		searchReplaceToolDef(),
		bashRunToolDef(),
		terminalConfigListToolDef(),
		terminalOpenToolDef(),
		terminalInputToolDef(),
		terminalReadToolDef(),
		terminalTerminateToolDef(),
		terminalListToolDef(),
		terminalCommandToolDef(),
		backgroundJobStatusToolDef(),
		backgroundJobCancelToolDef(),
		askUserInformationToolDef(),
		rememberToolDef(),
		loadSkillsToolDef(),
		unloadSkillsToolDef(),
		clearSkillsToolDef(),
		triggerListToolDef(),
		triggerGetToolDef(),
		triggerCreateToolDef(),
		triggerUpdateToolDef(),
		triggerDeleteToolDef(),
	)
	if r.browserToolsEnabled() {
		base = append(base, r.browserToolDefs()...)
	}
	if defs := r.wecomToolDefs(); len(defs) > 0 {
		base = append(base, defs...)
	}
	base = append(base, childAgentToolDefs()...)
	if r.linuxProvider != nil {
		base = append(base, terminalFileTransferToolDefs()...)
		if r.legacyLinuxTools {
			base = append(base, linuxExecToolDef()...)
			base = append(base, linuxFileTransferToolDefs()...)
		}
	}
	base = append(base, r.mcpToolDefs()...)
	defs := r.filterToolDefs(base)
	for i := range defs {
		defs[i].Function.Description = strings.TrimSpace(defs[i].Function.Description) + ResultDescriptionSuffixForTool(defs[i].Function.Name)
	}
	// Keep the serialized tools prefix stable even when optional providers
	// contribute definitions in a different order. MCP itself is already
	// sorted, but normalizing the complete list also covers future providers.
	sort.SliceStable(defs, func(i, j int) bool {
		left := defs[i].Function.Name
		right := defs[j].Function.Name
		if left == right {
			return defs[i].Type < defs[j].Type
		}
		return left < right
	})
	return defs
}

// Execute 按名称 dispatch 工具；未知工具或未启用工具返回 error 文本。
// 子 Agent RestrictedRegistry 在通过自身 allowlist 后应使用 WithEnabledBypass，
// 以免父 Agent 的 enabledOnly 误拦子会话允许的工具。
func (r *Registry) Execute(ctx context.Context, name, arguments string) (string, error) {
	if err := r.rejectIfDisabled(ctx, name); err != nil {
		return "", err
	}
	h, ok := r.handlers[name]
	if !ok {
		return "", fmt.Errorf("unknown tool: %s", name)
	}
	return h(ctx, json.RawMessage(arguments))
}

// ToolRetryAllowed is deliberately an allowlist. Unknown/custom tools and
// commands that may mutate state are never retried automatically because a
// process restart or transport error cannot prove whether their side effect
// committed.
func (r *Registry) ToolRetryAllowed(name string) bool {
	switch strings.TrimSpace(name) {
	case "read_file", "read_image", "show_image", "glob_files", "grep_file", "grep_files", "search_file",
		"terminal_read", "terminal_list", "background_job_status":
		return true
	default:
		return false
	}
}

func (r *Registry) registerBuiltins() {
	r.handlers["read_file"] = r.execReadFile
	r.handlers["show_image"] = r.execShowImage
	r.handlers["read_image"] = r.execReadImage
	r.handlers["write_file"] = r.execWriteFile
	r.handlers["glob_files"] = r.execGlobFiles
	r.handlers["grep_file"] = r.execGrepFile
	r.handlers["grep_files"] = r.execGrepFiles
	r.handlers["search_file"] = r.execSearchFile
	r.handlers["search_replace"] = r.execSearchReplace
	r.handlers["bash_run"] = r.execBashRun
	r.handlers["terminal_config_list"] = r.execTerminalConfigList
	r.handlers["terminal_open"] = r.execTerminalOpen
	r.handlers["terminal_input"] = r.execTerminalInput
	r.handlers["terminal_read"] = r.execTerminalRead
	r.handlers["terminal_terminate"] = r.execTerminalTerminate
	r.handlers["terminal_list"] = r.execTerminalList
	r.handlers["terminal_command"] = r.execTerminalCommand
	r.handlers["linux_exec"] = r.execLinuxExec
	r.handlers["linux_file_upload"] = r.execLinuxFileUpload
	r.handlers["linux_file_download"] = r.execLinuxFileDownload
	r.handlers["terminal_upload"] = r.execTerminalUpload
	r.handlers["terminal_download"] = r.execTerminalDownload
	r.handlers["background_job_status"] = r.execBackgroundJobStatus
	r.handlers["background_job_cancel"] = r.execBackgroundJobCancel
	r.handlers["ask_user_information"] = func(context.Context, json.RawMessage) (string, error) {
		return "", fmt.Errorf("ask_user_information must be handled by orchestrator")
	}
	r.handlers["remember"] = func(context.Context, json.RawMessage) (string, error) {
		return "", fmt.Errorf("remember must be handled by orchestrator")
	}
	for _, name := range []string{"load_skills", "unload_skills", "clear_skills"} {
		n := name
		r.handlers[n] = func(context.Context, json.RawMessage) (string, error) {
			return "", fmt.Errorf("%s must be handled by orchestrator", n)
		}
	}
	r.handlers["trigger_list"] = r.execTriggerList
	r.handlers["trigger_get"] = r.execTriggerGet
	r.handlers["trigger_create"] = r.execTriggerCreate
	r.handlers["trigger_update"] = r.execTriggerUpdate
	r.handlers["trigger_delete"] = r.execTriggerDelete
	r.RegisterChildAgentToolStubs()
}
