package tools

// skill tool definitions — 执行由 turn 编排器处理（需读写 session loaded_skills）。

func loadSkillsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "load_skills",
			Description: "加载 skills 到当前会话，使后续回合能按 skill 正文执行。" +
				"可用技能目录（system prompt 中列出）仅含 name/description，不含具体步骤；" +
				"当用户任务与某 skill 的 description 匹配时，且该skill未加载，必须先调用本工具加载该 skill，加载成功后下一轮 system prompt 会注入其完整正文。" +
				"整组替换当前已加载列表（非追加）；skill_names 传 [] 清空。" +
				"一次可加载多个 name，数量受配置上限约束；不再需要时用 unload_skills 或 clear_skills。",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_names": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
						"description": "要加载的 skill 名称（必填）。须与可用技能目录或本工具返回的 available_skills[].skill_name 一致；" +
							"通常等于 skills/<name> 目录名（FS_ROOT 下）及 SKILL.md frontmatter 的 name 字段",
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
			Description: "从当前会话已加载 skills 中移除指定项；移除后该 skill 正文不再注入后续 system prompt。",
			Parameters: injectRunInBackgroundParam(map[string]any{
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
			Description: "清空当前会话已加载的全部 skills，等价于 load_skills([])；清空后不再注入任何 skill 正文。",
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
