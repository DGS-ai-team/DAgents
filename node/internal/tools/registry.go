package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
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
	terminalConfigResolver TerminalConfigResolver
	processEventSink       ProcessEventSink
	terminalBroker         TerminalSessionBroker
	triggerStore           *triggers.Store
	triggerSched           *triggers.Scheduler
	agentID                string
	skillsCatalogHolder
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

// WithLinuxShellProvider enables the separately named linux_exec tool. It is
// kept separate from bash_run so a Linux channel can never be selected by
// accidentally changing a local bash target.
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

// SetTerminalConfigResolver binds the Agent-scoped terminal config view. The
// resolver is consulted both when listing configs and when opening one, so a
// model cannot bypass the Agent's binding by guessing a channel ID.
func (r *Registry) SetTerminalConfigResolver(resolver TerminalConfigResolver) {
	if r == nil {
		return
	}
	r.terminalConfigResolver = resolver
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
		base = append(base, linuxExecToolDef()...)
	}
	base = append(base, r.mcpToolDefs()...)
	return r.enrichDefinitions(r.filterToolDefs(base))
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
	r.handlers["linux_exec"] = r.execLinuxExec
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
