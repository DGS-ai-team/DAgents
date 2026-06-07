// Package logx 提供 Node 日志级别解析与 logger 默认值辅助。
package logx

import (
	"io"
	"log/slog"
	"strings"
)

// ParseLevel 将配置字符串解析为 slog.Level；无法识别时返回 Info 与 false。
func ParseLevel(raw string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}

// NewLogger 创建写入 stderr 的 text handler logger。
func NewLogger(w io.Writer, level slog.Level) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

// OrDefault 在 l 为 nil 时返回 slog.Default()。
func OrDefault(l *slog.Logger) *slog.Logger {
	if l == nil {
		return slog.Default()
	}
	return l
}

// Discard 返回丢弃全部输出的 logger（单测用）。
func Discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError + 1}))
}
