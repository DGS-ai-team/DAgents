package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type globFilesArgs struct {
	Directory   string `json:"directory"`
	GlobPattern string `json:"glob_pattern"`
	Offset      *int   `json:"offset"`
	MaxResults  *int   `json:"max_results"`
	IncludeDirs bool   `json:"include_dirs"`
}

func globFilesToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "glob_files",
			Description: descFSPathConvention + " 在指定目录下按 glob 列举匹配的路径，不读取文件内容。" +
				"glob_pattern 相对 directory，支持 *、?、** 递归；可用 offset/max_results 分页。",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": map[string]any{
						"type":        "string",
						"description": "相对 fs_root 的起始目录（必填）；传 . 表示工作区根",
					},
					"glob_pattern": map[string]any{
						"type":        "string",
						"description": "文件名 glob（必填），如 *.go、**/*.yaml；相对 directory",
					},
					"offset": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"description": "跳过前 N 条结果（可选，默认 0）",
					},
					"max_results": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "本页最多返回条数（可选，默认 100）",
					},
					"include_dirs": map[string]any{
						"type":        "boolean",
						"description": "是否包含匹配的目录项，默认 false（仅文件）",
					},
				},
				"required":             []string{"directory", "glob_pattern"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execGlobFiles(_ context.Context, raw json.RawMessage) (string, error) {
	var args globFilesArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	offset := 0
	if args.Offset != nil && *args.Offset >= 0 {
		offset = *args.Offset
	}
	maxResults := defaultGlobMaxResults
	if args.MaxResults != nil && *args.MaxResults >= 1 {
		maxResults = *args.MaxResults
	}

	matches, total, err := r.collectGlobMatches(args.Directory, args.GlobPattern, globCollectOptions{
		includeDirs: args.IncludeDirs,
		offset:      offset,
		maxResults:  maxResults,
	})
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}

	dir := strings.TrimSpace(args.Directory)
	if dir == "" {
		dir = "."
	}
	nextOffset := "无"
	hasLater := false
	if offset+len(matches) < total {
		hasLater = true
		nextOffset = fmt.Sprintf("%d", offset+len(matches))
	}

	header := []string{
		fmt.Sprintf("directory: %s", dir),
		fmt.Sprintf("glob_pattern: %q", strings.TrimSpace(args.GlobPattern)),
		fmt.Sprintf("total_matches: %d", total),
		fmt.Sprintf("showing: %d-%d", offset+1, offset+len(matches)),
		fmt.Sprintf("next_offset: %s", nextOffset),
		fmt.Sprintf("后方是否还有结果: %s", yesNo(hasLater)),
		"---",
	}
	body := strings.Join(matches, "\n")
	if body == "" {
		body = "(无匹配)"
	}
	return strings.Join(header, "\n") + "\n" + body, nil
}
