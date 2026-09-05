package store

import "encoding/json"

// RuntimeState 保存会话的非生命周期运行时状态。
// Turn/Step/Interaction 状态统一由生命周期事件重放得到，不在这里复制。
type RuntimeState struct {
	// InputBoxState is the serialized FIFO tail of external user/trigger/child-agent
	// inputs. Resume/cancel remain control-plane operations and are not stored
	// here.
	InputBoxState json.RawMessage `json:"input_box_state,omitempty"`
	// HistoryRevision monotonically identifies the committed message snapshot.
	// It is independent from the SSE Hub sequence and the Turn lifecycle
	// sequence, so hydrate callers can reject an older transcript projection.
	HistoryRevision         uint64                     `json:"history_revision,omitempty"`
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
