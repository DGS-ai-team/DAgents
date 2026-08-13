// Package mcp implements the Node-side Model Context Protocol client.
//
// The package deliberately owns protocol and connection-lifecycle concerns
// only; tool registration and Agent policy remain in their existing packages.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const (
	// ProtocolVersion is the MCP revision used during initialize.
	ProtocolVersion = "2025-06-18"
	TransportStdio  = "stdio"
	// TransportStreamableHTTP is MCP's HTTP transport where each JSON-RPC
	// request is sent as an HTTP POST. A response may be JSON or SSE.
	TransportStreamableHTTP = "streamable_http"
	StatusDisabled          = "disabled"
	StatusOffline           = "offline"
	StatusReady             = "ready"
	StatusError             = "error"
)

var toolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var llmToolNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ServerConfig is persisted by Node. EnvRefs/HeaderRefs map a child-process
// variable or HTTP header to an existing Node process environment variable.
// EnvValues/HeaderValues are the optional literal values supplied in the raw
// MCP configuration. They are hidden from the normal API server view, but are
// persisted so a locally configured MCP server can use either form.
type ServerConfig struct {
	ID           string            `json:"id"`
	DisplayName  string            `json:"display_name"`
	Transport    string            `json:"transport"`
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	CWD          string            `json:"cwd,omitempty"`
	URL          string            `json:"url,omitempty"`
	EnvRefs      map[string]string `json:"env_refs,omitempty"`
	HeaderRefs   map[string]string `json:"header_refs,omitempty"`
	EnvValues    map[string]string `json:"-"`
	HeaderValues map[string]string `json:"-"`
	// EnabledTools is the Node-side service allowlist. An empty list means
	// no remote tools are exposed; it is intentionally fail-closed.
	EnabledTools []string `json:"enabled_tools,omitempty"`
	Enabled      bool     `json:"enabled"`
}

// Binding selects a configured server for one Agent. An empty ToolAllowlist
// means all tools advertised by that server.
type Binding struct {
	ServerID      string   `json:"server_id"`
	Enabled       bool     `json:"enabled"`
	ToolAllowlist []string `json:"tool_allowlist,omitempty"`
}

type AgentConfig struct {
	Bindings []Binding `json:"bindings,omitempty"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema"`
	Enabled     bool           `json:"enabled"`
}

type ContentBlock struct {
	Type     string         `json:"type"`
	Text     string         `json:"text,omitempty"`
	MimeType string         `json:"mimeType,omitempty"`
	Data     string         `json:"data,omitempty"`
	Raw      map[string]any `json:"-"`
}

func (b *ContentBlock) UnmarshalJSON(data []byte) error {
	type alias ContentBlock
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decoded.Raw = raw
	*b = ContentBlock(decoded)
	return nil
}

