package workgroup

import (
	"path/filepath"
	"sync"
)

// Worker 聚合 D2 骨架能力：provision / journal / fencing / manifest / session。
type Worker struct {
	mu               sync.Mutex
	NodeID           string
	CatalogRevision  string
	Capabilities     []string
	Bindings         BindingStore
	Journal          CommandJournal
	Session          Session
	Provision        *Provisioner
	Commands         *CommandHandler
	AgentSessions    AgentSessionHandler
	Tombstones       map[string]ArchiveTombstone
	MemberTombstones map[string]ArchiveTombstone
	// OnTimelineEvent forwards public Manage Timeline frames to the local Web UI.
	OnTimelineEvent func(WSEnvelope)

	outboundMu sync.RWMutex
	outbound   func(map[string]any) error
}

// Config 构造 Worker。
type Config struct {
	NodeID        string
	Bindings      BindingStore
	Journal       CommandJournal
	AgentSessions AgentSessionHandler
	NodeToolNames []string
	// DataDir 非空时默认使用目录持久化 Binding + CommandJournal（重启后可继续执行）。
	// 显式 Bindings/Journal 优先。
	DataDir string
}

// NewWorker 创建 Worker；未指定存储时：有 DataDir 则落盘，否则纯内存（测试默认）。
func NewWorker(cfg Config) *Worker {
	bindings := cfg.Bindings
	journal := cfg.Journal
	if bindings == nil || journal == nil {
		if dir := filepath.Clean(cfg.DataDir); dir != "" && dir != "." {
			if bindings == nil {
				b, err := NewDirBindingStore(filepath.Join(dir, "bindings"))
				if err != nil {
					bindings = NewMemoryBindingStore()
				} else {
					bindings = b
				}
			}
			if journal == nil {
				j, err := NewDirJournal(filepath.Join(dir, "journal"))
				if err != nil {
					journal = NewMemoryJournal()
				} else {
					journal = j
				}
			}
		}
	}
	if bindings == nil {
		bindings = NewMemoryBindingStore()
	}
	if journal == nil {
		journal = NewMemoryJournal()
	}
	w := &Worker{
		NodeID:           cfg.NodeID,
		Bindings:         bindings,
		Journal:          journal,
		Tombstones:       map[string]ArchiveTombstone{},
		MemberTombstones: map[string]ArchiveTombstone{},
	}
	w.AgentSessions = cfg.AgentSessions
	if setter, ok := w.AgentSessions.(AgentEventEmitter); ok {
		setter.SetAgentEventEmitter(w.EmitAgentFrame)
	}
	// 工作区工具由 Worker Executor 提供，不依赖本地 Agent Registry 枚举名。
	nodeTools := mergeToolNames(cfg.NodeToolNames, WorkspaceExecutableToolNames())
	manifest := BuildManifest(cfg.NodeID, nodeTools, WorkspaceToolSchemas(), WorkspaceSideEffectClasses())
	w.Provision = &Provisioner{
		NodeID:           cfg.NodeID,
		Bindings:         bindings,
		MemberTombstones: w.MemberTombstones,
		NodeToolNames:    nodeTools,
	}
	w.CatalogRevision = manifest.ToolCatalogRevision
	w.Capabilities = []string{
		"fencing",
		"idempotency",
		"resume",
		"timeline",
		"tool_execution",
	}
	w.Commands = &CommandHandler{
		Bindings:             bindings,
		Journal:              journal,
		ConnectionGeneration: 0,
		Tombstones:           w.Tombstones,
		MemberTombstones:     w.MemberTombstones,
		Executor:             NewWorkspaceToolExecutor(bindings),
	}
	return w
}

// SetOutbound installs the writer for the currently established Node→Manage
// WebSocket. It is intentionally connection-scoped and may be replaced on
// reconnect; Agent handlers do not need to know about the socket.
func (w *Worker) SetOutbound(writer func(map[string]any) error) {
	if w == nil {
		return
	}
	w.outboundMu.Lock()
	w.outbound = writer
	w.outboundMu.Unlock()
	if setter, ok := w.AgentSessions.(AgentEventEmitter); ok {
		setter.SetAgentEventEmitter(w.EmitAgentFrame)
	}
}

// EmitAgentFrame sends an ephemeral agent session response/event when the
// Dialer is connected. A missing writer is reported so callers can retain
// their durable local state and rely on reconnect/resume.
func (w *Worker) EmitAgentFrame(frame map[string]any) error {
	if w == nil {
		return errf(CodeNotAuthorized, "workgroup ws is not connected")
	}
	w.outboundMu.RLock()
	writer := w.outbound
	w.outboundMu.RUnlock()
	if writer == nil {
		return errf(CodeNotAuthorized, "workgroup ws is not connected")
	}
	// The bridge may be called by a goroutine long after the request frame was
	// decoded. Stamp the current generation at the final WS boundary so Manage
	// can fence events from a replaced connection.
	copyFrame := make(map[string]any, len(frame)+1)
	for key, value := range frame {
		copyFrame[key] = value
	}
	if payload, ok := copyFrame["payload"].(map[string]any); ok {
		copyPayload := make(map[string]any, len(payload)+1)
		for key, value := range payload {
			copyPayload[key] = value
		}
		if _, exists := copyPayload["connection_generation"]; !exists || asInt64Value(copyPayload["connection_generation"]) == 0 {
			copyPayload["connection_generation"] = w.Session.Generation()
		}
		copyFrame["payload"] = copyPayload
	}
	return writer(copyFrame)
}

func asInt64Value(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// Connect 模拟 session.hello，返回 connection_generation。
func (w *Worker) Connect() int64 {
	gen := w.Session.Hello(w.NodeID)
	w.mu.Lock()
	w.Commands.ConnectionGeneration = gen
	w.mu.Unlock()
	return gen
}

// HandleProvision 处理 member.provision。
func (w *Worker) HandleProvision(req ProvisionRequest) (*ProvisionResult, error) {
	return w.Provision.Provision(req)
}

// HandleCommand 处理 tool.command。
func (w *Worker) HandleCommand(cmd ToolCommand) (*AcceptResult, error) {
	binding, err := w.Bindings.Get(cmd.MemberID)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, errf(CodeNotFound, "worker binding not found for member %s", cmd.MemberID)
	}
	w.mu.Lock()
	w.Commands.CatalogRevision = binding.ToolCatalogRevision
	w.Commands.ConnectionGeneration = w.Session.Generation()
	w.mu.Unlock()
	return w.Commands.Accept(cmd, *binding)
}

// HandleCancel 处理 tool.cancel。
func (w *Worker) HandleCancel(commandID, workgroupID string) (*AcceptResult, error) {
	w.mu.Lock()
	w.Commands.ConnectionGeneration = w.Session.Generation()
	w.mu.Unlock()
	return w.Commands.Cancel(commandID, workgroupID)
}

// HandleArchive 应用 tombstone。
func (w *Worker) HandleArchive(t ArchiveTombstone) error {
	return w.Commands.ApplyArchiveTombstone(t, w.Bindings)
}

// IsLocalAgent 恒为 false：WorkerBinding 不可枚举为本地 Agent。
func (w *Worker) IsLocalAgent(memberID string) bool {
	b, err := w.Bindings.Get(memberID)
	if err != nil || b == nil {
		return false
	}
	return !b.NotEnumerableAsLocalAgent
}
