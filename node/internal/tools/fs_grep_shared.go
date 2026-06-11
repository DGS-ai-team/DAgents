package tools

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type lineSearchOptions struct {
	rawPattern     string
	literal        bool
	caseSensitive  bool
	indexOffset    int
	countLimit     int
	contextLines   int
	maxOutputBytes int
	fileEncoding   string
}

func compileLinePattern(rawPat string, literal, caseSensitive bool) (*regexp.Regexp, error) {
	rawPat = strings.TrimSpace(rawPat)
	if rawPat == "" {
		return nil, fmt.Errorf("pattern 不能为空")
	}
	pattern := rawPat
	if literal {
		pattern = regexp.QuoteMeta(rawPat)
	}
	flags := ""
	if !caseSensitive {
		flags = "(?i)"
	}
	return regexp.Compile(flags + pattern)
}

func normalizeLineSearchOptions(args lineSearchOptions) lineSearchOptions {
	out := args
	if out.indexOffset < 0 {
		out.indexOffset = 0
	}
	if out.countLimit <= 0 {
		out.countLimit = defaultSearchCountLimit
	}
	if out.contextLines < 0 {
		out.contextLines = 0
	}
	if out.contextLines > maxSearchContextLines {
		out.contextLines = maxSearchContextLines
	}
	if out.maxOutputBytes <= 0 {
		out.maxOutputBytes = defaultSearchMaxBytes
	}
	return out
}

func formatMatchMode(literal, caseSensitive bool) string {
	if literal {
		if caseSensitive {
			return "literal / case-sensitive"
		}
		return "literal / ignore-case"
	}
	if caseSensitive {
		return "regex / case-sensitive"
	}
	return "regex / ignore-case"
}

func grepFileContentAtPath(displayPath string, lines []string, re *regexp.Regexp, opt lineSearchOptions) (string, error) {
	opt = normalizeLineSearchOptions(opt)
	hitIndexes, totalHits, indexCapped := scanRegexHits(lines, re)
	totalFileLines := len(lines)

	pageableTotal := len(hitIndexes)
	header := []string{
		fmt.Sprintf("文件: %s", displayPath),
		fmt.Sprintf("pattern: %q", opt.rawPattern),
		fmt.Sprintf("匹配模式: %s", formatMatchMode(opt.literal, opt.caseSensitive)),
		fmt.Sprintf("全文件命中数: %d", totalHits),
	}
	if indexCapped {
		header = append(header, fmt.Sprintf(
			"命中索引列表: 仅保留前 %d 处供 index_offset 分页；全文件命中数仍为上值。",
			maxSearchHitIndexes,
		))
	}

	io := opt.indexOffset
	cl := opt.countLimit
	var shownHits []int
	pageDesc := "无"
	nextIndex := "无"
	hasEarlier, hasLater := false, false
	if pageableTotal > 0 {
		if io >= pageableTotal {
			io = pageableTotal - 1
		}
		if cl > pageableTotal-io {
			cl = pageableTotal - io
		}
		end := io + cl
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
			start := h - opt.contextLines
			if start < 0 {
				start = 0
			}
			end := h + opt.contextLines + 1
			if end > totalFileLines {
				end = totalFileLines
			}
			rawRanges = append(rawRanges, [2]int{start, end})
		}
		lineMap := loadLinesForRanges(lines, mergeLineRanges(rawRanges))
		blocks = formatSearchBlocks(shownHits, hitIndexes, totalHits, lineMap, opt.contextLines, totalFileLines)
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
	out, truncated := applyMaxBytesToOutput(fullOut, opt.maxOutputBytes)
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

func (r *Registry) grepSingleFile(relPath string, re *regexp.Regexp, opt lineSearchOptions) (string, error) {
	absPath, err := r.resolvePath(relPath)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("ERROR: 文件不存在：%q", relPath), nil
		}
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	if info.IsDir() {
		return fmt.Sprintf(
			"ERROR: path 是目录 %q。按文件名列举请用 glob_files（directory + glob_pattern）；在目录树内搜内容请用 grep_files（directory + pattern）。",
			relPath,
		), nil
	}
	lines, err := readAllLines(absPath, opt.fileEncoding)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err), nil
	}
	return grepFileContentAtPath(relPath, lines, re, opt)
}
