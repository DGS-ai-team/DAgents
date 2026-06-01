package store

import "github.com/DGS-ai-team/DAgents/node/internal/turn"

// RuntimeState 为 session 运行时快照（HITL pending、tool 循环计数），用于 Node 重启后恢复。
type RuntimeState struct {
	Pending       *turn.PendingHITL `json:"pending,omitempty"`
	ToolLoopCount int               `json:"tool_loop_count"`
}
