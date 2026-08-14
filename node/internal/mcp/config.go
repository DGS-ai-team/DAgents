package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var envPlaceholderPattern = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)

// ConfigText is the portable MCP configuration shape used by the Node UI.
// It follows the mcpServers format used by common MCP clients while keeping
// credentials as environment-variable placeholders.
type configText struct {
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

type configTextServer struct {
	Type         string            `json:"type"`
	Transport    string            `json:"transport"`
	DisplayName  string            `json:"display_name"`
	Command      string            `json:"command"`
	Args         []string          `json:"args"`
	CWD          string            `json:"cwd"`
	URL          string            `json:"url"`
	Env          map[string]string `json:"env"`
	Headers      map[string]string `json:"headers"`
	EnvRefs      map[string]string `json:"env_refs"`
	HeaderRefs   map[string]string `json:"header_refs"`
	EnabledTools []string          `json:"enabled_tools"`
	Enabled      *bool             `json:"enabled"`
}

// ParseConfigText parses an mcpServers JSON document. Existing service-level
// allowlists are retained when enabled_tools is omitted, so editing transport
// details does not silently expose or disable tools.
func ParseConfigText(text string, existing []ServerConfig) ([]ServerConfig, error) {
	if strings.TrimSpace(text) == "" {
		return []ServerConfig{}, nil
	}
	var envelope configText
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &envelope); err != nil {
		return nil, fmt.Errorf("parse MCP configuration JSON: %w", err)
	}
	if len(envelope.MCPServers) == 0 {
		return []ServerConfig{}, nil
	}
	existingByID := make(map[string]ServerConfig, len(existing))
	for _, cfg := range existing {
		existingByID[cfg.ID] = cfg
	}
	ids := make([]string, 0, len(envelope.MCPServers))
	for id := range envelope.MCPServers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]ServerConfig, 0, len(ids))
	for _, id := range ids {
		var raw configTextServer
		if err := json.Unmarshal(envelope.MCPServers[id], &raw); err != nil {
			return nil, fmt.Errorf("parse MCP server %q: %w", id, err)
		}
		cfg := ServerConfig{
			ID: id, DisplayName: raw.DisplayName, Transport: firstNonEmpty(raw.Transport, raw.Type),
			Command: raw.Command, Args: raw.Args, CWD: raw.CWD, URL: raw.URL,
			EnvRefs: raw.EnvRefs, HeaderRefs: raw.HeaderRefs, EnabledTools: raw.EnabledTools,
			Enabled: raw.Enabled == nil || *raw.Enabled,
		}
		if cfg.Transport == "" && cfg.URL != "" {
			cfg.Transport = TransportStreamableHTTP
		}
		if len(raw.Env) > 0 {
			refs, values, err := splitConfiguredValues(raw.Env, "environment")
			if err != nil {
				return nil, fmt.Errorf("MCP server %q: %w", id, err)
			}
			cfg.EnvRefs = refs
			cfg.EnvValues = values
		}
		if len(raw.Headers) > 0 {
			refs, values, err := splitConfiguredValues(raw.Headers, "header")
			if err != nil {
				return nil, fmt.Errorf("MCP server %q: %w", id, err)
			}
			cfg.HeaderRefs = refs
			cfg.HeaderValues = values
		}
		if !jsonFieldPresent(envelope.MCPServers[id], "enabled_tools") {
			cfg.EnabledTools = append([]string(nil), existingByID[id].EnabledTools...)
		}
		validated, err := ValidateServerConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("MCP server %q: %w", id, err)
		}
		out = append(out, validated)
	}
	return out, nil
}

// FormatConfigText emits the normalized mcpServers JSON shown in the editor.
// Secret references are rendered as ${ENV_NAME}, never as their values.
func FormatConfigText(configs []ServerConfig) (string, error) {
	ids := make([]string, 0, len(configs))
	byID := make(map[string]ServerConfig, len(configs))
	for _, raw := range configs {
		cfg, err := ValidateServerConfig(raw)
		if err != nil {
			return "", err
		}
		ids = append(ids, cfg.ID)
		byID[cfg.ID] = cfg
	}
	sort.Strings(ids)
	servers := make(map[string]any, len(ids))
	for _, id := range ids {
		cfg := byID[id]
		item := map[string]any{}
		if cfg.DisplayName != "" {
			item["display_name"] = cfg.DisplayName
		}
		if cfg.Transport == TransportStreamableHTTP {
			item["type"] = "streamable-http"
			item["url"] = cfg.URL
			item["headers"] = configuredValues(cfg.HeaderRefs, cfg.HeaderValues)
		} else {
			item["type"] = TransportStdio
			item["command"] = cfg.Command
			item["args"] = append([]string{}, cfg.Args...)
			if cfg.CWD != "" {
				item["cwd"] = cfg.CWD
			}
			item["env"] = configuredValues(cfg.EnvRefs, cfg.EnvValues)
		}
		servers[id] = item
	}
	data, err := json.MarshalIndent(map[string]any{"mcpServers": servers}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func splitConfiguredValues(values map[string]string, kind string) (map[string]string, map[string]string, error) {
	refs := make(map[string]string, len(values))
	literals := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, nil, fmt.Errorf("%s name cannot be empty", kind)
		}
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil, fmt.Errorf("%s %q cannot be empty", kind, key)
		}
		match := envPlaceholderPattern.FindStringSubmatch(value)
		if len(match) == 2 {
			refs[key] = match[1]
		} else {
			if kind == "header" && strings.ContainsAny(value, "\r\n") {
				return nil, nil, fmt.Errorf("literal header %q contains invalid line breaks", key)
			}
			literals[key] = value
		}
	}
	return refs, literals, nil
}

func configuredValues(refs, literals map[string]string) map[string]string {
	out := make(map[string]string, len(refs)+len(literals))
	for key, value := range literals {
		out[key] = value
	}
	keys := make([]string, 0, len(refs))
	for key := range refs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out[key] = "${" + strings.TrimSpace(refs[key]) + "}"
	}
	return out
}

func jsonFieldPresent(raw json.RawMessage, field string) bool {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	_, ok := object[field]
	return ok
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
