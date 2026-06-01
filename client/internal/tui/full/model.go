// Package full 提供 bubbletea 全屏 TUI（上输出 / 下输入分区；SSH 交互首选）。
package full

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	"github.com/DGS-ai-team/DAgents/client/internal/probe"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
	"github.com/DGS-ai-team/DAgents/client/internal/version"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

const (
	inputHeight   = 3
	statusHeight  = 1
	helpHeight    = 1
	minViewWidth  = 20
	minViewHeight = 6
)

type uiMode int

const (
	modeChat uiMode = iota
	modeApproval
	modeUserInfo
)

type hitlResult struct {
	resume map[string]any
	err    error
}

// refreshViewportMsg 通知 Update 刷新 viewport 内容并滚到底。
type refreshViewportMsg struct{}

// streamErrMsg SSE 后台非致命错误（已记录到 transcript）。
type streamErrMsg struct {
	line string
}

// model 为 bubbletea 全屏 TUI 状态。
type model struct {
	ctx    context.Context
	cancel context.CancelFunc

	cfg    *config.Config
	client *nodeapi.Client
	probe  *probe.Result

	program *tea.Program

	sessionMu sync.Mutex
	sessionID string

	viewport viewport.Model
	input    textarea.Model

	transcript *tuishared.Transcript
	toolFold   *tuishared.ToolFold

	mode       uiMode
	hitlPrompt string
	hitlData   map[string]any
	hitlCh     chan hitlResult

	contextMode bool
	contextText string

	approvalItems    []clihitl.ToolApprovalItem
	approvalSelected map[string]bool
	approvalCursor   int

	userInfoReq        *clihitl.UserInformationRequest
	userInfoSelected   map[string]bool
	userInfoCursor     int
	userInfoUseOptions bool

	sseConnected bool
	sseDetail    string
	awaitingTurn bool
	submitSeen   bool
	showReasoning bool
	statusLine   string
	helpLine     string
	errLine      string

	streamCancel context.CancelFunc
	streamDone   chan struct{}
}

// Run 启动全屏 TUI；initialSession 非空时尝试恢复该 session。
func Run(ctx context.Context, cfg *config.Config, initialSession string, showReasoning bool) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	client := nodeapi.New(cfg.Local.Endpoint, nil)
	res, err := probe.Node(runCtx, cfg, nil)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}

	ta := textarea.New()
	ta.Placeholder = defaultInputPlaceholder
	ta.ShowLineNumbers = false
	ta.SetHeight(inputHeight - 1)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.CharLimit = 0

	m := &model{
		ctx:        runCtx,
		cancel:     cancel,
		cfg:        cfg,
		client:     client,
		probe:      res,
		input:      ta,
		transcript: tuishared.NewTranscript(0),
		toolFold:   &tuishared.ToolFold{},
		hitlCh:     make(chan hitlResult, 1),
		showReasoning: showReasoning,
		helpLine:   "Enter 发送 · Shift+Enter 换行 · Esc 取消 turn · /help 命令 · /quit 退出",
	}

	if err := m.bootstrapSession(initialSession); err != nil {
		return err
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(runCtx))
	m.program = p
	_, err = p.Run()
	if err != nil {
		return err
	}
	m.stopStream()
	return nil
}

func (m *model) bootstrapSession(initialSession string) error {
	id, err := m.client.CreateSession(m.ctx, initialSession)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	m.sessionMu.Lock()
	m.sessionID = id
	m.sessionMu.Unlock()

	welcome := fmt.Sprintf(
		"已连接 %s · agent=%s · client=%s · session=%s",
		m.probe.Endpoint, m.probe.AgentID, version.Version, id,
	)
	m.transcript.Add("[system] " + welcome)
	m.sseDetail = "连接中…"
	m.restartStream()
	return nil
}

func (m *model) currentSession() string {
	m.sessionMu.Lock()
	defer m.sessionMu.Unlock()
	return m.sessionID
}

func (m *model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.applySize(msg.Width, msg.Height)
		return m, nil

	case refreshViewportMsg:
		m.syncViewport()
		return m, nil

	case streamErrMsg:
		m.transcript.Add("[system] " + msg.line)
		m.syncViewport()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.contextMode && msg.String() == "esc" {
		m.exitContextView()
		return m, nil
	}
	switch m.mode {
	case modeApproval:
		return m.handleApprovalKey(msg)
	case modeUserInfo:
		return m.handleUserInfoKey(msg)
	default:
		return m.handleChatKey(msg)
	}
}

