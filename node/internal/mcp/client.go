package mcp

import (
	"context"
	"encoding/json"
)

// Client is the transport-neutral subset used by Manager. Implementations
// must keep MCP framing and connection lifecycle details out of the tool
// registry.
type Client interface {
	Start(context.Context) error
	ListTools(context.Context) ([]Tool, error)
	CallTool(context.Context, string, json.RawMessage) (CallResult, error)
	Close() error
}

// DiagnosticsProvider exposes transport-level failure evidence without
// widening the common Client interface. HTTP clients can omit it; stdio
// clients use it to preserve bounded stderr and the child exit code.
type DiagnosticsProvider interface {
	Diagnostics() ClientDiagnostics
}

type ClientDiagnostics struct {
	Stderr   string
	ExitCode *int
}
