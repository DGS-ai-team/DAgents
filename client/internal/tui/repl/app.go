// Package repl 提供行模式 REPL TUI（老终端 / --plain 兜底）。
package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/DGS-ai-team/DAgents/client/internal/probe"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
	"github.com/DGS-ai-team/DAgents/client/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// App 为交互式 REPL 会话状态与主循环。
type App struct {
	cfg    *config.Config
	client *nodeapi.Client
	probe  *probe.Result

	sessionMu sync.Mutex
	sessionID string

	transcript *tuishared.Transcript
	toolFold   *tuishared.ToolFold
	printMu    sync.Mutex
	showReasoning bool

	streamCancel context.CancelFunc
	streamDone   chan struct{}

	turn *tuishared.TurnGate
}

// Run 启动行模式 REPL；initialSession 非空时尝试恢复该 session。
func Run(ctx context.Context, cfg *config.Config, initialSession string, showReasoning bool) error {
	app := &App{
		cfg:           cfg,
		client:        nodeapi.New(cfg.Local.Endpoint, nil),
		transcript:    tuishared.NewTranscript(0),
		toolFold:      &tuishared.ToolFold{},
		showReasoning: showReasoning,
		turn:          tuishared.NewTurnGate(),
	}
	res, err := probe.Node(ctx, cfg, nil)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	app.probe = res

	if err := app.setSession(ctx, initialSession); err != nil {
		return err
	}
	defer app.stopStream()

	fmt.Fprintf(os.Stderr, "已连接 %s agent_id=%s client=%s (plain REPL)\n", res.Endpoint, res.AgentID, version.Version)
	fmt.Fprintf(os.Stderr, "session=%s（/help 查看命令）\n\n", app.currentSession())

	reader := bufio.NewReader(os.Stdin)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		fmt.Fprint(os.Stdout, "dagents> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println()
				return nil
			}
			return err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			quit, cmdErr := app.execCommand(ctx, line)
			if cmdErr != nil {
				fmt.Fprintf(os.Stderr, "命令失败: %v\n", cmdErr)
			}
			if quit {
				return nil
			}
			continue
		}

		app.transcript.AddBlockGapIfNeeded()
		app.transcript.Add("[user] " + line)
		app.turn.BeginSubmit()
		if err := app.client.SubmitMessage(ctx, app.currentSession(), line); err != nil {
			app.turn.FinishTurn()
			fmt.Fprintf(os.Stderr, "发送失败: %v\n", err)
			continue
		}
		fmt.Fprintln(os.Stderr, "（回合进行中；若有审批/询问，请在下方提示处输入）")
		if err := app.turn.Wait(ctx); err != nil {
			return err
		}
	}
}

func (a *App) currentSession() string {
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	return a.sessionID
}

func (a *App) setSession(ctx context.Context, requested string) error {
	id, err := a.client.CreateSession(ctx, requested)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	a.sessionMu.Lock()
	a.sessionID = id
	a.sessionMu.Unlock()
	a.restartStream(ctx)
	return nil
}

func (a *App) restartStream(ctx context.Context) {
	a.stopStream()
	streamCtx, cancel := context.WithCancel(ctx)
	a.streamCancel = cancel
	a.streamDone = make(chan struct{})
	sid := a.currentSession()
	go func() {
		defer close(a.streamDone)
		runner := newStreamRunner(a.client, sid, a.transcript, a.toolFold, &a.printMu, &a.showReasoning, a.turn)
		_ = runner.Run(streamCtx)
	}()
}

func (a *App) stopStream() {
	if a.streamCancel == nil {
		return
	}
	a.streamCancel()
	<-a.streamDone
	a.streamCancel = nil
}

