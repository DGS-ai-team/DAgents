// Package logx 提供 Node 日志级别解析与 logger 默认值辅助。
package logx

import (
	"context"
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

// NewSplitLogger 将 >= level 的日志写入 full，并将 >= Error 的日志额外写入 errW。
// 配合启动器 stdout→*.log、stderr→*.err.log：*.log 为完整日志，*.err.log 仅错误。
func NewSplitLogger(full, errW io.Writer, level slog.Level) *slog.Logger {
	if full == nil {
		full = io.Discard
	}
	if errW == nil {
		errW = io.Discard
	}
	opts := &slog.HandlerOptions{Level: level}
	return slog.New(&splitHandler{
		full: slog.NewTextHandler(full, opts),
		err:  slog.NewTextHandler(errW, &slog.HandlerOptions{Level: slog.LevelError}),
	})
}

type splitHandler struct {
	full slog.Handler
	err  slog.Handler
}

func (h *splitHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.full.Enabled(ctx, level)
}

func (h *splitHandler) Handle(ctx context.Context, r slog.Record) error {
	if err := h.full.Handle(ctx, r); err != nil {
		return err
	}
	if r.Level >= slog.LevelError && h.err.Enabled(ctx, r.Level) {
		return h.err.Handle(ctx, r)
	}
	return nil
}

func (h *splitHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &splitHandler{
		full: h.full.WithAttrs(attrs),
		err:  h.err.WithAttrs(attrs),
	}
}

func (h *splitHandler) WithGroup(name string) slog.Handler {
	return &splitHandler{
		full: h.full.WithGroup(name),
		err:  h.err.WithGroup(name),
	}
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
