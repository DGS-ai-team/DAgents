package store

import (
	"encoding/json"
	"time"
)

// RuntimeState 保存会话的非生命周期运行时状态。
// Turn/Step/Interaction 状态统一由生命周期事件重放得到，不在这里复制。
type RuntimeState struct {
	// InputBoxState stores only queue watermarks and the queue storage version.
	// Input payloads live in conversation_input_queue; receipts live in
	// conversation_inputs. Resume/cancel remain control-plane operations.
	InputBoxState json.RawMessage `json:"input_box_state,omitempty"`
	// HistoryRevision monotonically identifies the committed message snapshot.
	// It is independent from the SSE Hub sequence and the Turn lifecycle
	// sequence, so hydrate callers can reject an older transcript projection.
	HistoryRevision uint64 `json:"history_revision,omitempty"`
	// ContextCheckpointRevision is allocated independently by conversation
	// storage and does not share the transcript revision namespace.
	ContextCheckpointRevision uint64 `json:"context_checkpoint_revision,omitempty"`
	// ConversationRevision is the CAS revision for explicit record/checkpoint deltas.
	ConversationRevision uint64 `json:"conversation_revision,omitempty"`
	ActiveRoundID        string `json:"active_round_id,omitempty"`
	ActiveRevisionID     string `json:"active_revision_id,omitempty"`
	// ToolCallProjectionHiddenThroughGeneration preserves a context-clear display
	// boundary while leaving lifecycle facts available for recovery.
	ToolCallProjectionHiddenThroughGeneration uint64 `json:"tool_call_projection_hidden_through_generation,omitempty"`
	// ActiveContextStart is the index in the durable transcript from which
	// future model requests may build context. Older messages remain queryable.
	ActiveContextStart int `json:"active_context_start,omitempty"`
	// LastContextResetID makes a dreaming/context-boundary commit idempotent;
	// retries must not hide messages appended after the original reset.
	LastContextResetID string                     `json:"last_context_reset_id,omitempty"`
	DreamingAttempt    json.RawMessage            `json:"dreaming_attempt,omitempty"`
	HookStore          map[string]json.RawMessage `json:"hook_store,omitempty"`
	// LastHumanInteraction is set only after a human-originated logical turn
	// reaches a terminal state; it is the idle baseline, never UpdatedAt.
	LastHumanInteraction time.Time `json:"last_human_interaction,omitempty"`
	// NotifySeq 为最后需要 Client 关注的 SSE seq（F-E13 IM cursor）。
	NotifySeq int `json:"notify_seq,omitempty"`
	// AckSeq 为各 Client 已确认看到的最大 SSE seq。
	AckSeq int `json:"ack_seq,omitempty"`
}

// HasUnread 未读 = notify_seq > ack_seq。
func (rs RuntimeState) HasUnread() bool {
	return rs.NotifySeq > rs.AckSeq
}