func (a *App) execCommand(ctx context.Context, line string) (quit bool, err error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false, nil
	}
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	switch cmd {
	case "help", "h", "?":
		printHelp()
	case "status":
		err = a.printStatus(ctx)
	case "sessions", "ls":
		err = a.printSessions(ctx)
	case "switch":
		if len(parts) < 2 {
			return false, fmt.Errorf("用法: /switch <session_id>")
		}
		err = a.setSession(ctx, parts[1])
		if err == nil {
			fmt.Fprintf(os.Stderr, "已切换 session=%s\n", a.currentSession())
		}
	case "new":
		err = a.setSession(ctx, "")
		if err == nil {
			fmt.Fprintf(os.Stderr, "新 session=%s\n", a.currentSession())
		}
	case "clear":
		err = a.client.ClearSessionContext(ctx, a.currentSession())
		if err == nil {
			fmt.Fprintln(os.Stderr, "已清空对话上下文")
		}
	case "cancel":
		cancelled, cancelErr := a.client.CancelTurn(ctx, a.currentSession())
		if cancelErr != nil {
			err = cancelErr
		} else if cancelled {
			fmt.Fprintln(os.Stderr, "已取消在途 turn")
		} else {
			fmt.Fprintln(os.Stderr, "当前无在途 turn")
		}
	case "history":
		n := 20
		if len(parts) > 1 {
			if parts[1] == "all" {
				n = 0
			} else if v, parseErr := strconv.Atoi(parts[1]); parseErr == nil && v > 0 {
				n = v
			}
		}
		fmt.Println(a.transcript.FormatTail(n))
	case "tools":
		if len(parts) > 1 && strings.EqualFold(parts[1], "verbose") {
			a.toolFold.SetVerbose(true)
			fmt.Fprintln(os.Stderr, "tool 输出：详细模式")
		} else if len(parts) > 1 && strings.EqualFold(parts[1], "brief") {
			a.toolFold.SetVerbose(false)
			fmt.Fprintln(os.Stderr, "tool 输出：折叠模式")
		} else {
			mode := "折叠"
			if a.toolFold.Verbose() {
				mode = "详细"
			}
			fmt.Fprintf(os.Stderr, "tool 输出模式: %s（/tools verbose|brief）\n", mode)
		}
	case "reasoning":
		if len(parts) > 1 {
			switch strings.ToLower(parts[1]) {
			case "on", "true", "1":
				a.showReasoning = true
			case "off", "false", "0":
				a.showReasoning = false
			default:
				fmt.Fprintln(os.Stderr, "用法: /reasoning on|off")
				return false, nil
			}
		}
		mode := "关闭"
		if a.showReasoning {
			mode = "开启"
		}
		fmt.Fprintf(os.Stderr, "reasoning 显示: %s（/reasoning on|off）\n", mode)
	case "quit", "exit", "q":
		return true, nil
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q，输入 /help\n", cmd)
	}
	return false, err
}

func (a *App) printStatus(ctx context.Context) error {
	ctxBody, err := a.client.GetSessionContext(ctx, a.currentSession())
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "agent_id:      %s\n", a.probe.AgentID)
	fmt.Fprintf(os.Stderr, "node_version:  %s\n", a.probe.Version)
	fmt.Fprintf(os.Stderr, "client_version:%s\n", version.Version)
	fmt.Fprintf(os.Stderr, "endpoint:      %s\n", a.probe.Endpoint)
	fmt.Fprintf(os.Stderr, "session_id:    %s\n", a.currentSession())
	fmt.Fprintf(os.Stderr, "messages:      %d\n", ctxBody.MessagesCount)
	fmt.Fprintf(os.Stderr, "queue_pending: %d\n", ctxBody.QueuePending)
	fmt.Fprintf(os.Stderr, "active_turn:   %v", ctxBody.HasActiveTurn)
	if ctxBody.TurnState != "" {
		fmt.Fprintf(os.Stderr, " (%s)", ctxBody.TurnState)
	}
	fmt.Fprintln(os.Stderr)
	return nil
}

func (a *App) printSessions(ctx context.Context) error {
	items, err := a.client.ListSessions(ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "(无 session)")
		return nil
	}
	cur := a.currentSession()
	for _, s := range items {
		mark := " "
		if s.SessionID == cur {
			mark = "*"
		}
		active := "idle"
		if s.Active {
			active = "active"
		}
		preview := s.FirstUserMessage
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		fmt.Fprintf(os.Stderr, "%s %s [%s] msgs=%d %s\n", mark, s.SessionID, active, s.MessageCount, preview)
	}
	return nil
}

func printHelp() {
	fmt.Fprintln(os.Stderr, `命令:
  /status              显示 agent、session、队列深度
  /sessions (/ls)      列出 session（* 为当前）
  /switch <id>         切换 session
  /new                 新建 session
  /clear               清空当前对话上下文
  /cancel              取消在途 turn（回合等待期间不可用，请等审批结束）
  /history [n|all]     查看最近 n 行输出（默认 20）
  /tools [verbose|brief]  tool 输出折叠/展开
  /reasoning [on|off]  显示/隐藏模型推理流
  /quit                退出`)
}
