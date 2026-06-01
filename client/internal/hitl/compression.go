package hitl

import "fmt"

// FormatContextCompression 将 blocking/silent 压缩 SSE 格式化为终端提示行。
func FormatContextCompression(eventType string, data map[string]any) string {
	mode := "blocking"
	if eventType == "context_compression_silent" {
		mode = "silent"
	}
	phase := fmt.Sprint(data["phase"])
	status := fmt.Sprint(data["status"])
	count := data["compressed_message_count"]

	switch mode {
	case "blocking":
		switch phase {
		case "start":
			return fmt.Sprintf("[compression blocking] 上下文压缩进行中（阻塞本轮消息，压缩 %v 条）…", count)
		case "end":
			return formatCompressionEnd("blocking", status, count)
		}
	case "silent":
		switch phase {
		case "start":
			return fmt.Sprintf("[compression silent] 后台静默压缩已开始（目标 %v 条消息）", count)
		case "end":
			return formatCompressionEnd("silent", status, count)
		}
	}
	return fmt.Sprintf("[compression %s] phase=%s status=%s", mode, phase, status)
}

func formatCompressionEnd(mode, status string, count any) string {
	switch status {
	case "applied":
		return fmt.Sprintf("[compression %s] 压缩完成，已应用摘要（替换 %v 条消息）", mode, count)
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