type CallResult struct {
	Content           []ContentBlock `json:"content,omitempty"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
}

type ServerView struct {
	ServerConfig
	Status           string `json:"status"`
	LastError        string `json:"last_error,omitempty"`
	ToolCount        int    `json:"tool_count"`
	EnabledToolCount int    `json:"enabled_tool_count"`
	LastRefresh      string `json:"last_refresh,omitempty"`
	Tools            []Tool `json:"tools,omitempty"`
}

// EffectiveTool is the validated, per-Agent projection of a remote tool.
// Call is intentionally a closure so the tools package does not need to know
// anything about MCP process management.
type EffectiveTool struct {
	QualifiedName string
	ServerID      string
	RemoteName    string
	Description   string
	InputSchema   map[string]any
	Call          func(context.Context, json.RawMessage) (CallResult, error)
}

func (c ServerConfig) normalized() ServerConfig {
	c.ID = strings.TrimSpace(c.ID)
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	c.Transport = strings.ToLower(strings.TrimSpace(c.Transport))
	c.URL = strings.TrimSpace(c.URL)
	if c.Transport == "" {
		if c.URL != "" {
			c.Transport = TransportStreamableHTTP
		} else {
			c.Transport = TransportStdio
		}
	}
	switch c.Transport {
	case "http", "streamable-http", "streamablehttp":
		c.Transport = TransportStreamableHTTP
	}
	c.Command = strings.TrimSpace(c.Command)
	c.CWD = strings.TrimSpace(c.CWD)
	if c.Args == nil {
		c.Args = []string{}
	}
	if c.EnvRefs == nil {
		c.EnvRefs = map[string]string{}
	}
	if c.HeaderRefs == nil {
		c.HeaderRefs = map[string]string{}
	}
	if c.EnvValues == nil {
		c.EnvValues = map[string]string{}
	}
	if c.HeaderValues == nil {
		c.HeaderValues = map[string]string{}
	}
	if c.EnabledTools == nil {
		c.EnabledTools = []string{}
	}
	return c
}

func ValidateServerConfig(raw ServerConfig) (ServerConfig, error) {
	c := raw.normalized()
	if c.ID == "" {
		return ServerConfig{}, fmt.Errorf("mcp server id is required")
	}
	if !toolNamePattern.MatchString(c.ID) || len(c.ID) > 24 {
		return ServerConfig{}, fmt.Errorf("mcp server id must contain only letters, digits, ., _ or - and be at most 24 characters")
	}
	switch c.Transport {
	case TransportStdio:
		if c.Command == "" {
			return ServerConfig{}, fmt.Errorf("mcp server command is required")
		}
	case TransportStreamableHTTP:
		parsed, err := url.Parse(c.URL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return ServerConfig{}, fmt.Errorf("mcp server url must be an absolute http(s) URL")
		}
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return ServerConfig{}, fmt.Errorf("mcp server url must use http or https")
		}
		if parsed.User != nil {
			return ServerConfig{}, fmt.Errorf("mcp server url must not contain user credentials")
		}
	default:
		return ServerConfig{}, fmt.Errorf("unsupported mcp transport %q", c.Transport)
	}
	for childName, envName := range c.EnvRefs {
		if strings.TrimSpace(childName) == "" || strings.TrimSpace(envName) == "" {
			return ServerConfig{}, fmt.Errorf("mcp env_refs cannot contain empty names")
		}
	}
	for headerName, envName := range c.HeaderRefs {
		if strings.TrimSpace(headerName) == "" || strings.TrimSpace(envName) == "" {
			return ServerConfig{}, fmt.Errorf("mcp header_refs cannot contain empty names")
		}
		if http.CanonicalHeaderKey(strings.TrimSpace(headerName)) == "" {
			return ServerConfig{}, fmt.Errorf("mcp header_refs contains invalid header name %q", headerName)
		}
	}
	enabledTools, err := NormalizeEnabledTools(c.EnabledTools)
	if err != nil {
		return ServerConfig{}, err
	}
	c.EnabledTools = enabledTools
	return c, nil
}

// NormalizeEnabledTools validates and deduplicates the service-level tool
// allowlist. The empty result is meaningful: it disables every tool.
func NormalizeEnabledTools(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if !toolNamePattern.MatchString(name) {
			return nil, fmt.Errorf("mcp enabled_tools contains unsupported tool name %q", name)
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

func (b Binding) normalized() Binding {
	b.ServerID = strings.TrimSpace(b.ServerID)
	if b.ToolAllowlist == nil {
		b.ToolAllowlist = []string{}
	}
	return b
}

func BindingsFromDefaults(defaults map[string]any) []Binding {
	if defaults == nil {
		return nil
	}
	raw, ok := defaults["mcp"]
	if !ok || raw == nil {
		return nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	out := make([]Binding, 0, len(cfg.Bindings))
	seen := map[string]struct{}{}
	for _, binding := range cfg.Bindings {
		binding = binding.normalized()
		if binding.ServerID == "" {
			continue
		}
		if _, exists := seen[binding.ServerID]; exists {
			continue
		}
		seen[binding.ServerID] = struct{}{}
		out = append(out, binding)
	}
	return out
}

func BindingsToDefaults(defaults map[string]any, bindings []Binding) map[string]any {
	out := make(map[string]any, len(defaults)+1)
	for key, value := range defaults {
		out[key] = value
	}
	normalized := make([]Binding, 0, len(bindings))
	seen := map[string]struct{}{}
	for _, binding := range bindings {
		binding = binding.normalized()
		if binding.ServerID == "" {
			continue
		}
		if _, exists := seen[binding.ServerID]; exists {
			continue
		}
		seen[binding.ServerID] = struct{}{}
		sort.Strings(binding.ToolAllowlist)
		normalized = append(normalized, binding)
	}
	out["mcp"] = map[string]any{"bindings": normalized}
	return out
}

func QualifiedToolName(serverID, remoteName string) (string, error) {
	serverID = strings.TrimSpace(serverID)
	remoteName = strings.TrimSpace(remoteName)
	if !toolNamePattern.MatchString(serverID) || !toolNamePattern.MatchString(remoteName) {
		return "", fmt.Errorf("mcp tool name contains unsupported characters: %q", remoteName)
	}
	// MCP tool names may contain dots, while OpenAI-compatible function
	// schemas (including DeepSeek) only accept letters, digits, '_' and '-'.
	// Keep the raw remote name in the MCP catalog and replace unsupported
	// characters only in the stable name exposed to the LLM.
	name := "mcp__" + normalizeToolNamePart(serverID) + "__" + normalizeToolNamePart(remoteName)
	if !llmToolNamePattern.MatchString(name) {
		return "", fmt.Errorf("mcp qualified tool name is not LLM-compatible: %q", name)
	}
	if len(name) > 64 {
		return "", fmt.Errorf("qualified mcp tool name is too long: %q", name)
	}
	return name, nil
}

func normalizeToolNamePart(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func toolAllowed(binding Binding, remoteName string) bool {
	if len(binding.ToolAllowlist) == 0 {
		return true
	}
	for _, allowed := range binding.ToolAllowlist {
		if strings.TrimSpace(allowed) == remoteName {
			return true
		}
	}
	return false
}
