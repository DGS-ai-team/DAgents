package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	defaultGrepFilesMaxFiles = 50
	defaultGrepFilesMaxHits  = 10
)

type grepFilesArgs struct {
	Directory     string  `json:"directory"`
	Pattern       string  `json:"pattern"`
	GlobPattern   string  `json:"glob_pattern"`
	HitOffset     *int    `json:"hit_offset"`
	MaxHits       *int    `json:"max_hits"`
	MaxFiles      *int    `json:"max_files"`
	ContextLines  *int    `json:"context_lines"`
	CaseSensitive bool    `json:"case_sensitive"`
	Literal       bool    `json:"literal"`
	Encoding      *string `json:"encoding"`
}

type flatHit struct {
	relPath string
	lineIdx int
}

func grepFilesToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "grep_files",
			Description: "在目录树内先按 glob_pattern 筛选文件，再按 pattern 逐行搜索文本内容，分页返回跨文件命中及上下文。" +
				"directory 为起始目录；pattern 匹配行内文本；glob_pattern 相对 directory，支持 **，默认 **/*。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"directory": map[string]any{
						"type":        "string",
						"description": "起始目录（必填）；传 . 表示工作区根",
					},
					"pattern": map[string]any{
						"type":        "string",
						"description": "行内容匹配表达式（必填）；literal=true 时按普通字符串匹配",
					},
					"glob_pattern": map[string]any{
						"type":        "string",
						"description": "限定扫描哪些文件（可选，默认 **/*）；相对 directory，支持 **",
					},
					"hit_offset": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"description": "跳过前 N 个跨文件命中（可选，默认 0）",
					},
					"max_hits": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "本页最多展示命中数（可选，默认 10）",
					},
					"max_files": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "最多扫描文件数（可选，默认 50）；超出时提示收窄 glob_pattern",
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
				"required":             []string{"directory", "pattern"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execGrepFiles(_ context.Context, raw json.RawMessage) (string, error) {
	var args grepFilesArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	re, err := compileLinePattern(args.Pattern, args.Literal, args.CaseSensitive)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}

	globPat := strings.TrimSpace(args.GlobPattern)
	if globPat == "" {
		globPat = defaultGrepFilesGlob
	}
	maxFiles := defaultGrepFilesMaxFiles
	if args.MaxFiles != nil && *args.MaxFiles >= 1 {
		maxFiles = *args.MaxFiles
	}
	files, scanned, err := r.collectGlobFilePaths(args.Directory, globPat, maxFiles+1)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	fileCapHit := len(files) > maxFiles
	if fileCapHit {
		files = files[:maxFiles]
	}

	var allHits []flatHit
	totalHits := 0
	indexCapped := false
	fileEncArg := args.Encoding
	for _, rel := range files {
		abs, err := r.resolvePath(rel)
		if err != nil {
			continue
		}
		lines, _, err := r.readTextLinesAt(rel, abs, fileEncArg)
		if err != nil {
			continue
		}
		for i, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			totalHits++
			if len(allHits) < maxSearchHitIndexes {
				allHits = append(allHits, flatHit{relPath: rel, lineIdx: i})
			} else {
				indexCapped = true
			}
		}
	}

	hitOffset := 0
	if args.HitOffset != nil && *args.HitOffset >= 0 {
		hitOffset = *args.HitOffset
	}
	maxHits := defaultGrepFilesMaxHits
	if args.MaxHits != nil && *args.MaxHits >= 1 {
		maxHits = *args.MaxHits
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

	pageableTotal := len(allHits)
	dir := strings.TrimSpace(args.Directory)
	if dir == "" {
		dir = "."
	}

	header := []string{
		fmt.Sprintf("directory: %s", dir),
		fmt.Sprintf("glob_pattern: %q", globPat),
		fmt.Sprintf("pattern: %q", strings.TrimSpace(args.Pattern)),
		fmt.Sprintf("匹配模式: %s", formatMatchMode(args.Literal, args.CaseSensitive)),
		fmt.Sprintf("扫描文件数: %d（上限 %d）", len(files), maxFiles),
		fmt.Sprintf("walk 访问文件数: %d", scanned),
		fmt.Sprintf("全树命中数: %d", totalHits),
	}
	if fileCapHit {
		header = append(header, fmt.Sprintf(
			"提示: 已达 max_files=%d，请收窄 glob_pattern 后重试；已扫描结果可能不完整。",
			maxFiles,
		))
	}
	if indexCapped {
		header = append(header, fmt.Sprintf(
			"命中索引列表: 仅保留前 %d 处供 hit_offset 分页；全树命中数仍为上值。",
			maxSearchHitIndexes,
		))
	}

	var shown []flatHit
	pageDesc := "无"
	nextHitOffset := "无"
	hasEarlier, hasLater := false, false
	if pageableTotal > 0 {
		io := hitOffset
		if io >= pageableTotal {
			io = pageableTotal - 1
		}
		end := io + maxHits
		if end > pageableTotal {
			end = pageableTotal
		}
		shown = allHits[io:end]
		hasEarlier = io > 0
		hasLater = end < pageableTotal
		pageDesc = fmt.Sprintf("第 %d-%d 处", io+1, end)
		if hasLater {
			nextHitOffset = fmt.Sprintf("%d", end)
		}
	}

	var blocks []string
	if len(shown) > 0 {
		blocks = formatGrepFilesBlocks(r, shown, allHits, totalHits, ctxLines, args.Encoding)
	}

	header = append(header,
		fmt.Sprintf("本页命中: %s / 可分页 %d 处", pageDesc, pageableTotal),
		fmt.Sprintf("next_hit_offset: %s", nextHitOffset),
		fmt.Sprintf("前方是否还有命中: %s", yesNo(hasEarlier)),
		fmt.Sprintf("后方是否还有命中: %s", yesNo(hasLater)),
		"---",
	)
	fullOut := strings.Join(header, "\n")
	if len(blocks) > 0 {
		fullOut = fullOut + "\n\n" + strings.Join(blocks, "\n\n")
	}
	out, truncated := applyMaxTokensToOutput(fullOut, defaultSearchMaxTokens)
	if truncated {
		out += "\nnext_hit_offset: " + nextHitOffset
	}
	return out, nil
}

