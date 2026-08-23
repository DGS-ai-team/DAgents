// Package workgroup 实现 Node 侧 WorkgroupWorker（D2 骨架）。
package workgroup

import (
	"context"
	"time"
)

const SchemaVersion = "0.5.0"

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
	TurnID          string `json:"turn_id,omitempty"`
	UserMessage     string `json:"user_message"`
	ClientMessageID string `json:"client_message_id,omitempty"`
}

type AgentTurnCancelRequest struct {
	WorkgroupID string `json:"workgroup_id"`
	MemberID    string `json:"member_id"`
	AgentID     string `json:"agent_id"`
	SessionID   string `json:"session_id"`
	AssignID    string `json:"assign_id"`
}

// AgentSessionHandler is implemented by the Node API bridge. It is separate
// from the legacy WorkerBinding executor so an existing Agent is never
// materialized as a restricted synthetic member.
type AgentSessionHandler interface {
	OpenAgentSession(context.Context, AgentSessionOpenRequest) (AgentSessionResult, error)
	StartAgentTurn(context.Context, AgentTurnStartRequest) error
	CancelAgentTurn(context.Context, AgentTurnCancelRequest) error
	CloseAgentSession(context.Context, AgentSessionOpenRequest) error
}

// AgentEventEmitter is an optional hook used by asynchronous Node runtimes to
// send streamed events back over the already established outbound WS.
type AgentEventEmitter interface {
	SetAgentEventEmitter(func(map[string]any) error)
}

// WorkerBinding 对应 D0.5 WorkerBinding.json；不进入本地 Agents API。
type WorkerBinding struct {
	MemberID                  string    `json:"member_id"`
	WorkgroupID               string    `json:"workgroup_id"`
	HomeNodeID                string    `json:"home_node_id"`
	ProvisionID               string    `json:"provision_id"`
	MemberSpecDigest          string    `json:"member_spec_digest"`
	LeaseEpoch                int64     `json:"lease_epoch"`
	MemberGeneration          int64     `json:"member_generation"`
	WorkspacePath             string    `json:"workspace_path"`
	Status                    string    `json:"status"` // provisioning|ready|busy|archived|error
	NotEnumerableAsLocalAgent bool      `json:"not_enumerable_as_local_agent"`
	ToolAllowNames            []string  `json:"tool_allow_names,omitempty"`
	ToolCatalogRevision       string    `json:"tool_catalog_revision,omitempty"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

// ProvisionRequest 为 member.provision 入参。
type ProvisionRequest struct {
	ProvisionID      string
	WorkgroupID      string
	MemberID         string
	HomeNodeID       string
	MemberSpecDigest string
	LeaseEpoch       int64
	MemberGeneration int64
	ToolAllowNames   []string
	WorkspaceRoot    string // Node agents 根目录；实际路径 = root/workgroup/<wg>/<mb>
}

// ProvisionResult 为 provision 结果。
type ProvisionResult struct {
	Binding  WorkerBinding
	Created  bool // 本次是否新建 workspace
	Manifest ToolManifest
}

// ToolManifest 对齐契约 §7.1。
type ToolManifest struct {
	NodeID              string             `json:"node_id"`
	ToolCatalogRevision string             `json:"tool_catalog_revision"`
	Tools               []ToolCatalogEntry `json:"tools"`
}

// ToolCatalogEntry 为 manifest 中单工具条目。
type ToolCatalogEntry struct {
	Name            string         `json:"name"`
	JSONSchema      map[string]any `json:"json_schema"`
	SideEffectClass string         `json:"side_effect_class"`
	ExecutionMode   string         `json:"execution_mode"`
}

// ToolCommand 对齐 ToolCommand.json（D2 用到的字段）。
type ToolCommand struct {
	CommandID           string `json:"command_id"`
	WorkgroupID         string `json:"workgroup_id"`
	MemberID            string `json:"member_id"`
	AssignID            string `json:"assign_id"`
	RunID               string `json:"run_id"`
	TurnID              string `json:"turn_id"`
	ToolCallID          string `json:"tool_call_id"`
	ToolName            string `json:"tool_name"`
	ArgumentsJSON       string `json:"arguments_json"`
	PayloadHash         string `json:"payload_hash"`
	LeaseID             string `json:"lease_id"`
	LeaseEpoch          int64  `json:"lease_epoch"`
	MemberGeneration    int64  `json:"member_generation"`
	MemberSpecDigest    string `json:"member_spec_digest"`
	ToolCatalogRevision string `json:"tool_catalog_revision"`
	Status              string `json:"status"`
	SideEffectClass     string `json:"side_effect_class"`
}

// CommandAck 对齐 CommandAck.json。
type CommandAck struct {
	CommandID            string `json:"command_id"`
	Status               string `json:"status"`
	ConnectionGeneration int64  `json:"connection_generation"`
	JournaledAt          string `json:"journaled_at"`
}

// JournalEntry 为 Node 本地 command journal 记录。
type JournalEntry struct {
	CommandID       string `json:"command_id"`
	PayloadHash     string `json:"payload_hash"`
	Status          string `json:"status"`
	MemberID        string `json:"member_id"`
	WorkgroupID     string `json:"workgroup_id"`
	ToolName        string `json:"tool_name"`
	SideEffectClass string `json:"side_effect_class"`
	ResultJSON      string `json:"result_json,omitempty"`
	ErrorCode       string `json:"error_code,omitempty"`
	Executions      int    `json:"executions"`
	JournaledAt     string `json:"journaled_at"`
	UpdatedAt       string `json:"updated_at"`
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

// ArchiveTombstone 归档栅栏。
type ArchiveTombstone struct {
	WorkgroupID         string `json:"workgroup_id"`
	MemberID            string `json:"member_id,omitempty"`
	LeaseEpochAtArchive int64  `json:"lease_epoch_at_archive"`
}

func memberTombstoneKey(workgroupID, memberID string) string {
	return workgroupID + "\x00" + memberID
}
