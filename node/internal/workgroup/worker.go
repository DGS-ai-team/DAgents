package workgroup

import "sync"

// Worker 聚合 D2 骨架能力：provision / journal / fencing / manifest / session。
type Worker struct {
	mu        sync.Mutex
	NodeID    string
	Bindings  BindingStore
	Journal   CommandJournal
	Session   Session
	Provision *Provisioner
	Commands  *CommandHandler
	Tombstones map[string]ArchiveTombstone
}

// Config 构造 Worker。
type Config struct {
	NodeID        string
	Bindings      BindingStore
	Journal       CommandJournal
	NodeToolNames []string
}

// NewWorker 创建内存默认存储的 Worker。
func NewWorker(cfg Config) *Worker {
	bindings := cfg.Bindings
	if bindings == nil {
		bindings = NewMemoryBindingStore()
	}
	journal := cfg.Journal
	if journal == nil {
		journal = NewMemoryJournal()
	}
	w := &Worker{
		NodeID:     cfg.NodeID,
		Bindings:   bindings,
		Journal:    journal,
		Tombstones: map[string]ArchiveTombstone{},
	}
	w.Provision = &Provisioner{
		NodeID:        cfg.NodeID,
		Bindings:      bindings,
		NodeToolNames: append([]string(nil), cfg.NodeToolNames...),
	}
	w.Commands = &CommandHandler{
		Bindings:             bindings,
		Journal:              journal,
		ConnectionGeneration: 0,
		Tombstones:           w.Tombstones,
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
