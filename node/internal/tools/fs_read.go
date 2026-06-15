package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type readFileArgs struct {
	Path               string  `json:"path"`
	LineOffset         *int    `json:"line_offset"`
	LineLimit          *int    `json:"line_limit"`
	IncludeLineNumbers bool    `json:"include_line_numbers"`
	Encoding           *string `json:"encoding"`
}

func readFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "read_file",
			Description: descFSPathConvention + " " + descPromptContext + " 按行窗口读取文本文件，大文件用 line_offset/line_limit 分页。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "工作区内相对 fs_root 的路径（必填）；支持无后缀及常见文本后缀（含 .jsonl、.html）",
					},
					"line_offset": map[string]any{
						"type":        "integer",
						"description": "起始行（1-based，默认 1）；非正整数时从文件末尾倒数起算",
					},
					"line_limit": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "本页最多读取行数（默认 100），分页主参数",
					},
					"include_line_numbers": map[string]any{
						"type":        "boolean",
						"description": "是否在正文前加 1-based 行号与 tab，默认 false",
					},
					"encoding": fileEncodingToolProperty(),
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execReadFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args readFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := r.resolvePath(args.Path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("ERROR: 文件不存在：%q", args.Path), nil
		}
		return fmt.Sprintf("ERROR: read_file 失败: %v", err), nil
	}
	if info.IsDir() {
		return fmt.Sprintf("ERROR: 目标是目录，无法读取：%q", args.Path), nil
	}

	offset := defaultLineOffset
	if args.LineOffset != nil {
		offset = *args.LineOffset
	}
	limit := defaultLineLimit
	if args.LineLimit != nil && *args.LineLimit >= 1 {
		limit = *args.LineLimit
	}

	lines, choice, err := r.readTextLinesAt(args.Path, path, args.Encoding)
	if err != nil {
		if _, ok := err.(*encodingDecodeError); ok {
			return fmt.Sprintf("ERROR: read_file 失败: %v", err), nil
		}
		return fmt.Sprintf("ERROR: read_file 失败: %v", err), nil
	}
	total := len(lines)
	start, end := windowFromTotal(total, offset, limit)
	windowLines := lines[start:end]
	hasMoreAfter := end < total

	bodyLines := windowLines
	if args.IncludeLineNumbers {
		bodyLines = formatNumberedLines(windowLines, start+1)
	}
	body := strings.Join(bodyLines, "\n")
	truncateHint := fmt.Sprintf(
		"当前窗口超过约 %d tokens（DeepSeek 粗算）；请减小 line_limit，下一页建议 line_offset=%d（若后方仍有行）。",
		defaultReadMaxTokens, end+1,
	)
	body, tokenTruncated := applyMaxTokensToBody(body, defaultReadMaxTokens, truncateHint)

	nextLine := "无"
	if hasMoreAfter {
		nextLine = fmt.Sprintf("%d", end+1)
	}
	pageStart, pageEnd := start+1, end
	if total == 0 {
		pageStart, pageEnd = 0, 0
	}
	header := []string{
		fmt.Sprintf("文件修改时间: %s", fileMtimeText(path)),
	}
	header = append(header, formatEncodingHeaderLines(choice, choice.GarbledWarning)...)
	header = append(header,
		fmt.Sprintf("文件总行数: %d", total),
		fmt.Sprintf("本页行区间: %d-%d / %d", pageStart, pageEnd, total),
		fmt.Sprintf("next_line_offset: %s", nextLine),
		fmt.Sprintf("后方是否还有未读取行: %s", yesNo(hasMoreAfter)),
		fmt.Sprintf("正文是否包含行号: %s", yesNo(args.IncludeLineNumbers)),
		fmt.Sprintf("本页内容是否因 token 上限截断: %s", yesNo(tokenTruncated)),
		"---",
	)
	return strings.Join(header, "\n") + "\n" + body, nil
}
