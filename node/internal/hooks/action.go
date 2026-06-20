package hooks

// Action 描述单个 Hook 对当前 phase 链的控制语义。
type Action string

const (
	ActionContinue      Action = "continue"
	ActionSkip          Action = "skip"           // 跳过本 phase 剩余 hook
	ActionAbortTurn     Action = "abort_turn"
	ActionAbortTool     Action = "abort_tool"
	ActionRejectEnqueue Action = "reject_enqueue"
)

// IsAbort 返回 action 是否会中断 phase 链并向上抛错。
func (a Action) IsAbort() bool {
	switch a {
	case ActionAbortTurn, ActionAbortTool, ActionRejectEnqueue:
		return true
	default:
		return false
	}
}

// normalizeAction 将空 action 视为 continue。
func normalizeAction(a Action) Action {
	if a == "" {
		return ActionContinue
	}
	return a
}
