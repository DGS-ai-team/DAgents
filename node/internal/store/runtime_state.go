package store

import (
	"encoding/json"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// RuntimeState 为 session 的兼容性持久化快照。
//
// Pending 与 ToolLoopCount 是旧版本字段。新运行时的权威状态来自 turn
// 事件重放后的 Coordinator；这两个字段只用于无生命周期事件的老数据迁移
// 以及向旧读取方提供镜像，不能作为新的执行位置来源。
type RuntimeState struct {
	Pending                 *turn.PendingHITL          `json:"pending,omitempty"`
	ToolLoopCount           int                        `json:"tool_loop_count"`
	HookStore               map[string]json.RawMessage `json:"hook_store,omitempty"`
	IdleAutoCompressApplied bool                       `json:"idle_auto_compress_applied,omitempty"`
	// NotifySeq 为最后需要 Client 关注的 SSE seq（F-E13 IM cursor）。
	NotifySeq int `json:"notify_seq,omitempty"`
	// AckSeq 为各 Client 已确认看到的最大 SSE seq。
	AckSeq int `json:"ack_seq,omitempty"`
}

// HasUnread 未读 = notify_seq > ack_seq。
func (rs RuntimeState) HasUnread() bool {
	return rs.NotifySeq > rs.AckSeq
}
