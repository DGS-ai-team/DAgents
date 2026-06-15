package toolresult

import (
	"fmt"
	"strings"
)

// Result 为 tool.after_each 对单条 tool 结果的拆分。
type Result struct {
	ForClient  string
	ForHistory string
	SpillPath  string // 相对 FSRoot，供 read_file
	Spilled    bool
}

// Package 将 normalized 全文给 Client；超长则落盘并对 history 做头尾摘要。
func Package(cfg Config, sessionID, toolCallID, toolName, normalized string) (Result, error) {
	cfg = cfg.Normalized()
	text := strings.TrimSpace(normalized)
	if text == "" {
		text = "（空输出）"
	}
	out := Result{ForClient: text, ForHistory: text}
	if !cfg.Enabled || !cfg.appliesTo(toolName) {
		return out, nil
	}
	totalTokens := EstimateTokens(text)
	if totalTokens <= float64(cfg.MaxHistoryTokens) {
		return out, nil
	}
	if cfg.FSRoot == "" {
		return out, fmt.Errorf("toolresult: FSRoot required to spill tool output")
	}
	relPath, absPath, err := spillPaths(cfg, sessionID, toolCallID)
	if err != nil {
		return Result{}, err
	}
	if err := writeSpillFile(absPath, text); err != nil {
		return Result{}, err
	}
	out.Spilled = true
	out.SpillPath = relPath
	out.ForHistory = formatHeadTailWithHint(text, cfg.MaxHistoryTokens, relPath)
	return out, nil
}

func formatHeadTailWithHint(text string, maxTokens int, relPath string) string {
	totalTokens := EstimateTokens(text)
	limit := float64(maxTokens)
	if maxTokens <= 0 || totalTokens <= limit {
		return text
	}
	hintTemplate := "...（已省略约 %d tokens，完整输出已写入 %q，请用 read_file(path=%q, line_offset=1, line_limit=100) 分页读取）..."
	placeholder := fmt.Sprintf(hintTemplate, 0, relPath, relPath)
	hintTokens := EstimateTokens(placeholder) + 4
	budget := limit - hintTokens
	if budget < 1 {
		return fmt.Sprintf(hintTemplate, int(totalTokens+0.5), relPath, relPath)
	}
	headBudget := budget / 2
	tailBudget := budget - headBudget
	head := takeRunesForTokenBudget(text, headBudget)
	tail := takeRunesForTokenBudgetFromEnd(text, tailBudget)
	omitted := totalTokens - EstimateTokens(head) - EstimateTokens(tail)
	if omitted < 0 {
		omitted = 0
	}
	hint := fmt.Sprintf(hintTemplate, int(omitted+0.5), relPath, relPath)
	return head + hint + tail
}

func takeRunesForTokenBudget(s string, maxTokens float64) string {
	if maxTokens <= 0 {
		return ""
	}
	var b strings.Builder
	used := 0.0
	for _, r := range s {
		w := tokenWeight(r)
		if used+w > maxTokens && used > 0 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String()
}

func takeRunesForTokenBudgetFromEnd(s string, maxTokens float64) string {
	if maxTokens <= 0 {
		return ""
	}
	runes := []rune(s)
	var picked []rune
	used := 0.0
	for i := len(runes) - 1; i >= 0; i-- {
		w := tokenWeight(runes[i])
		if used+w > maxTokens && used > 0 {
			break
		}
		picked = append([]rune{runes[i]}, picked...)
		used += w
	}
	return string(picked)
}
