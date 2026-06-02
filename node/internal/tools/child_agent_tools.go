package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

func childAgentToolDefs() []ToolDef {
	return []ToolDef{
		createTemporaryAgentToolDef(),
		waitChildAgentsToolDef(),
		childAgentStatusToolDef(),
		cancelChildAgentToolDef(),
	}
}

func createTemporaryAgentToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "create_temporary_agent",
			Description: "创建临时子 Agent 执行自包含子任务；wait=true 阻塞至完成",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task":           map[string]any{"type": "string", "description": "子 Agent 首条 user 任务（必填，须自包含）"},
					"purpose":        map[string]any{"type": "string", "description": "短说明（必填）"},
					"template_id":    map[string]any{"type": "string", "description": "模板 id，默认 general-helper"},
					"allowed_tools":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"ttl_seconds":    map[string]any{"type": "integer"},
					"max_turns":      map[string]any{"type": "integer"},
					"wait":           map[string]any{"type": "boolean", "description": "true 时阻塞等待子 Agent 结果"},
					"detached":       map[string]any{"type": "boolean"},
				},
				"required":             []string{"task", "purpose"},
				"additionalProperties": false,
			},
		},
	}
}

func waitChildAgentsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "wait_child_agents",
			Description: "等待多个子 Agent 终态并汇总",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"child_session_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"timeout_seconds":   map[string]any{"type": "integer"},
					"fail_fast":         map[string]any{"type": "boolean"},
				},
				"required":             []string{"child_session_ids"},
				"additionalProperties": false,
			},
		},
	}
}

func childAgentStatusToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "child_agent_status",
			Description: "非阻塞查询子 Agent 状态",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"child_session_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required":             []string{"child_session_ids"},
				"additionalProperties": false,
			},
		},
	}
}

func cancelChildAgentToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "cancel_child_agent",
			Description: "取消仍在运行的子 Agent",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"child_session_id": map[string]any{"type": "string"},
					"reason":           map[string]any{"type": "string"},
				},
				"required":             []string{"child_session_id"},
				"additionalProperties": false,
			},
		},
	}
}

// RegisterChildAgentToolStubs 注册子 Agent 工具占位（执行由 orchestrator 处理）。
func (r *Registry) RegisterChildAgentToolStubs() {
	for _, name := range []string{
		"create_temporary_agent", "wait_child_agents", "child_agent_status", "cancel_child_agent",
	} {
		n := name
		r.handlers[n] = func(context.Context, json.RawMessage) (string, error) {
			return "", fmt.Errorf("%s must be handled by orchestrator", n)
		}
	}
}
