// Package tui 为 Go Client 终端入口：默认全屏 bubbletea（full），可回退行模式 REPL（repl）。
//
// 目录：
//   - full/   全屏 TUI（SSH 交互、输入/输出分区）
//   - repl/   行模式 REPL（--plain、TERM=dumb、老终端）
//   - shared/ transcript 与 tool 格式化
//
// Python Textual TUI 位于 app/cli/tui/，与本包无关。
package tui

import (
	"context"
	"os"

	"github.com/DGS-ai-team/DAgents/client/internal/tui/full"
	"github.com/DGS-ai-team/DAgents/client/internal/tui/repl"
	"github.com/DGS-ai-team/DAgents/shared/config"
	"golang.org/x/term"
)

// Options 控制 TUI 模式选择。
type Options struct {
	Plain         bool
	ForceFull     bool
	ShowReasoning bool
}

// Run 启动交互式 TUI；按 Options 与环境选择 full 或 repl。
func Run(ctx context.Context, cfg *config.Config, initialSession string, opts Options) error {
	if opts.Plain || (!opts.ForceFull && preferPlain()) {
		return repl.Run(ctx, cfg, initialSession, opts.ShowReasoning)
	}
	return full.Run(ctx, cfg, initialSession, opts.ShowReasoning)
}

// preferPlain 判断是否应使用行模式 REPL。

// 逻辑：
// 1. DAGENTS_TUI=plain|full 显式覆盖；
// 2. TERM=dumb 或无 tty 时用 plain；
// 3. 否则默认 full。
func preferPlain() bool {
	switch os.Getenv("DAGENTS_TUI") {
	case "plain", "repl":
		return true
	case "full":
		return false
	}
	if term := os.Getenv("TERM"); term == "dumb" || term == "" {
		return true
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return true
	}
	return false
}
