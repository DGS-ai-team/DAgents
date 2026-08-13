package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
)

const maxMCPResultRunes = 200000

// MCPTool is the Registry-facing projection of a remote MCP tool.
type MCPTool struct {
	Name        string
	Description string
	Parameters  map[string]any
	Call        func(context.Context, json.RawMessage) (mcp.CallResult, error)
}

func (r *Registry) SetMCPTools(remoteTools []MCPTool) error {
	if r == nil {
		return fmt.Errorf("registry is nil")
	}
	for name := range r.mcpTools {
		delete(r.handlers, name)
	}
	r.mcpTools = make(map[string]MCPTool, len(remoteTools))
	for _, remote := range remoteTools {
		name := strings.TrimSpace(remote.Name)
		if !strings.HasPrefix(name, "mcp__") || remote.Call == nil {
			return fmt.Errorf("invalid MCP tool %q", name)
		}
		if _, exists := r.handlers[name]; exists {
			return fmt.Errorf("tool name collision: %q", name)
		}
		remote.Name = name
		if remote.Parameters == nil {
			remote.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		r.mcpTools[name] = remote
		tool := remote
		r.handlers[name] = func(ctx context.Context, args json.RawMessage) (string, error) {
			_, cleaned := ParseToolCallArguments(string(args))
			result, err := tool.Call(ctx, json.RawMessage(cleaned))
			output := formatMCPResult(result)
			if err != nil {
				return output, err
			}
			if result.IsError {
				return output, fmt.Errorf("MCP tool returned an error")
			}
			return output, nil
		}
	}
	return nil
}

func (r *Registry) mcpToolDefs() []ToolDef {
	if r == nil || len(r.mcpTools) == 0 {
		return nil
	}
	names := make([]string, 0, len(r.mcpTools))
	for name := range r.mcpTools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]ToolDef, 0, len(names))
	for _, name := range names {
		remote := r.mcpTools[name]
		out = append(out, ToolDef{
			Type: "function",
			Function: FunctionDef{
				Name:        name,
				Description: remote.Description,
				Parameters:  injectCallPurposeParam(cloneToolSchema(remote.Parameters)),
			},
		})
	}
	return out
}

func cloneToolSchema(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return out
}

func formatMCPResult(result mcp.CallResult) string {
	parts := make([]string, 0, len(result.Content)+1)
	for _, block := range result.Content {
		switch strings.ToLower(strings.TrimSpace(block.Type)) {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		default:
			if len(block.Raw) > 0 {
				if raw, err := json.Marshal(block.Raw); err == nil {
					parts = append(parts, string(raw))
				}
			}
		}
	}
	if len(parts) == 0 && result.StructuredContent != nil {
		if raw, err := json.Marshal(result.StructuredContent); err == nil {
			parts = append(parts, string(raw))
		}
	}
	output := strings.Join(parts, "\n")
	if len([]rune(output)) <= maxMCPResultRunes {
		return output
	}
	runes := []rune(output)
	return string(runes[:maxMCPResultRunes]) + "\n[output truncated by Node]"
}
