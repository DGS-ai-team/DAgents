package hitl

import (
	"fmt"
	"math"
)

// FormatContextCompression 将 blocking/silent 压缩 SSE 格式化为终端提示行。
func FormatContextCompression(eventType string, data map[string]any) string {
	mode := "blocking"
	if eventType == "context_compression_silent" {
		mode = "silent"
	}
	phase := fmt.Sprint(data["phase"])
	status := fmt.Sprint(data["status"])

	switch mode {
	case "blocking":
		switch phase {
		case "start":
			return fmt.Sprintf("[compression blocking] 上下文压缩进行中（阻塞本轮消息，压缩 %v 条）…", data["compressed_message_count"])
		case "end":
			return formatCompressionEnd("blocking", status, data)
		}
	case "silent":
		switch phase {
		case "start":
			return fmt.Sprintf("[compression silent] 后台静默压缩已开始（目标 %v 条消息）", data["compressed_message_count"])
		case "end":
			return formatCompressionEnd("silent", status, data)
		}
	}
	return fmt.Sprintf("[compression %s] phase=%s status=%s", mode, phase, status)
}

func formatCompressionEnd(mode, status string, data map[string]any) string {
	count := data["compressed_message_count"]
	switch status {
	case "applied":
		line := fmt.Sprintf("[compression %s] 压缩完成，已应用摘要（替换 %v 条消息）", mode, count)
		return line + formatCompressionMetricsSuffix(data)
	case "failed":
		return fmt.Sprintf("[compression %s] 压缩失败，保留原始上下文", mode)
	case "stale":
		return fmt.Sprintf("[compression %s] 压缩结果已过期，已丢弃", mode)
	case "invalid":
		return fmt.Sprintf("[compression %s] 压缩结果无效，已丢弃", mode)
	default:
		return fmt.Sprintf("[compression %s] 压缩结束（status=%s）", mode, status)
	}
}

func formatCompressionMetricsSuffix(data map[string]any) string {
	prompt, okPrompt := intFromAny(data["prompt_tokens"])
	completion, okCompletion := intFromAny(data["completion_tokens"])
	if !okPrompt || !okCompletion {
		return ""
	}
	rate := floatFromAny(data["token_reduction_rate"])
	suffix := fmt.Sprintf("；prompt %s→completion %s tokens（减少 %s）",
		formatTokenCount(prompt),
		formatTokenCount(completion),
		formatPercent(rate),
	)
	if hit, ok := intFromAny(data["prompt_cache_hit_tokens"]); ok {
		miss, _ := intFromAny(data["prompt_cache_miss_tokens"])
		cachePart := fmt.Sprintf("；cache hit %s / miss %s", formatTokenCount(hit), formatTokenCount(miss))
		if hr := floatFromAny(data["prompt_cache_hit_rate"]); data["prompt_cache_hit_rate"] != nil {
			cachePart += fmt.Sprintf("（命中率 %s）", formatPercent(hr))
		}
		suffix += cachePart
	}
	return suffix
}

func formatTokenCount(n int) string {
	if n >= 10_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func formatPercent(rate float64) string {
	if rate < 0 {
		rate = 0
	}
	if rate > 1 {
		rate = 1
	}
	return fmt.Sprintf("%.0f%%", math.Round(rate*100))
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case float32:
		return int(n), true
	default:
		return 0, false
	}
}

func floatFromAny(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}
