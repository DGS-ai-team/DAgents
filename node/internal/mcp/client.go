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