func (m *model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.awaitingTurn {
			if err := m.cancelTurn(); err != nil {
				m.errLine = err.Error()
			} else {
				m.statusLine = "已请求取消 turn"
				m.awaitingTurn = false
			}
		}
		return m, nil
	case "ctrl+c":
		m.shutdown()
		return m, tea.Quit
	case "enter":
		text := strings.TrimSpace(m.input.Value())
		if text == "" {
			return m, nil
		}
		m.input.SetValue("")
		if strings.HasPrefix(text, "/") {
			if quit, err := m.execCommand(text); err != nil {
				m.errLine = err.Error()
			} else if quit {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.awaitingTurn {
			m.errLine = "上一回合尚未结束，请等待或 Esc 取消"
			return m, nil
		}
		m.transcript.Add("[user] " + text)
		m.syncViewport()
		m.awaitingTurn = true
		m.submitSeen = false
		if err := m.client.SubmitMessage(m.ctx, m.currentSession(), text); err != nil {
			m.awaitingTurn = false
			m.errLine = err.Error()
		} else {
			m.statusLine = "等待 Agent 回复…"
			m.errLine = ""
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m *model) View() string {
	status := m.renderStatusBar()
	viewBody := m.viewport.View()
	if m.contextMode {
		viewBody = lipgloss.NewStyle().Render(m.contextText)
	}
	inputBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(m.viewport.Width).
		Render(m.input.View())

	help := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.helpLine)
	if m.errLine != "" {
		help = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Render(m.errLine)
	}

	parts := []string{status, viewBody, inputBox, help}
	if m.mode == modeApproval {
		body := clihitl.FormatApprovalInteractive(m.hitlData, m.approvalSelected, m.approvalCursor)
		prompt := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("214")).
			Padding(0, 1).
			Width(m.viewport.Width).
			Render("工具审批\n" + body)
		parts = []string{status, viewBody, prompt, help}
	} else if m.mode == modeUserInfo {
		question := m.hitlPrompt
		if m.userInfoReq != nil && m.userInfoReq.Question != "" {
			question = m.userInfoReq.Question
		}
		prompt := lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Render("Agent 询问: " + question)
		parts = []string{status, viewBody, prompt}
		if m.userInfoUseOptions && m.userInfoReq != nil {
			opts := lipgloss.NewStyle().Render(clihitl.FormatUserInformationOptions(m.userInfoReq, m.userInfoSelected, m.userInfoCursor))
			parts = append(parts, opts)
			parts = append(parts, help)
		} else {
			parts = append(parts, inputBox, help)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *model) renderStatusBar() string {
	sse := "SSE 未连接"
	color := "203"
	if m.sseConnected {
		sse = "SSE 已连接"
		color = "78"
	}
	left := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("● " + sse)
	if m.sseDetail != "" {
		left += " · " + m.sseDetail
	}
	if m.statusLine != "" {
		left += " · " + m.statusLine
	}
	right := fmt.Sprintf("session=%s", m.currentSession())
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *model) applySize(w, h int) {
	if w < minViewWidth {
		w = minViewWidth
	}
	if h < minViewHeight {
		h = minViewHeight
	}
	viewH := h - statusHeight - inputHeight - helpHeight - 2
	if m.mode == modeApproval {
		viewH = h - statusHeight - 5 - helpHeight
	} else if m.mode == modeUserInfo {
		viewH = h - statusHeight - inputHeight - helpHeight - 3
	}
	if viewH < 3 {
		viewH = 3
	}
	m.viewport = viewport.New(w, viewH)
	m.viewport.YPosition = 0
	m.input.SetWidth(w - 4)
	m.syncViewport()
}

func (m *model) syncViewport() {
	m.viewport.SetContent(strings.Join(m.transcript.Lines(), "\n"))
	m.viewport.GotoBottom()
}

func (m *model) resolveHITL(res hitlResult) {
	select {
	case m.hitlCh <- res:
	default:
	}
}

func (m *model) promptApproval(ctx context.Context, data map[string]any) (map[string]any, error) {
	m.initApprovalState(data)
	m.hitlPrompt = clihitl.FormatApprovalPrompt(data)
	if m.program != nil {
		m.program.Send(refreshViewportMsg{})
	}
	m.mode = modeApproval
	m.statusLine = "等待审批…"
	select {
	case <-ctx.Done():
		m.resetHITLState()
		return nil, ctx.Err()
	case res := <-m.hitlCh:
		if res.err != nil {
			m.resetHITLState()
			return nil, res.err
		}
		m.resetHITLState()
		return res.resume, nil
	}
}

func (m *model) promptUserInfo(ctx context.Context, data map[string]any) (map[string]any, error) {
	m.initUserInfoState(data)
	if m.userInfoReq != nil {
		m.hitlPrompt = m.userInfoReq.Question
	} else {
		m.hitlPrompt = clihitl.FormatUserInformationQuestion(data)
	}
	if m.program != nil {
		m.program.Send(refreshViewportMsg{})
	}
	m.mode = modeUserInfo
	m.input.SetValue("")
	if m.userInfoUseOptions {
		m.input.Placeholder = "使用 ↑/↓ + Space 选择选项"
	} else {
		m.input.Placeholder = "输入回答后 Enter 提交 · Esc 取消"
	}
	m.statusLine = "等待用户回答…"
	select {
	case <-ctx.Done():
		m.resetHITLState()
		return nil, ctx.Err()
	case res := <-m.hitlCh:
		if res.err != nil {
			m.resetHITLState()
			return nil, res.err
		}
		m.resetHITLState()
		return res.resume, nil
	}
}

func (m *model) onStreamEvent(ev nodeapi.StreamEvent) {
	sink := clihitl.Sink{
		OnAssistant: func(text string) {
			if m.awaitingTurn && !m.submitSeen {
				m.submitSeen = true
			}
			m.transcript.AppendPartial("assistant", text)
			if m.program != nil {
				m.program.Send(refreshViewportMsg{})
			}
		},
		OnReasoning: func(text string) {
			if !m.showReasoning {
				return
			}
			m.transcript.AppendPartial("reasoning", text)
			if m.program != nil {
				m.program.Send(refreshViewportMsg{})
			}
		},
		OnTool: func(eventType string, data map[string]any) {
			m.transcript.FinishPartial("assistant")
			m.transcript.FinishPartial("reasoning")
			line := m.toolFold.Format(eventType, data)
			m.transcript.Add("[system] " + line)
			if m.program != nil {
				m.program.Send(refreshViewportMsg{})
			}
		},
		OnCompression: func(eventType string, data map[string]any) {
			m.transcript.Add("[system] " + clihitl.FormatContextCompression(eventType, data))
			if m.program != nil {
				m.program.Send(refreshViewportMsg{})
			}
		},
		OnError: func(msg string) {
			m.transcript.Add("[system] error: " + msg)
			if m.program != nil {
				m.program.Send(refreshViewportMsg{})
			}
		},
	}
	interact := &clihitl.Interact{
		PromptApproval: m.promptApproval,
		PromptUserInfo: m.promptUserInfo,
	}
	cont, err := clihitl.HandleStreamEvent(m.ctx, m.client, m.currentSession(), ev, sink, interact, false)
	if err != nil && m.program != nil {
		m.program.Send(streamErrMsg{line: fmt.Sprintf("事件处理失败: %v", err)})
	}
	if ev.Type == "done" {
		m.transcript.FinishPartial("assistant")
		m.transcript.FinishPartial("reasoning")
		if m.awaitingTurn && m.submitSeen {
			m.awaitingTurn = false
			m.statusLine = "回合结束"
		}
		if m.program != nil {
			m.program.Send(refreshViewportMsg{})
		}
	}
	_ = cont
}

func (m *model) cancelTurn() error {
	cancelled, err := m.client.CancelTurn(m.ctx, m.currentSession())
	if err != nil {
		return err
	}
	if cancelled {
		m.transcript.Add("[system] turn 已取消")
		m.syncViewport()
	}
	return nil
}

func (m *model) shutdown() {
	m.stopStream()
	m.cancel()
}
