package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

const (
	defaultLineOffset         = 1
	defaultLineLimit          = 100
	defaultSearchIndexOffset  = 0
	defaultSearchCountLimit   = 5
	defaultSearchContextLines = 10
	maxSearchContextLines     = 50
	maxSearchHitIndexes       = 10000
	// defaultFSMaxOutputTokens fs 工具（read_file 正文、grep 输出）单页 token 预算（DeepSeek 粗算）。
	// 与默认 line_limit=100 匹配，避免单页过早截断。
	defaultFSMaxOutputTokens = 3000
	defaultReadMaxTokens     = defaultFSMaxOutputTokens
	defaultSearchMaxTokens   = defaultFSMaxOutputTokens
)

var textSuffixes = map[string]struct{}{
	".txt": {}, ".md": {}, ".py": {}, ".json": {}, ".jsonl": {}, ".html": {}, ".yaml": {}, ".yml": {},
	".toml": {}, ".ini": {}, ".cfg": {}, ".sh": {}, ".bat": {}, ".ps1": {},
	".log": {}, ".csv": {}, ".ts": {}, ".tsx": {}, ".js": {}, ".jsx": {}, ".go": {},
}

func isTextReadable(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := textSuffixes[ext]; ok {
		return true
	}
	return ext == ""
}

func windowFromTotal(total, lineOffset, lineLimit int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	if lineOffset > 0 {
		start = lineOffset - 1
		if start < 0 {
			start = 0
		}
	} else {
		start = total + lineOffset
		if start < 0 {
			start = 0
		}
	}
	end = start + lineLimit
	if end > total {
		end = total
	}
	return start, end
}

func formatNumberedLines(lines []string, startLine int) []string {
	out := make([]string, len(lines))
	for i, line := range lines {
		// 用空格对齐行号，避免 \t 在 Windows 终端中显示为方框并压住正文。
		out[i] = fmt.Sprintf("%6d  %s", startLine+i, line)
	}
	return out
}

// clipReadBodyToTokenBudget clips a read_file window and returns the first
// original file line whose content is not fully represented in the clipped
// prefix. The token budget is applied after line-window selection, so the
// unread line may be inside the requested window rather than after it.
func clipReadBodyToTokenBudget(body string, maxTokens float64, firstLine int) (string, bool, int) {
	clipped, truncated := tokens.ClipToTokenBudget(body, maxTokens)
	if !truncated {
		return body, false, 0
	}

	line := firstLine + strings.Count(clipped, "\n")
	fullRunes := []rune(body)
	clippedRunes := []rune(clipped)
	if len(clippedRunes) < len(fullRunes) && fullRunes[len(clippedRunes)] == '\n' {
		// The complete current line was included but its separator was not.
		// Resume at the following line instead of rereading that complete line.
		line++
	}
	return clipped, true, line
}

func applyMaxTokensToOutput(full string, maxTokens float64) (string, bool) {
	clipped, truncated := tokens.ClipToTokenBudget(full, maxTokens)
	if !truncated {
		return full, false
	}
	return clipped + fmt.Sprintf(
		"\n\n[TRUNCATED] 输出超过约 %d tokens（DeepSeek 粗算）；请减小 count_limit 或缩小检索范围，并使用 next_index_offset 翻页。",
		int(maxTokens+0.5),
	), true
}

func mergeLineRanges(ranges [][2]int) [][2]int {
	if len(ranges) == 0 {
		return nil
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i][0] < ranges[j][0] })
	merged := [][2]int{ranges[0]}
	for _, rg := range ranges[1:] {
		prev := merged[len(merged)-1]
		if rg[0] <= prev[1] {
			if rg[1] > prev[1] {
				merged[len(merged)-1] = [2]int{prev[0], rg[1]}
			}
		} else {
			merged = append(merged, rg)
		}
	}
	return merged
}

func loadLinesForRanges(all []string, ranges [][2]int) map[int]string {
	out := make(map[int]string)
	if len(ranges) == 0 {
		return out
	}
	maxEnd := 0
	for _, rg := range ranges {
		if rg[1] > maxEnd {
			maxEnd = rg[1]
		}
	}
	for i := 0; i < len(all) && i < maxEnd; i++ {
		for _, rg := range ranges {
			if i >= rg[0] && i < rg[1] {
				out[i] = all[i]
				break
			}
		}
	}
	return out
}

func scanRegexHits(lines []string, re *regexp.Regexp) (stored []int, totalHits int, capped bool) {
	for i, line := range lines {
		if re.MatchString(line) {
			totalHits++
			if len(stored) < maxSearchHitIndexes {
				stored = append(stored, i)
			}
		}
	}
	return stored, totalHits, totalHits > len(stored)
}

func formatSearchBlocks(
	shownHits, hitIndexes []int,
	totalHits int,
	lineMap map[int]string,
	contextLines, totalFileLines int,
) []string {
	if len(shownHits) == 0 {
		return nil
	}
	var rawRanges [][2]int
	for _, h := range shownHits {
		start := h - contextLines
		if start < 0 {
			start = 0
		}
		end := h + contextLines + 1
		if end > totalFileLines {
			end = totalFileLines
		}
		rawRanges = append(rawRanges, [2]int{start, end})
	}
	merged := mergeLineRanges(rawRanges)
	hitRank := make(map[int]int, len(hitIndexes))
	for i, idx := range hitIndexes {
		hitRank[idx] = i + 1
	}
	var blocks []string
	for _, rg := range merged {
		var ranks, lineNums []string
		for _, h := range shownHits {
			if h >= rg[0] && h < rg[1] {
				ranks = append(ranks, fmt.Sprintf("#%d", hitRank[h]))
				lineNums = append(lineNums, fmt.Sprintf("%d", h+1))
			}
		}
		var ctxLines []string
		for i := rg[0]; i < rg[1]; i++ {
			ctxLines = append(ctxLines, lineMap[i])
		}
		readOffset := rg[0] + 1
		readLimit := rg[1] - rg[0]
		if readLimit < 1 {
			readLimit = 1
		}
		blocks = append(blocks, fmt.Sprintf(
			"命中 %s/%d（原文行 %s）\n建议 read_file: line_offset=%d, line_limit=%d\n上下文:\n%s",
			strings.Join(ranks, "、"),
			totalHits,
			strings.Join(lineNums, ", "),
			readOffset,
			readLimit,
			strings.Join(ctxLines, "\n"),
		))
	}
	return blocks
}

func fileMtimeText(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	t := info.ModTime()
	return fmt.Sprintf("%s（unix %.6f）", t.Format(time.RFC3339), float64(t.Unix())+float64(t.Nanosecond())/1e9)
}
