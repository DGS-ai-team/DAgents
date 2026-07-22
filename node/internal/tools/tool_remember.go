package tools

// rememberToolDef 供 LLM 写入长期记忆；实际由 turn 编排器处理（含冲突检测与 HITL）。
func rememberToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "remember",
			Description: "将重要信息写入长期记忆（结构化条目，持久化在数据库；作用域由 Agent 配置为全局或独立）。若与已有记忆冲突，会请用户确认保留方式后再写入。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"information": map[string]any{
						"type":        "string",
						"description": "需要记住的新信息（必填）",
					},
				},
				"required":             []string{"information"},
				"additionalProperties": false,
			}),
		},
	}
}

// IsRemember 判断工具名是否为 remember。
func IsRemember(name string) bool {
	return name == "remember"
}
