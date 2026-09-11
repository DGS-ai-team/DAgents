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
					"kind":         map[string]any{"type": "string", "enum": []string{"fact", "preference", "decision", "procedure", "experience", "constraint"}, "description": "记忆类型，可选"},
					"tier":         map[string]any{"type": "string", "enum": []string{"core", "recall"}, "description": "core 会在每个新 Turn 召回；默认 recall"},
					"semantic_key": map[string]any{"type": "string", "description": "稳定的事实键；同键单值会触发冲突确认"},
					"subject":      map[string]any{"type": "string", "description": "事实主体，可选"},
					"predicate":    map[string]any{"type": "string", "description": "事实关系，可选"},
					"cardinality":  map[string]any{"type": "string", "enum": []string{"single", "multiple"}, "description": "同一事实是否允许多个值"},
					"importance":   map[string]any{"type": "integer", "minimum": 0, "maximum": 100, "description": "重要性 0-100，可选"},
					"confidence":   map[string]any{"type": "integer", "minimum": 0, "maximum": 100, "description": "置信度 0-100，可选"},
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
