package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

func childAgentToolDefs() []ToolDef {
	return []ToolDef{
		createTemporaryAgentToolDef(),
		waitTemporaryAgentsToolDef(),
		temporaryAgentStatusToolDef(),
		cancelTemporaryAgentToolDef(),
	}
}

func createTemporaryAgentToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "create_temporary_agent",
			Description: "创建同进程临时 Agent（temporary agent）执行自包含子任务。" +
				"wait=true 时阻塞至完成。须在 task 中提供完整上下文，并通过 allowed_tools 指定其可用工具（仅限你当前拥有的工具子集）。" +
				"可通过 skill_names 在创建时预加载 skills（与 load_skills 同名语义，子 Agent 运行期不可再加载 skills）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task": map[string]any{
						"type":        "string",
						"description": "临时 Agent 首条 user 任务（必填，须自包含，含角色与约束）",
					},
					"purpose": map[string]any{"type": "string", "description": "短说明（必填，用于日志与 UI）"},
					"allowed_tools": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "临时 Agent 可用工具子集，须在父可下放列表内；默认 read_file、glob_files、grep_file、bash_run",
					},
					"skill_names": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "创建时预加载的 skills 名称（可选）；directory_name 须对应 `<runtime_root>/skills/` 下的子目录，整组替换语义同 load_skills",
					},
					"ttl_seconds": map[string]any{"type": "integer", "description": "临时 Agent 生命周期（秒）"},
					"max_turns":   map[string]any{"type": "integer", "description": "临时 Agent 最大回合数"},
					"wait":        map[string]any{"type": "boolean", "description": "true 时阻塞等待临时 Agent 结果"},
				},
				"required":             []string{"task", "purpose"},
				"additionalProperties": false,
			}),
		},
	}
}

func waitTemporaryAgentsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "wait_temporary_agents",
			Description: "等待一个或多个临时 Agent（temporary agent）到达终态并汇总。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"child_agent_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "临时 Agent 的 session id 列表（create_temporary_agent 返回）",
					},
					"timeout_seconds": map[string]any{"type": "integer", "description": "最长等待秒数，0 表示立即返回当前快照"},
					"fail_fast":       map[string]any{"type": "boolean", "description": "任一进入失败终态时提前返回"},
				},
				"required":             []string{"child_agent_ids"},
				"additionalProperties": false,
			}),
		},
	}
}

func temporaryAgentStatusToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "temporary_agent_status",
			Description: "非阻塞查询临时 Agent（temporary agent）状态。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"child_agent_ids": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "临时 Agent 的 session id 列表",
					},
				},
				"required":             []string{"child_agent_ids"},
				"additionalProperties": false,
			}),
		},
	}
}

func cancelTemporaryAgentToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "cancel_temporary_agent",
			Description: "取消仍在运行的临时 Agent（temporary agent）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"child_agent_id": map[string]any{
						"type":        "string",
						"description": "临时 Agent 的 session id",
					},
					"reason": map[string]any{"type": "string", "description": "取消原因（可选）"},
				},
				"required":             []string{"child_agent_id"},
				"additionalProperties": false,
			}),
		},
	}
}

// RegisterChildAgentToolStubs 注册临时 Agent 工具占位（执行由 orchestrator 处理）。
func (r *Registry) RegisterChildAgentToolStubs() {
	for _, name := range []string{
		"create_temporary_agent", "wait_temporary_agents", "temporary_agent_status", "cancel_temporary_agent",
	} {
		n := name
		r.handlers[n] = func(context.Context, json.RawMessage) (string, error) {
			return "", fmt.Errorf("%s must be handled by orchestrator", n)
		}
	}
}
