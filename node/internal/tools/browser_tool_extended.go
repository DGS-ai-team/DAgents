package tools

func browserExtendedToolDefs() []ToolDef {
	return []ToolDef{
		browserSearchToolDef(),
		browserGoBackToolDef(),
		browserScrollToolDef(),
		browserFindTextToolDef(),
		browserSwitchTabToolDef(),
		browserCloseTabToolDef(),
		browserExtractToolDef(),
		browserEvaluateToolDef(),
		browserFindElementsToolDef(),
		browserSearchPageToolDef(),
		browserUploadFileToolDef(),
		browserDropdownOptionsToolDef(),
		browserSelectDropdownToolDef(),
	}
}

func browserSearchToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_search",
			Description: "在搜索引擎中查询（duckduckgo/google/bing），等同 browser-use search。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":  map[string]any{"type": "string", "description": "搜索关键词"},
					"engine": map[string]any{"type": "string", "enum": []string{"duckduckgo", "google", "bing"}, "description": "默认 duckduckgo"},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserGoBackToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_go_back",
			Description: "浏览器后退一页。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			}),
		},
	}
}

func browserScrollToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_scroll",
			Description: "滚动页面或指定 index 元素。down=true 向下，pages 为页数（0.5~10）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"down":  map[string]any{"type": "boolean", "description": "true=向下，false=向上，默认 true"},
					"pages": map[string]any{"type": "number", "minimum": 0.5, "maximum": 10, "description": "滚动页数，默认 1"},
					"index": map[string]any{"type": "integer", "minimum": 0, "description": "可选，在指定元素内滚动；0 或未设表示整页"},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func browserFindTextToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_find_text",
			Description: "滚动页面直到指定文本可见。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string", "description": "要定位的文本"},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserSwitchTabToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_switch_tab",
			Description: "切换到另一标签页。tab_id 为 browser_snapshot detail.tabs 中的 4 字符 ID。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tab_id": map[string]any{"type": "string", "minLength": 4, "maxLength": 4},
				},
				"required":             []string{"tab_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserCloseTabToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_close_tab",
			Description: "关闭指定标签页（tab_id 为 4 字符 ID）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tab_id": map[string]any{"type": "string", "minLength": 4, "maxLength": 4},
				},
				"required":             []string{"tab_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserExtractToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "browser_extract",
			Description: "用页面 markdown + 配置中的 LLM 按 query 提取结构化信息（browser-use extract）。" +
				"需 config llm.model 可用。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query":             map[string]any{"type": "string", "description": "提取目标描述"},
					"extract_links":     map[string]any{"type": "boolean", "description": "是否包含链接"},
					"extract_images":    map[string]any{"type": "boolean", "description": "是否包含图片 URL"},
					"start_from_char":   map[string]any{"type": "integer", "minimum": 0, "description": "长页分段起始字符"},
					"already_collected": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "已收集项，避免重复"},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserEvaluateToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_evaluate",
			Description: "在当前页面执行 JavaScript 并返回结果。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code": map[string]any{"type": "string", "description": "JS 表达式或 async 函数体"},
				},
				"required":             []string{"code"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserFindElementsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_find_elements",
			Description: "按 CSS selector 查询 DOM 元素（零 LLM 成本）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"selector":    map[string]any{"type": "string"},
					"attributes":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "description": "默认 50"},
					"include_text": map[string]any{"type": "boolean", "description": "默认 true"},
				},
				"required":             []string{"selector"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserSearchPageToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_search_page",
			Description: "在页面文本中搜索 pattern（类似 grep，零 LLM 成本）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"pattern":        map[string]any{"type": "string"},
					"regex":          map[string]any{"type": "boolean", "description": "默认 false"},
					"case_sensitive": map[string]any{"type": "boolean", "description": "默认 false"},
					"context_chars":  map[string]any{"type": "integer", "minimum": 1, "description": "默认 150"},
					"css_scope":      map[string]any{"type": "string", "description": "限定搜索范围"},
					"max_results":    map[string]any{"type": "integer", "minimum": 1, "description": "默认 25"},
				},
				"required":             []string{"pattern"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserUploadFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_upload_file",
			Description: "向 file input 上传 FS_ROOT 内文件。须先 browser_snapshot 获取 index。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index": map[string]any{"type": "integer", "minimum": 1},
					"path":  map[string]any{"type": "string", "description": "相对 FS_ROOT 或绝对路径"},
				},
				"required":             []string{"index", "path"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserDropdownOptionsToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_dropdown_options",
			Description: "获取下拉框/ARIA 菜单的全部选项。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index": map[string]any{"type": "integer", "minimum": 1},
				},
				"required":             []string{"index"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserSelectDropdownToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_select_dropdown",
			Description: "按选项文本选择 <select> 下拉项。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index": map[string]any{"type": "integer", "minimum": 1},
					"text":  map[string]any{"type": "string", "description": "选项显示文本/值"},
				},
				"required":             []string{"index", "text"},
				"additionalProperties": false,
			}),
		},
	}
}
