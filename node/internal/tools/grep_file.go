package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

type grepFileArgs struct {
	Path          string  `json:"path"`
	Pattern       string  `json:"pattern"`
	IndexOffset   *int    `json:"index_offset"`
	CountLimit    *int    `json:"count_limit"`
	ContextLines  *int    `json:"context_lines"`
	CaseSensitive bool    `json:"case_sensitive"`
	Literal       bool    `json:"literal"`
	Encoding      *string `json:"encoding"`
}

func grepFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "grep_file",
			Description: "在单个文件内按行搜索文本内容（正则或字面量），分页返回命中行及上下文。" +
				"path 须为文件路径；pattern 匹配行内文本，不是文件名。",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "相对 fs_root 的文件路径（必填，必须是文件）",
					},
					"pattern": map[string]any{
						"type":        "string",
						"description": "行内容匹配表达式（必填）；literal=true 时按普通字符串匹配",
					},
					"index_offset": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"description": "跳过前 N 个命中（可选，默认 0）",
					},
					"count_limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "本页最多展示命中数（可选，默认 5）",
					},
					"context_lines": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"maximum":     maxSearchContextLines,
						"description": "每处命中前后展示的上下文行数（可选，默认 10）",
					},
					"case_sensitive": map[string]any{
						"type":        "boolean",
						"description": "是否大小写敏感，默认 true",
					},
					"literal": map[string]any{
						"type":        "boolean",
						"description": "是否把 pattern 当普通字符串而非正则，默认 false",
					},
					"encoding": fileEncodingToolProperty(),
				},
				"required":             []string{"path", "pattern"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execGrepFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args grepFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	re, err := compileLinePattern(args.Pattern, args.Literal, args.CaseSensitive)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	opt := lineSearchOptions{
		rawPattern:    args.Pattern,
		literal:       args.Literal,
		caseSensitive: args.CaseSensitive,
		contextLines:  defaultSearchContextLines,
	}
	if args.IndexOffset != nil && *args.IndexOffset >= 0 {
		opt.indexOffset = *args.IndexOffset
	}
	if args.CountLimit != nil && *args.CountLimit >= 1 {
		opt.countLimit = *args.CountLimit
	}
	if args.ContextLines != nil {
		opt.contextLines = *args.ContextLines
	}
	opt.fileEncoding = r.resolveFileEncoding(args.Encoding)
	return r.grepSingleFile(args.Path, re, opt)
}

// execSearchFile 保留旧工具名 handler，与 grep_file 行为一致。
func (r *Registry) execSearchFile(ctx context.Context, raw json.RawMessage) (string, error) {
	return r.execGrepFile(ctx, raw)
}
