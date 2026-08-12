package store

import (
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// RuntimeState 为 session 运行时快照（HITL pending、tool 循环计数、Hook 持久变量），用于 Node 重启后恢复。
type RuntimeState struct {
	Pending                 *turn.PendingHITL        `json:"pending,omitempty"`
	ToolLoopCount           int                      `json:"tool_loop_count"`
	HookStore               map[string]json.RawMessage `json:"hook_store,omitempty"`
	IdleAutoCompressApplied bool                     `json:"idle_auto_compress_applied,omitempty"`
	// NotifySeq 为最后需要 Client 关注的 SSE seq（F-E13 IM cursor）。
	NotifySeq int `json:"notify_seq,omitempty"`
	// AckSeq 为各 Client 已确认看到的最大 SSE seq。
	AckSeq int `json:"ack_seq,omitempty"`
}

// HasUnread 未读 = notify_seq > ack_seq。
func (rs RuntimeState) HasUnread() bool {
	return rs.NotifySeq > rs.AckSeq
}
