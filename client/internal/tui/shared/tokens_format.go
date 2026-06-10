package shared

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"
)

// UsageStripSnapshot 为 input strip 右侧最近一次 SSE usage 快照。
type UsageStripSnapshot struct {
	PromptTokens     int
	CompletionTokens int
	CacheHitTokens   int
	CacheHitRate     float64 // [0,1]；-1 表示未知
	ReasoningTokens  int
	HasData          bool
}

// ParseUsageRound 从 SSE usage 事件 data 解析单轮 LLM 用量（round_* 字段）。
func ParseUsageRound(data map[string]any) UsageStripSnapshot {
	if data == nil {
		return UsageStripSnapshot{}
	}
	roundData := map[string]any{
		"prompt_tokens":            data["round_prompt_tokens"],
		"completion_tokens":        data["round_completion_tokens"],
		"prompt_cache_hit_tokens":  data["round_prompt_cache_hit_tokens"],
		"prompt_cached_tokens":     data["round_prompt_cached_tokens"],
		"prompt_cache_hit_rate":    data["round_prompt_cache_hit_rate"],
		"reasoning_tokens":         data["round_reasoning_tokens"],
	}
	if details, ok := data["round_completion_tokens_details"].(map[string]any); ok {
		roundData["completion_tokens_details"] = details
	}
	return parseUsageFields(roundData)
}

// ParseUsageStrip 从 SSE usage 事件 data 解析 turn 累计 strip 快照（JSON 数字可能是 float64）。
func ParseUsageStrip(data map[string]any) UsageStripSnapshot {
	if data == nil {
		return UsageStripSnapshot{}
	}
	return parseUsageFields(data)
}

func parseUsageFields(data map[string]any) UsageStripSnapshot {
	prompt := intFromAny(data["prompt_tokens"])
	completion := intFromAny(data["completion_tokens"])
	hit := intFromAny(data["prompt_cache_hit_tokens"])
	cached := intFromAny(data["prompt_cached_tokens"])
	if hit <= 0 && cached > 0 {
		hit = cached
	}
	if prompt <= 0 && completion <= 0 {
		return UsageStripSnapshot{}
	}
	rate := -1.0
	if v, ok := data["prompt_cache_hit_rate"].(float64); ok && v >= 0 {
		rate = v
	} else if prompt > 0 && hit > 0 {
		rate = float64(hit) / float64(prompt)
		if rate > 1 {
			rate = 1
		}
	}
	reasoning := intFromAny(data["reasoning_tokens"])
	if reasoning <= 0 {
		if details, ok := data["completion_tokens_details"].(map[string]any); ok {
			reasoning = intFromAny(details["reasoning_tokens"])
		}
	}
	return UsageStripSnapshot{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		CacheHitTokens:   hit,
		CacheHitRate:     rate,
		ReasoningTokens:  reasoning,
		HasData:          true,
	}
}

// FormatInlineUsage 格式化 assistant 块尾部的单轮用量短文案（不含终端样式）。
func FormatInlineUsage(s UsageStripSnapshot) string {
	if !s.HasData {
		return ""
	}
	text := fmt.Sprintf(" · ↑%s ↓%s", formatCompactCount(s.PromptTokens), formatCompactCount(s.CompletionTokens))
	if s.ReasoningTokens > 0 {
		text += fmt.Sprintf(" · think %s", formatCompactCount(s.ReasoningTokens))
	}
	return text
}

// StyleInlineUsage 为 inline usage 施加终端浅灰样式（bright black / dim）。
func StyleInlineUsage(suffix string) string {
	if suffix == "" {
		return ""
	}
	return "\033[90m" + suffix + "\033[0m"
}

// FormatInputStripUsage 格式化 ↑上行 ↓下行 与 cache hit（无数据时返回空串）。
func FormatInputStripUsage(s UsageStripSnapshot) string {
	if !s.HasData {
		return ""
	}
	text := fmt.Sprintf("↑%s ↓%s", formatCompactCount(s.PromptTokens), formatCompactCount(s.CompletionTokens))
	if s.CacheHitTokens > 0 {
		if s.CacheHitRate >= 0 {
			text += fmt.Sprintf(" · hit %s (%.0f%%)", formatCompactCount(s.CacheHitTokens), s.CacheHitRate*100)
		} else {
			text += fmt.Sprintf(" · hit %s", formatCompactCount(s.CacheHitTokens))
		}
	}
	if s.ReasoningTokens > 0 {
		text += fmt.Sprintf(" · think %s", formatCompactCount(s.ReasoningTokens))
	}
	return text
}

// FormatInputStripLine 组合左侧状态与右侧 usage/token，按 cell 宽度右对齐；必要时截断左侧，避免 usage 被折行拆开。
func FormatInputStripLine(left, right string, width int) string {
	left = strings.TrimRight(left, " ")
	right = strings.TrimSpace(right)
	if right == "" {
		return left
	}
	if width <= 0 {
		width = 80
	}
	const minGap = 1
	rightW := runewidth.StringWidth(right)
	leftW := runewidth.StringWidth(left)
	if leftW+rightW+minGap > width {
		maxLeft := width - rightW - minGap
		if maxLeft < 0 {
			maxLeft = 0
		}
		left = runewidth.Truncate(left, maxLeft, "…")
		leftW = runewidth.StringWidth(left)
	}
	gap := width - leftW - rightW
	if gap < minGap {
		gap = minGap
	}
	return left + strings.Repeat(" ", gap) + right
}

// FormatInputStripTokens 将 context token 估算值格式化为 input strip 右侧短文案。
//
// 逻辑：
// 1. tokens < 0 表示尚未拉取，返回空串；
// 2. >= 10000 时用一位小数的 k 后缀；
// 3. 其余用千分位整数。
func FormatInputStripTokens(tokens int) string {
	if tokens < 0 {
		return ""
	}
	return fmt.Sprintf("ctx %s", formatCompactCount(tokens))
}

func formatCompactCount(n int) string {
	if n < 0 {
		n = 0
	}
	if n >= 10_000 {
		val := math.Round(float64(n)/100) / 10
		if val == math.Floor(val) {
			return fmt.Sprintf("%.0fk", val)
		}
		return fmt.Sprintf("%.1fk", val)
	}
	return formatThousands(n)
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return max(0, n)
	case int32:
		return max(0, int(n))
	case int64:
		return max(0, int(n))
	case float64:
		return max(0, int(n))
	case float32:
		return max(0, int(n))
	default:
		if v == nil {
			return 0
		}
		i, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
		if err != nil {
			return 0
		}
		return max(0, i)
	}
}

func formatThousands(n int) string {
	if n < 0 {
		n = 0
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	start := len(s) % 3
	if start > 0 {
		out = append(out, s[:start]...)
		if len(s) > start {
			out = append(out, ',')
		}
	}
	for i := start; i < len(s); i += 3 {
		if i > start {
			out = append(out, ',')
		}
		out = append(out, s[i:i+3]...)
	}
	return string(out)
}
