package hooks

// Phase 标识 Hook 执行锚点（首版仅实现 tool.before_each）。
type Phase string

const (
	PhaseToolBeforeEach Phase = "tool.before_each"
)
