// dagents-node 为 Agent Node 进程入口：加载配置、启动 HTTP/SSE API，直至收到退出信号。
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/DGS-ai-team/DAgents/node/internal/api"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/processlock"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func main() {
	if len(os.Args) >= 2 && (os.Args[1] == "version" || os.Args[1] == "-version" || os.Args[1] == "--version") {
		fmt.Println(version.Version)
		return
	}
	// 1) 解析 -config / 环境变量 / 默认 packaging 路径。
	configPath := flag.String("config", "", "path to config.yaml (optional; default: DAGENTS_CONFIG or packaging/agent-client/config.yaml)")
	logLevelFlag := flag.String("log-level", "", "log level: debug, info, warn, error (overrides config log.level)")
	flag.Parse()

	resolved, err := config.ResolveConfigPath(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(2)
	}

	release, err := processlock.AcquireNode(resolved)
	if err != nil {
		if err == processlock.ErrAlreadyRunning {
			fmt.Fprintf(os.Stderr, "dagents-node: another instance is already running for this config\n")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "dagents-node: process lock: %v\n", err)
		os.Exit(1)
	}
	defer release()

	// 2) 加载引导 YAML，再 overlay node_settings.db（空库时写入产品默认）。
	cfg, err := config.LoadFile(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}
	nodeSettings, err := store.BootstrapNodeSettings(context.Background(), cfg, resolved, slog.Default())
	if err != nil {
		fmt.Fprintf(os.Stderr, "node settings: %v\n", err)
		os.Exit(1)
	}

	levelText := cfg.Log.Level
	if override := *logLevelFlag; override != "" {
		levelText = override
	}
	level, ok := logx.ParseLevel(levelText)
	if !ok {
		fmt.Fprintf(os.Stderr, "config: invalid log level %q (use debug, info, warn, error)\n", levelText)
		os.Exit(1)
	}
	logger := logx.NewSplitLogger(os.Stdout, os.Stderr, level)
	slog.SetDefault(logger)

	// 3) 构造 HTTP 服务（session、turn、tools、SQLite 等由 api.NewServer 内部装配）。
	logger.Info("config loaded", "path", resolved, "log_level", level.String(), "agent_id", cfg.NodeID)
	srv := api.NewServer(cfg, logger, api.WithConfigPath(resolved), api.WithNodeSettings(nodeSettings))

	// 4) SIGINT/SIGTERM 触发 ctx 取消，ListenAndServe 优雅关闭。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.ListenAndServe(ctx); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
	logger.Info("agent node exited")
}
