package tools

// skill tool definitions — 执行由 turn 编排器处理（需读写 session loaded_skills）。

func loadSkillsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "load_skills",
			Description: "按名称整组替换当前会话已启用的 skills，并同步注册各 skill 的 hooks（unload/clear 时移除）。会话状态和 hooks 立即更新；当前模型请求保持稳定，skill 正文在显式变更后的下一个模型 Step 以独立的会话上下文消息生效。" +
				"整组替换当前已加载列表（非追加）；skill_names 传 [] 清空。" +
				"一次可加载多个 name，数量受配置上限约束；返回 requested、loaded_skills、rejected、session_state_applied_boundary、model_context_applied_boundary、hooks_status、hooks_loaded 和 hooks_failed；不再需要时用 unload_skills 或 clear_skills。" +
				"注意：模型只有在正文进入模型上下文后才能依赖 skill.md 内容；如果需要修改文件，不要直接改动当前已加载 skill 文件。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_names": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
						"description": "要加载的 skill 名称（必填）。应使用 list_available_skills 返回的 skill_name 或 directory_name；" +
							"directory_name 对应 `<runtime_root>/skills/<directory_name>/`，skill_name 对应 SKILL.md frontmatter 的 name 字段",
					},
				},
				"required":             []string{"skill_names"},
				"additionalProperties": false,
			}),
		},
	}
}

func unloadSkillsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "unload_skills",
			Description: "从当前会话已启用 skills 中移除指定项；会话状态和 hooks 立即更新，当前模型请求保持稳定，移除结果在显式变更后的下一个模型 Step 从模型上下文中生效。返回 loaded_skills、未加载名称的 rejected 诊断以及 hooks_status/hooks_loaded/hooks_failed。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_names": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "要卸载的 skill 名称数组（必填）；不存在或未加载的名称会被忽略",
					},
				},
				"required":             []string{"skill_names"},
				"additionalProperties": false,
			}),
		},
	}
}

func clearSkillsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "clear_skills",
			Description: "清空当前会话已启用的全部 skills，等价于 load_skills([])；会话状态和 hooks 立即更新，当前模型请求保持稳定，清空结果在显式变更后的下一个模型 Step 从模型上下文中生效，并返回 hooks_status/hooks_loaded/hooks_failed。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []string{},
				"additionalProperties": false,
			}),
		},
	}
}

// ListAvailableSkillsToolDef describes the metadata-only discovery tool. It is
// appended by the turn orchestrator when the Skills tool group is visible.
func ListAvailableSkillsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "list_available_skills",
			Description: "查询当前可见的 skills 元数据（名称、目录名和用途摘要），不写入 system prompt，不读取或返回 SKILL.md 正文；" +
				"支持 query 搜索和有限分页，获得名称后再调用 load_skills 加载正文。返回 status、catalog_revision、query、skills、has_more 和 next_cursor。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "可选的名称、目录名或用途关键词；空字符串返回全部可见 skills",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "每页数量，默认 10，最大 20",
						"minimum":     1,
						"maximum":     20,
					},
					"cursor": map[string]any{
						"type":        "string",
						"description": "上一页返回的 next_cursor；首次查询省略",
					},
				},
				"required":             []string{},
				"additionalProperties": false,
			}),
		},
	}
}

// IsSkillTool 判断是否为 session skills 工具。
func IsSkillTool(name string) bool {
	switch name {
	case "load_skills", "unload_skills", "clear_skills", "list_available_skills":
		return true
	default:
		return false
	}
}
