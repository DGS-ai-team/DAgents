package toolresult

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

// Result 为 tool.after_each 对单条 tool 结果的拆分。
type Result struct {
	ForClient  string
	ForHistory string
	SpillPath  string // 相对 workspace root，供 read_file
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
	totalTokens := tokens.Estimate(text)
	if totalTokens <= float64(cfg.SpillThresholdTokens) {
		return out, nil
	}
	if cfg.WorkspaceRoot == "" {
		return out, fmt.Errorf("toolresult: WorkspaceRoot required to spill tool output")
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
	out.ForHistory = formatHeadTailWithHint(text, cfg.SpillThresholdTokens, relPath)
	return out, nil
}

func formatHeadTailWithHint(text string, maxTokens int, relPath string) string {
	totalTokens := tokens.Estimate(text)
	limit := float64(maxTokens)
	if maxTokens <= 0 || totalTokens <= limit {
		return text
	}
	hintTemplate := "...（已省略约 %d tokens，完整输出已写入 %q，请用 read_file(path=%q, line_offset=1, line_limit=100) 分页读取）..."
	placeholder := fmt.Sprintf(hintTemplate, 0, relPath, relPath)
	hintTokens := tokens.Estimate(placeholder) + 4
	budget := limit - hintTokens
	if budget < 1 {
		return fmt.Sprintf(hintTemplate, int(totalTokens+0.5), relPath, relPath)
	}
	headBudget := budget / 2
	tailBudget := budget - headBudget
	head := tokens.TakePrefixForTokenBudget(text, headBudget)
	tail := tokens.TakeSuffixForTokenBudget(text, tailBudget)
	omitted := totalTokens - tokens.Estimate(head) - tokens.Estimate(tail)
	if omitted < 0 {
		omitted = 0
	}
	hint := fmt.Sprintf(hintTemplate, int(omitted+0.5), relPath, relPath)
	return head + hint + tail
}
