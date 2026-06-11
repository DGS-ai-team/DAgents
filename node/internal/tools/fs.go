package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
			Description: "按行窗口读取 fs_root 内文本文件，大文件用 line_offset/line_limit 分页",
			Parameters: injectRunInBackgroundParam(map[string]any{
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

	lines, err := readAllLines(path, r.resolveFileEncoding(args.Encoding))
	if err != nil {
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
		"当前窗口超过配置上限 %d bytes；请减小 line_limit，下一页建议 line_offset=%d（若后方仍有行）。",
		defaultReadMaxBytes, end+1,
	)
	body, byteTruncated := applyMaxBytesToBody(body, defaultReadMaxBytes, truncateHint)

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
		fmt.Sprintf("文件编码: %s", r.resolveFileEncoding(args.Encoding)),
		fmt.Sprintf("文件总行数: %d", total),
		fmt.Sprintf("本页行区间: %d-%d / %d", pageStart, pageEnd, total),
		fmt.Sprintf("next_line_offset: %s", nextLine),
		fmt.Sprintf("后方是否还有未读取行: %s", yesNo(hasMoreAfter)),
		fmt.Sprintf("正文是否包含行号: %s", yesNo(args.IncludeLineNumbers)),
		fmt.Sprintf("本页内容是否因体积上限截断: %s", yesNo(byteTruncated)),
		"---",
	}
	return strings.Join(header, "\n") + "\n" + body, nil
}

type writeFileArgs struct {
	Path     string  `json:"path"`
	Content  string  `json:"content"`
	Encoding *string `json:"encoding"`
}

func writeFileToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "write_file",
			Description: "写入 fs_root 内文本文件（覆盖）",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "工作区内相对 fs_root 的路径（必填）",
					},
					"content": map[string]any{
						"type":        "string",
						"description": "写入全文（必填）；覆盖已有内容",
					},
					"encoding": fileEncodingToolProperty(),
				},
				"required":             []string{"path", "content"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execWriteFile(_ context.Context, raw json.RawMessage) (string, error) {
	var args writeFileArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := r.resolvePath(args.Path)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	enc := r.resolveFileEncoding(args.Encoding)
	payload, err := encodeFileContent(args.Content, enc)
	if err != nil {
		return fmt.Sprintf("ERROR: write_file 失败: %v", err), nil
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s (encoding=%s)", len(payload), args.Path, enc), nil
}

type searchReplaceArgs struct {
	Path        string  `json:"path"`
	OldString   string  `json:"old_string"`
	NewString   string  `json:"new_string"`
	ReplaceAll  bool    `json:"replace_all"`
	Encoding    *string `json:"encoding"`
}

func searchReplaceToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "search_replace",
			Description: "在 fs_root 内用精确子串替换修改文本（改前请先 read_file）",
			Parameters: injectRunInBackgroundParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "工作区内相对 fs_root 的路径（必填）",
					},
					"old_string": map[string]any{
						"type":        "string",
						"description": "须在磁盘文件中精确出现的片段（含空格与换行；勿带行号前缀）",
					},
					"new_string": map[string]any{
						"type":        "string",
						"description": "替换结果；空字符串表示删除 old_string 片段",
					},
					"replace_all": map[string]any{
						"type":        "boolean",
						"description": "是否替换全部匹配，默认 false；为 false 时须恰好 1 处匹配",
					},
					"encoding": fileEncodingToolProperty(),
				},
				"required":             []string{"path", "old_string", "new_string"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execSearchReplace(_ context.Context, raw json.RawMessage) (string, error) {
	var args searchReplaceArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	path, err := r.resolvePath(args.Path)
	if err != nil {
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return formatSearchReplaceFail(args.Path, fmt.Sprintf("文件不存在：%q", args.Path)), nil
		}
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	if info.IsDir() {
		return formatSearchReplaceFail(args.Path, fmt.Sprintf("目标是目录，无法编辑：%q", args.Path)), nil
	}
	if args.OldString == "" {
		return formatSearchReplaceFail(args.Path, "old_string 不能为空。"), nil
	}

	oldText, err := readAllLines(path, r.resolveFileEncoding(args.Encoding))
	if err != nil {
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	rawText := strings.Join(oldText, "\n")
	hitCount := strings.Count(rawText, args.OldString)
	lineHint := formatHitLines(rawText, args.OldString)
	if hitCount == 0 {
		return formatSearchReplaceFail(args.Path, "未找到 old_string（0 处匹配）；请 read_file 核对空白与换行。"), nil
	}
	if !args.ReplaceAll && hitCount != 1 {
		return formatSearchReplaceFail(args.Path, fmt.Sprintf("old_string 匹配 %d 处；请扩大上下文或设 replace_all=true。匹配行: %s", hitCount, lineHint)), nil
	}

	var newText string
	replaced := 0
	if args.ReplaceAll {
		newText = strings.ReplaceAll(rawText, args.OldString, args.NewString)
		replaced = hitCount
	} else {
		newText = strings.Replace(rawText, args.OldString, args.NewString, 1)
		replaced = 1
	}
	if newText == rawText {
		return formatSearchReplaceSuccess(0, args.OldString, args.NewString, lineHint), nil
	}
	enc := r.resolveFileEncoding(args.Encoding)
	payload, err := encodeFileContent(newText, enc)
	if err != nil {
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	return formatSearchReplaceSuccess(replaced, args.OldString, args.NewString, lineHint), nil
}

const (
	searchReplaceMaxPreviewBytes = 1500
	searchReplacePreviewMaxLines = 8
	searchReplacePreviewMaxRunes = 400
)

func formatSearchReplaceSuccess(replaced int, oldStr, newStr, lineHint string) string {
	meta := fmt.Sprintf("成功: 是\n替换次数: %d", replaced)
	preview := formatSearchReplacePreview(oldStr, newStr, replaced, lineHint)
	if preview == "" {
		return meta
	}
	return meta + "\n---\n" + preview
}

func formatSearchReplaceFail(path, msg string) string {
	return fmt.Sprintf("成功: 否\n路径: %s\n错误: %s", path, msg)
}

// formatSearchReplacePreview 仅在多处替换或多行片段时输出局部预览（供模型验收，限流）。
func formatSearchReplacePreview(oldStr, newStr string, replaced int, lineHint string) string {
	multiLine := strings.Contains(oldStr, "\n") || strings.Contains(newStr, "\n")
	if replaced <= 1 && !multiLine {
		return ""
	}
	var b strings.Builder
	switch {
	case replaced > 1:
		fmt.Fprintf(&b, "@@ 共 %d 处相同替换", replaced)
		if lineHint != "" {
			b.WriteString(" · 行 ")
			b.WriteString(lineHint)
		}
		b.WriteString(" @@\n")
	case lineHint != "":
		fmt.Fprintf(&b, "@@ 行 %s @@\n", lineHint)
	}
	b.WriteString(formatDiffSnippet("-", oldStr))
	b.WriteString(formatDiffSnippet("+", newStr))
	out := strings.TrimSpace(b.String())
	if len(out) > searchReplaceMaxPreviewBytes {
		out = out[:searchReplaceMaxPreviewBytes] + "\n...(预览已截断)"
	}
	return out
}

func formatDiffSnippet(prefix, text string) string {
	lines := strings.Split(text, "\n")
	truncated := false
	if len(lines) > searchReplacePreviewMaxLines {
		lines = append(lines[:searchReplacePreviewMaxLines], "...")
		truncated = true
	}
	var b strings.Builder
	runes := 0
	for _, line := range lines {
		lineRunes := []rune(line)
		if runes+len(lineRunes) > searchReplacePreviewMaxRunes {
			remain := searchReplacePreviewMaxRunes - runes
			if remain > 1 {
				line = string(lineRunes[:remain-1]) + "…"
			} else {
				line = "…"
			}
			truncated = true
		}
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteByte('\n')
		runes += len([]rune(line)) + 1
		if truncated {
			break
		}
	}
	return b.String()
}

func formatHitLines(text, needle string) string {
	if needle == "" {
		return "未知"
	}
	lines := strings.Split(text, "\n")
	var hits []string
	for i, line := range lines {
		if strings.Contains(line, needle) {
			hits = append(hits, fmt.Sprintf("%d", i+1))
		}
		if len(hits) >= 8 {
			return strings.Join(hits, "、") + "、..."
		}
	}
	if len(hits) == 0 {
		return "未知"
	}
	return strings.Join(hits, "、")
}

