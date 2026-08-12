package workgroup

import (
	"path/filepath"
	"sync"
)

// Worker 聚合 D2 骨架能力：provision / journal / fencing / manifest / session。
type Worker struct {
	mu         sync.Mutex
	NodeID     string
	Bindings   BindingStore
	Journal    CommandJournal
	Session    Session
	Provision  *Provisioner
	Commands   *CommandHandler
	Tombstones map[string]ArchiveTombstone
	// OnTimelineEvent forwards public Manage Timeline frames to the local Web UI.
	OnTimelineEvent func(WSEnvelope)
}

// Config 构造 Worker。
type Config struct {
	NodeID        string
	Bindings      BindingStore
	Journal       CommandJournal
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
		NodeID:     cfg.NodeID,
		Bindings:   bindings,
		Journal:    journal,
		Tombstones: map[string]ArchiveTombstone{},
	}
	// 工作区工具由 Worker Executor 提供，不依赖本地 Agent Registry 枚举名。
	nodeTools := mergeToolNames(cfg.NodeToolNames, WorkspaceExecutableToolNames())
	w.Provision = &Provisioner{
		NodeID:        cfg.NodeID,
		Bindings:      bindings,
		NodeToolNames: nodeTools,
	}
	w.Commands = &CommandHandler{
		Bindings:             bindings,
		Journal:              journal,
		ConnectionGeneration: 0,
		Tombstones:           w.Tombstones,
		Executor:             NewWorkspaceToolExecutor(bindings),
	}
	return w
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
