package shared

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// UsageStripSnapshot 为 input strip 右侧最近一次 SSE usage 快照。
type UsageStripSnapshot struct {
	PromptTokens     int
	CompletionTokens int
	CacheHitTokens   int
	HasData          bool
}

// ParseUsageStrip 从 SSE usage 事件 data 解析 strip 快照（JSON 数字可能是 float64）。
func ParseUsageStrip(data map[string]any) UsageStripSnapshot {
	if data == nil {
		return UsageStripSnapshot{}
	}
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
	return UsageStripSnapshot{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		CacheHitTokens:   hit,
		HasData:          true,
	}
}

// FormatInputStripUsage 格式化 ↑上行 ↓下行 与 cache hit（无数据时返回空串）。
func FormatInputStripUsage(s UsageStripSnapshot) string {
	if !s.HasData {
		return ""
	}
	text := fmt.Sprintf("↑%s ↓%s", formatCompactCount(s.PromptTokens), formatCompactCount(s.CompletionTokens))
	if s.CacheHitTokens > 0 {
		text += fmt.Sprintf(" · hit %s", formatCompactCount(s.CacheHitTokens))
	}
	return text
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
