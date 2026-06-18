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
	"time"

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

	if ctxBody, err := app.client.GetSessionContext(ctx, app.currentSession()); err == nil {
		for _, line := range tuishared.SkillsBloatWarningTexts(ctxBody) {
			fmt.Fprintln(os.Stderr, "警告: "+line)
		}
	}

	fmt.Fprintf(os.Stderr, "已连接 %s agent_id=%s model=%s client=%s (plain REPL)\n",
		res.Endpoint, res.AgentID, orReplDash(res.LLM.Model), version.Version)
	if res.LLM.ThinkingSupported {
		fmt.Fprintf(os.Stderr, "thinking: %s（/thinking on|off · /thinking effort high|max）\n",
			tuishared.FormatLLMThinkingSummary(res.LLM))
	}
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
		fmt.Fprintln(os.Stderr, "等待 Agent 响应…")
		if err := waitTurnWithFeedback(ctx, app.turn); err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
}

func waitTurnWithFeedback(ctx context.Context, gate *tuishared.TurnGate) error {
	done := make(chan error, 1)
	go func() {
		done <- gate.Wait(ctx)
	}()
	start := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			return err
		case <-tick.C:
			secs := int(time.Since(start).Seconds())
			fmt.Fprintf(os.Stderr, "\r等待 Agent… %ds（Esc 不可用；Ctrl+C 退出）", secs)
		case <-ctx.Done():
			return ctx.Err()
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
	case "thinking":
		err = a.handleThinkingCommand(ctx, parts[1:])
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
	fmt.Fprintf(os.Stderr, "model:         %s\n", orReplDash(a.probe.LLM.Model))
	if a.probe.LLM.ThinkingSupported {
		fmt.Fprintf(os.Stderr, "thinking:      %s\n", tuishared.FormatLLMThinkingSummary(a.probe.LLM))
	}
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
  /history [n|all]     查看最近 n 行输出（默认 20）
  /tools [verbose|brief]  tool 输出折叠/展开
  /reasoning [on|off]  显示/隐藏模型推理流
  /thinking [on|off]   模型思考开关（deepseek/qwen）
  /thinking effort high|max  思考强度
  /quit                退出（流式输出中请用 Esc 取消 turn）`)
}

func orReplDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func (a *App) handleThinkingCommand(ctx context.Context, args []string) error {
	if !a.probe.LLM.ThinkingSupported {
		return fmt.Errorf("当前 provider 不支持 thinking 控制（需 deepseek 或 qwen）")
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "thinking: %s\n", tuishared.FormatLLMThinkingSummary(a.probe.LLM))
		return nil
	}
	var patch nodeapi.LLMSettingsPatch
	switch strings.ToLower(args[0]) {
	case "on", "enabled", "true", "1":
		v := "enabled"
		patch.Thinking = &v
	case "off", "disabled", "false", "0":
		v := "disabled"
		patch.Thinking = &v
	case "effort":
		if len(args) < 2 {
			return fmt.Errorf("用法: /thinking effort high|max")
		}
		v := strings.ToLower(args[1])
		if v != "high" && v != "max" {
			return fmt.Errorf("用法: /thinking effort high|max")
		}
		patch.ReasoningEffort = &v
	default:
		return fmt.Errorf("用法: /thinking on|off 或 /thinking effort high|max")
	}
	settings, err := a.client.PatchLLMSettings(ctx, patch)
	if err != nil {
		return err
	}
	a.probe.LLM = probe.LLMInfo{
		Provider:          settings.Provider,
		Model:             settings.Model,
		Mock:              settings.Mock,
		ThinkingSupported: settings.ThinkingSupported,
		Thinking:          settings.Thinking,
		ReasoningEffort:   settings.ReasoningEffort,
	}
	fmt.Fprintf(os.Stderr, "thinking: %s\n", tuishared.FormatLLMThinkingSummary(a.probe.LLM))
	return nil
}
