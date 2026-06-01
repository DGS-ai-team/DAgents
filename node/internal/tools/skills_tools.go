package tools

// skill tool definitions — 执行由 turn 编排器处理（需读写 session loaded_skills）。

func loadSkillsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "load_skills",
			Description: "设置当前会话已加载 skills（整组替换；空数组表示清空）",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_names": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "技能名称数组（必填）；整组替换当前已加载 skills，空数组表示清空；名称须与 skills 根目录下子目录名一致",
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
			Description: "从当前会话已加载 skills 中移除指定项",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_names": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "要卸载的技能名称数组（必填）；不存在或未加载的名称会被忽略",
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
			Description: "清空当前会话已加载 skills",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"required":             []string{},
				"additionalProperties": false,
			}),
		},
	}
}

// IsSkillTool 判断是否为 session skills 工具。
func IsSkillTool(name string) bool {
	switch name {
	case "load_skills", "unload_skills", "clear_skills":
		return true
	default:
		return false
	}
}