func formatGrepFilesBlocks(r *Registry, shown []flatHit, allHits []flatHit, totalHits, ctxLines int, fileEncoding *string) []string {
	hitRank := make(map[flatHit]int, len(allHits))
	for i, h := range allHits {
		hitRank[h] = i + 1
	}

	linesCache := make(map[string][]string)
	var blocks []string
	for _, h := range shown {
		lines, ok := linesCache[h.relPath]
		if !ok {
			abs, err := r.resolvePath(h.relPath)
			if err != nil {
				continue
			}
			var errRead error
			lines, _, errRead = r.readTextLinesAt(h.relPath, abs, fileEncoding)
			if errRead != nil {
				continue
			}
			linesCache[h.relPath] = lines
		}
		start := h.lineIdx - ctxLines
		if start < 0 {
			start = 0
		}
		end := h.lineIdx + ctxLines + 1
		if end > len(lines) {
			end = len(lines)
		}
		ctx := lines[start:end]
		readOffset := start + 1
		readLimit := end - start
		if readLimit < 1 {
			readLimit = 1
		}
		blocks = append(blocks, fmt.Sprintf(
			"文件: %s\n全局命中 #%d/%d（原文行 %d）\n建议 read_file: path=%q, line_offset=%d, line_limit=%d\n上下文:\n%s",
			h.relPath,
			hitRank[h],
			totalHits,
			h.lineIdx+1,
			h.relPath,
			readOffset,
			readLimit,
			strings.Join(ctx, "\n"),
		))
	}
	return blocks
}
