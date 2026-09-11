package tools

import (
	"strings"
)

// Keep tool schemas independent from the memory implementation package. The
// runtime enforces the same limits; duplicating these small contract values
// here avoids an import cycle through the tool registry.
const (
	memorySearchToolLimit      = 6
	memoryGetContentTokenLimit = 1500
)

func memorySearchToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "memory_search",
			Description: "按关键词检索长期记忆。仅在当前上下文没有提供相关记忆，或需要查找更具体的历史事实时调用；结果是受 token 预算限制的预览，若需完整内容请使用 memory_get；不会改写 system prompt。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "要检索的事实、对象或关键词（必填）"},
					"scope": map[string]any{"type": "string", "enum": []string{"agent", "global"}, "description": "检索范围；默认使用 Agent 当前配置"},
					"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": memorySearchToolLimit, "description": "最多返回条数，默认 6；结果总量受 token 预算限制"},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			}),
		},
	}
}

func memoryGetToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "memory_get",
			Description: "按记忆 ID 分页查看一条长期记忆的结构化内容。默认返回受 token 预算限制的一页；如果 has_more=true，请使用 next_offset 继续读取。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":         map[string]any{"type": "string", "description": "memory_search 返回的记忆 ID（必填）"},
					"scope":      map[string]any{"type": "string", "enum": []string{"agent", "global"}, "description": "记忆范围；默认使用 Agent 当前配置"},
					"offset":     map[string]any{"type": "integer", "minimum": 0, "description": "正文分页起始字符位置，首次读取省略，后续使用上一次返回的 next_offset"},
					"max_tokens": map[string]any{"type": "integer", "minimum": 100, "maximum": memoryGetContentTokenLimit, "description": "本页正文最多返回的 token 数，默认 1500"},
				},
				"required":             []string{"id"},
				"additionalProperties": false,
			}),
		},
	}
}

func memoryForgetToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "memory_forget",
			Description: "删除一条长期记忆。只有用户明确要求忘记、删除或纠正某条记忆时才调用；需要先通过 memory_search 或 memory_get 确认 ID。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "要删除的记忆 ID（必填）"},
					"scope":  map[string]any{"type": "string", "enum": []string{"agent", "global"}, "description": "记忆范围；默认使用 Agent 当前配置"},
					"reason": map[string]any{"type": "string", "description": "删除原因，可选"},
				},
				"required":             []string{"id"},
				"additionalProperties": false,
			}),
		},
	}
}

func IsMemorySearch(name string) bool { return strings.TrimSpace(name) == "memory_search" }
func IsMemoryGet(name string) bool    { return strings.TrimSpace(name) == "memory_get" }
func IsMemoryForget(name string) bool { return strings.TrimSpace(name) == "memory_forget" }
func IsMemoryTool(name string) bool {
	return IsMemorySearch(name) || IsMemoryGet(name) || IsMemoryForget(name)
}
