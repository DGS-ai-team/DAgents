package hooks

// Result 为单个 Hook 对 Context 的决策输出。
type Result struct {
	Action    Action
	Mutations map[string]any
	Err       error // Abort 时附带原因
}
