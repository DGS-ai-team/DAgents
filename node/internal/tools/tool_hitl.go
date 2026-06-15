package tools

// askUserInformationToolDef 供 LLM 调用；实际由 turn 编排器发 user_information_required 处理。
func askUserInformationToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "ask_user_information",
			Description: "向用户询问补充信息（选项或自由文本）",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"question": map[string]any{
						"type":        "string",
						"description": "向用户展示的问题（必填）",
					},
					"options": map[string]any{
						"type":        "array",
						"description": "可选项列表，每项含 id/label，可选 value；为空则收集自由文本",
						"items": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"id":    map[string]any{"type": "string", "description": "选项唯一标识"},
								"label": map[string]any{"type": "string", "description": "展示文案"},
								"value": map[string]any{"type": "string", "description": "提交值（可选，默认同 label）"},
							},
							"required":             []string{"id", "label"},
							"additionalProperties": false,
						},
					},
					"allow_multiple": map[string]any{
						"type":        "boolean",
						"description": "是否允许多选（仅 options 非空时有效，默认 false）",
					},
					"placeholder": map[string]any{
						"type":        "string",
						"description": "自由文本输入时的占位提示（可选）",
					},
					"required": map[string]any{
						"type":        "boolean",
						"description": "是否必须回答（默认 true；用户仍可通过 UI 取消）",
					},
				},
				"required":             []string{"question"},
				"additionalProperties": false,
			}),
		},
	}
}

// IsAskUserInformation 判断工具名是否为 ask_user_information。
func IsAskUserInformation(name string) bool {
	return name == "ask_user_information"
}
