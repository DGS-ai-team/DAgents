package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type searchFileArgs struct {
	Path          string `json:"path"`
	Pattern       string `json:"pattern"`
	IndexOffset   *int   `json:"index_offset"`
	CountLimit    *int   `json:"count_limit"`
	ContextLines  *int   `json:"context_lines"`
	CaseSensitive bool   `json:"case_sensitive"`
	Literal       bool   `json:"literal"`
}

func searchFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "search_file",
			Description: "在 fs_root 内按正则逐行检索，分页返回命中及上下文",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "工作区内相对 fs_root 的路径（必填）",
					},
					"pattern": map[string]any{
						"type":        "string",
						"description": "正则表达式（必填，非空）；literal=true 时按普通字符串匹配",
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
				},
				"required":             []string{"path", "pattern"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execSearchFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args searchFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := r.resolvePath(args.Path)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("ERROR: 文件不存在：%q", args.Path), nil
		}
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	if info.IsDir() {
		return fmt.Sprintf("ERROR: 目标是目录，无法搜索：%q", args.Path), nil
	}
	rawPat := strings.TrimSpace(args.Pattern)
	if rawPat == "" {
		return "ERROR: pattern 不能为空。", nil
	}

	pattern := rawPat
	if args.Literal {
		pattern = regexp.QuoteMeta(rawPat)
	}
	flags := ""
	if !args.CaseSensitive {
		flags = "(?i)"
	}
	re, err := regexp.Compile(flags + pattern)
	if err != nil {
		return fmt.Sprintf("ERROR: 正则无效: %v", err), nil
	}

	lines, err := readAllLines(path)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	hitIndexes, totalHits, indexCapped := scanRegexHits(lines, re)
	totalFileLines := len(lines)

	io := defaultSearchIndexOffset
	if args.IndexOffset != nil && *args.IndexOffset >= 0 {
		io = *args.IndexOffset
	}
	cl := defaultSearchCountLimit
	if args.CountLimit != nil && *args.CountLimit >= 1 {
		cl = *args.CountLimit
	}
	ctxLines := defaultSearchContextLines
	if args.ContextLines != nil {
		ctxLines = *args.ContextLines
		if ctxLines < 0 {
			ctxLines = 0
		}
		if ctxLines > maxSearchContextLines {
			ctxLines = maxSearchContextLines
		}
	}

	pageableTotal := len(hitIndexes)
	header := []string{
		fmt.Sprintf("文件: %s", path),
		fmt.Sprintf("pattern: %q", rawPat),
	}
	if args.Literal {
		if args.CaseSensitive {
			header = append(header, "匹配模式: literal / case-sensitive")
		} else {
			header = append(header, "匹配模式: literal / ignore-case")
		}
	} else {
		if args.CaseSensitive {
			header = append(header, "匹配模式: regex / case-sensitive")
		} else {
			header = append(header, "匹配模式: regex / ignore-case")
		}
	}
	header = append(header, fmt.Sprintf("全文件命中数: %d", totalHits))
	if indexCapped {
		header = append(header, fmt.Sprintf(
			"命中索引列表: 仅保留前 %d 处供 index_offset 分页；全文件命中数仍为上值。",
			maxSearchHitIndexes,
		))
	}

	var shownHits []int
	pageDesc := "无"
	nextIndex := "无"
	hasEarlier, hasLater := false, false
	if pageableTotal > 0 {
		if io >= pageableTotal {
			io = pageableTotal - 1
		}
		if io < 0 {
			io = 0
		}
		if cl > pageableTotal {
			cl = pageableTotal
		}
		end := io + cl
		if end > pageableTotal {
			end = pageableTotal
		}
		shownHits = hitIndexes[io:end]
		hasEarlier = io > 0
		hasLater = end < pageableTotal
		pageDesc = fmt.Sprintf("第 %d-%d 处", io+1, end)
		if hasLater {
			nextIndex = fmt.Sprintf("%d", end)
		}
	}

	var blocks []string
	if len(shownHits) > 0 {
		var rawRanges [][2]int
		for _, h := range shownHits {
			start := h - ctxLines
			if start < 0 {
				start = 0
			}
			end := h + ctxLines + 1
			if end > totalFileLines {
				end = totalFileLines
			}
			rawRanges = append(rawRanges, [2]int{start, end})
		}
		lineMap := loadLinesForRanges(lines, mergeLineRanges(rawRanges))
		blocks = formatSearchBlocks(shownHits, hitIndexes, totalHits, lineMap, ctxLines, totalFileLines)
	}

	header = append(header,
		fmt.Sprintf("本页命中: %s / 可分页 %d 处", pageDesc, pageableTotal),
		fmt.Sprintf("next_index_offset: %s", nextIndex),
		fmt.Sprintf("前方是否还有命中: %s", yesNo(hasEarlier)),
		fmt.Sprintf("后方是否还有命中: %s", yesNo(hasLater)),
		"---",
	)
	fullOut := strings.Join(header, "\n")
	if len(blocks) > 0 {
		fullOut = fullOut + "\n\n" + strings.Join(blocks, "\n\n")
	}
	out, truncated := applyMaxBytesToOutput(fullOut, defaultSearchMaxBytes)
	if truncated {
		out += "\nnext_index_offset: " + nextIndex
	}
	return out, nil
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}
