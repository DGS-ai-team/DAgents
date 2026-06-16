package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

type searchReplaceArgs struct {
	Path       string  `json:"path"`
	OldString  string  `json:"old_string"`
	NewString  string  `json:"new_string"`
	ReplaceAll bool    `json:"replace_all"`
	Encoding   *string `json:"encoding"`
}

func searchReplaceToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "search_replace",
			Description: "修改已有文件前须先 read_file 核对空白、换行与上下文。用精确子串替换修改文本。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "路径（必填）",
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

	oldLines, choice, err := r.readTextLinesAt(args.Path, path, args.Encoding)
	if err != nil {
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	rawText := strings.Join(oldLines, "\n")
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
	enc := choice.Encoding
	encSrc := choice.Source
	payload, err := encodeFileContent(newText, enc)
	if err != nil {
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return formatSearchReplaceFail(args.Path, err.Error()), nil
	}
	if info, err := os.Stat(path); err == nil {
		r.rememberPathEncoding(args.Path, enc, info.ModTime(), encSrc)
	}
	out := formatSearchReplaceSuccess(replaced, args.OldString, args.NewString, lineHint)
	out, _ = applyMaxTokensToOutput(out, defaultSearchReplaceMaxTokens)
	return out, nil
}

const (
	searchReplaceMaxPreviewBytes = 1500
	searchReplacePreviewMaxLines = 8
	searchReplacePreviewMaxRunes = 400
	// defaultSearchReplaceMaxTokens search_replace 整段 tool 输出 token 上限（含 diff 预览）。
	defaultSearchReplaceMaxTokens = 2000
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
	clipped, truncated := tokens.ClipToTokenBudget(out, defaultSearchReplaceMaxTokens/2)
	if truncated {
		out = clipped + "\n...(diff 预览因 token 上限截断)"
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
