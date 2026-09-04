// Package workgroup implements the Node side of the Workgroup AgentRef
// session protocol.
package workgroup

import (
	"context"
)

const (
	ProtocolVersion = "1"
	SchemaVersion   = "0.5.0"
)

// AgentSessionOpenRequest binds a Workgroup member to an already existing
// Agent runtime on this Node. The session id is deliberately supplied by
// Manage so it is stable across reconnects and isolated per workgroup.
type AgentSessionOpenRequest struct {
	WorkgroupID string `json:"workgroup_id"`
	MemberID    string `json:"member_id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
}

type AgentSessionResult struct {
	WorkgroupID string `json:"workgroup_id"`
	MemberID    string `json:"member_id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	Status      string `json:"status"` // ready|error|closed
	Message     string `json:"message,omitempty"`
}

// AgentTurnStartRequest is the session-scoped user turn sent by Manage.
type AgentTurnStartRequest struct {
	WorkgroupID     string `json:"workgroup_id"`
	MemberID        string `json:"member_id"`
	AgentID         string `json:"agent_id"`
	SessionID       string `json:"session_id"`
	AssignID        string `json:"assign_id"`
	Source          string `json:"source"` // leader_tool|direct_member
	ParentTurnID    string `json:"parent_turn_id"`
	ChildTurnID     string `json:"child_turn_id"`
	AttemptID       string `json:"attempt_id"`
	UserMessage     string `json:"user_message"`
	ClientMessageID string `json:"client_message_id,omitempty"`
}

type AgentTurnCancelRequest struct {
	WorkgroupID string `json:"workgroup_id"`
	MemberID    string `json:"member_id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	AssignID    string `json:"assign_id"`
	ChildTurnID string `json:"child_turn_id"`
	AttemptID   string `json:"attempt_id"`
}

// AgentToolCancelRequest cancels one tool execution inside an AgentRef turn.
// The Node remains the authority for whether that concrete tool is
// cancellable; unsupported tools return an explicit error instead of
// accidentally cancelling the whole turn.
type AgentToolCancelRequest struct {
	WorkgroupID string `json:"workgroup_id"`
	MemberID    string `json:"member_id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	AssignID    string `json:"assign_id"`
	ToolCallID  string `json:"tool_call_id"`
	ToolName    string `json:"tool_name"`
}

// AgentTurnResumeRequest resumes the pending HITL in an AgentRef session.
// The resume value is intentionally opaque to the Workgroup protocol; Node's
// turn runtime remains the authority for validating its concrete shape.
type AgentTurnResumeRequest struct {
	WorkgroupID string         `json:"workgroup_id"`
	MemberID    string         `json:"member_id"`
	AgentID     string         `json:"agent_id"`
	SessionID   string         `json:"session_id"`
	AssignID    string         `json:"assign_id"`
	ChildTurnID string         `json:"child_turn_id"`
	AttemptID   string         `json:"attempt_id"`
	HitlID      string         `json:"hitl_id,omitempty"`
	ResumeValue map[string]any `json:"resume_value"`
}

// AgentSessionHandler is implemented by the Node API bridge. Workgroup
// members always bind to an existing Agent session; no synthetic member is
// materialized on the Node.
type AgentSessionHandler interface {
	OpenAgentSession(context.Context, AgentSessionOpenRequest) (AgentSessionResult, error)
	StartAgentTurn(context.Context, AgentTurnStartRequest) error
	CancelAgentTurn(context.Context, AgentTurnCancelRequest) error
	ResumeAgentTurn(context.Context, AgentTurnResumeRequest) error
	CloseAgentSession(context.Context, AgentSessionOpenRequest) error
}

// AgentToolCancelHandler is implemented when the Node can cancel the active
// tool without cancelling the whole Agent turn.
type AgentToolCancelHandler interface {
	CancelAgentTool(context.Context, AgentToolCancelRequest) error
}

// AgentEventEmitter is an optional hook used by asynchronous Node runtimes to
// send streamed events back over the already established outbound WS.
type AgentEventEmitter interface {
	SetAgentEventEmitter(func(map[string]any) error)
}

// WSEnvelope 对齐 WSEnvelope.json。
type WSEnvelope struct {
	EnvelopeID           string         `json:"envelope_id"`
	SchemaVersion        string         `json:"schema_version"`
	Type                 string         `json:"type"`
	DeliverySeq          int64          `json:"delivery_seq"`
	WorkgroupID          string         `json:"workgroup_id,omitempty"`
	ConnectionGeneration int64          `json:"connection_generation,omitempty"`
	Payload              map[string]any `json:"payload"`
	SentAt               string         `json:"sent_at"`
}

// ResumeCursor 对齐 ResumeCursor.json 核心字段。
type ResumeCursor struct {
	LastAckDeliverySeq int64 `json:"last_ack_delivery_seq"`
}
