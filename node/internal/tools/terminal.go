package tools

import (
	"context"
	"io"
	"time"
)

// TerminalRequest describes a long-lived interactive shell session. It is
// deliberately separate from ExecRequest: an interactive terminal owns a
// persistent stdin/stdout stream and must not inherit one-shot command
// timeout or background-job semantics.
type TerminalRequest struct {
	Target      ExecutionTarget
	Context     ExecutionContext
	ConfigID    string
	CWD         string
	Env         map[string]string
	Environment EnvironmentPolicy
	Shell       string
	Rows        int
	Cols        int
	EventSink   ProcessEventSink
}

// Terminal is the provider-neutral contract for an interactive PTY. Output
// is a single PTY stream, so stderr is intentionally not exposed separately.
// Implementations must treat Close as idempotent and must not keep remote
// sessions alive after the caller explicitly closes the terminal.
type Terminal interface {
	ID() string
	Input(ctx context.Context, data []byte) error
	Output() (io.ReadCloser, error)
	Resize(ctx context.Context, rows, cols int) error
	Start() error
	Wait() error
	ExitStatus() *ExitStatus
	Terminate(ctx context.Context) error
	Close() error
}

// TerminalProvider opens interactive terminals for an execution target. The
// provider owns transport and PTY lifecycle; orchestration still owns policy,
// HITL, history, and user-facing result formatting.
type TerminalProvider interface {
	OpenTerminal(ctx context.Context, req TerminalRequest) (Terminal, error)
	Test(ctx context.Context, target ExecutionTarget) (TargetStatus, error)
}

// TerminalSessionInfo is the model/UI-visible metadata for a long-lived
// terminal. The terminal's byte stream remains private to the session broker;
// callers use ReadOutput to consume bounded text snapshots.
type TerminalSessionInfo struct {
	ID         string    `json:"terminal_id"`
	AgentID    string    `json:"agent_id"`
	ConfigID   string    `json:"config_id,omitempty"`
	TargetKind string    `json:"target_kind"`
	TargetID   string    `json:"target_id,omitempty"`
	Shell      string    `json:"shell,omitempty"`
	CWD        string    `json:"cwd,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// TerminalCommandRequest is the bounded, one-shot command contract attached
// to an already-open terminal session. The terminal_id is validated by the
// session broker before this request reaches a provider; Target/ConfigID are
// filled from the authoritative session metadata and are not model supplied.
type TerminalCommandRequest struct {
	TerminalID     string
	Target         ExecutionTarget
	ConfigID       string
	Shell          string
	CWD            string
	Command        string
	Timeout        time.Duration
	MaxOutputBytes int
}

// TerminalCommandResult keeps stdout/stderr and exit semantics separate from
// the interactive PTY transcript. A command still requires an open terminal
// so target ownership and authorization cannot be bypassed by guessing a
// Linux channel ID.
type TerminalCommandResult struct {
	Status          string `json:"status"`
	TerminalID      string `json:"terminal_id"`
	TargetKind      string `json:"target_kind"`
	ExitCode        int    `json:"exit_code"`
	Stdout          string `json:"stdout,omitempty"`
	Stderr          string `json:"stderr,omitempty"`
	StdoutBytes     int    `json:"stdout_bytes"`
	StderrBytes     int    `json:"stderr_bytes"`
	OutputTruncated bool   `json:"output_truncated"`
	Cancelled       bool   `json:"cancelled,omitempty"`
	TimedOut        bool   `json:"timed_out"`
	Error           string `json:"error,omitempty"`
}

// TerminalConfigInfo is the safe, model-visible summary of a terminal target.
// Authentication material, host-key details, and other secrets are excluded.
// TargetKind/TargetID are internal routing fields and are intentionally not
// serialized into terminal_config_list results.
type TerminalConfigInfo struct {
	ConfigID    string `json:"config_id"`
	DisplayName string `json:"display_name,omitempty"`
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	Username    string `json:"username,omitempty"`
	Remark      string `json:"remark,omitempty"`
	Shell       string `json:"shell,omitempty"`
	TargetKind  string `json:"-"`
	TargetID    string `json:"-"`
}

// TerminalConfigLinuxPrefix prevents a remote channel ID from colliding with
// the built-in local config ID and makes it impossible to confuse a config
// identifier with an arbitrary provider target.
const TerminalConfigLinuxPrefix = "linux_channel:"

// DefaultTerminalSessionLimit is the in-memory per-Agent terminal limit. It
// is deliberately finite because terminal sessions are interactive resources
// and are not restored after a Node restart.
const DefaultTerminalSessionLimit = 8

// TerminalConfigResolver exposes only the terminal configs bound to one Agent.
// Implementations must never return passwords, private keys, secret refs, or
// host-key material.
type TerminalConfigResolver interface {
	ListTerminalConfigs(ctx context.Context, agentID string) ([]TerminalConfigInfo, error)
	ResolveTerminalConfig(ctx context.Context, agentID, configID string) (TerminalConfigInfo, error)
}

// TerminalOutputChunk is a bounded output fragment returned to an Agent tool
// or another non-WebSocket consumer. Seq is monotonic within one session.
type TerminalOutputChunk struct {
	Seq  uint64 `json:"seq"`
	Data []byte `json:"data"`
}

// TerminalOutput is deliberately cursor-based so an Agent can poll without
// copying an unbounded PTY transcript into every tool result.
type TerminalOutput struct {
	Chunks            []TerminalOutputChunk `json:"chunks"`
	NextSeq           uint64                `json:"next_seq"`
	ReplayGap         bool                  `json:"replay_gap,omitempty"`
	Exited            bool                  `json:"exited"`
	Graceful          bool                  `json:"graceful,omitempty"`
	Forced            bool                  `json:"forced,omitempty"`
	TerminationStatus string                `json:"termination_status,omitempty"`
	Exit              *ExitStatus           `json:"exit,omitempty"`
}

// TerminalSessionBroker is the lifecycle seam shared by the Agent terminal
// tools and the WebSocket/UI transport. Implementations must enforce Agent
// ownership for every operation.
type TerminalSessionBroker interface {
	Open(ctx context.Context, agentID string, req TerminalRequest) (TerminalSessionInfo, error)
	List(agentID string) []TerminalSessionInfo
	Lookup(agentID, terminalID string) (TerminalSessionInfo, error)
	ReadOutput(ctx context.Context, agentID, terminalID string, afterSeq uint64, maxBytes int) (TerminalOutput, error)
	Input(ctx context.Context, agentID, terminalID string, data []byte) error
	Terminate(ctx context.Context, agentID, terminalID string) (TerminalOutput, error)
}
