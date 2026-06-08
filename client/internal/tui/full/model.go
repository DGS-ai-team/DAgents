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
	inputHeight      = 3
	statusHeight     = 1
	inputStripHeight = 1
	helpHeight       = 1
	minViewWidth  = 20
	minViewHeight = 6
)

type uiMode int

const (
	modeChat uiMode = iota
	modeApproval
	modeUserInfo
)

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
	hitlQueue  []hitlPending

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
	turn         *tuishared.TurnGate
	showReasoning bool
	statusLine   string
	helpLine     string
	errLine      string

	messagesTotalTokens int
	usageStrip          tuishared.UsageStripSnapshot

	streamCancel context.CancelFunc
	streamDone   chan struct{}

	children *childAgentTracker
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
	ta.Prompt = ""
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
		showReasoning: showReasoning,
		helpLine:   "Enter 发送 · Shift+Enter 换行 · Esc 取消 turn · /help 命令 · /quit 退出",
		children:            newChildAgentTracker(),
		messagesTotalTokens: -1,
		turn:                tuishared.NewTurnGate(),
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
	// textarea 默认未 focus；不调用 Focus() 时 Update 会直接丢弃按键。
	return m.input.Focus()
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

	case pendingHITLChangedMsg:
		m.showNextHITLIfIdle()
		m.syncViewport()
		return m, m.refocusInputIfNeeded()

	case hitlSubmitResultMsg:
		if msg.err != nil {
			m.errLine = msg.err.Error()
		} else {
			m.errLine = ""
		}
		return m, nil

	case childAgentsSyncedMsg:
		if msg.err == nil {
			m.children.replaceFromAPI(msg.items)
		}
		return m, nil

	case syncChildAgentsMsg:
		return m, m.cmdSyncChildAgents()

	case refreshContextTokensMsg:
		return m, m.cmdRefreshContextTokens()

	case contextTokensSyncedMsg:
		if msg.tokens >= 0 {
			m.messagesTotalTokens = msg.tokens
		}
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
		if m.turn.Awaiting() {
			if err := m.cancelTurn(); err != nil {
				m.errLine = err.Error()
			} else {
				m.statusLine = "已请求取消 turn"
				m.turn.FinishTurn()
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
		if m.turn.Awaiting() {
			m.errLine = "上一回合尚未结束，请等待或 Esc 取消"
			return m, nil
		}
		m.invalidateHITLForUserMessage()
		m.resetUsageStrip()
		m.transcript.AddBlockGapIfNeeded()
		m.transcript.Add("[user] " + text)
		m.syncViewport()
		m.turn.BeginSubmit()
		if err := m.client.SubmitMessage(m.ctx, m.currentSession(), text); err != nil {
			m.turn.FinishTurn()
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

	parts := []string{status, viewBody, m.renderInputStrip(), inputBox, help}
	if m.mode == modeApproval {
		body := clihitl.FormatApprovalInteractive(m.hitlData, m.approvalSelected, m.approvalCursor)
		borderColor := lipgloss.Color("214")
		if clihitl.IsTemporaryAgentApproval(m.hitlData) {
			borderColor = lipgloss.Color("39")
		}
		prompt := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(borderColor).
			Padding(0, 1).
			Width(m.viewport.Width).
			Render(clihitl.ApprovalHeader(m.hitlData) + "\n" + body)
		parts = []string{status, viewBody, m.renderInputStrip(), prompt, help}
	} else if m.mode == modeUserInfo {
		// 问题已在 transcript「Agent 询问」块中展示；底部仅保留选项或输入框。
		parts = []string{status, viewBody, m.renderInputStrip()}
		if m.userInfoUseOptions && m.userInfoReq != nil {
			opts := lipgloss.NewStyle().Render(clihitl.FormatUserInformationOptions(m.userInfoReq, m.userInfoSelected, m.userInfoCursor))
			parts = append(parts, opts, help)
		} else {
			parts = append(parts, inputBox, help)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *model) renderInputStrip() string {
	_, pending := m.children.counts()
	left := m.renderInputStripStyled()
	right := tuishared.FormatInputStripUsage(m.usageStrip)
	if right == "" {
		right = tuishared.FormatInputStripTokens(m.messagesTotalTokens)
	}
	text := left
	if right != "" {
		width := m.viewport.Width
		if width <= 0 {
			width = 80
		}
		gap := width - len(left) - len(right)
		if gap < 2 {
			text = left + "  ·  " + right
		} else {
			text = left + strings.Repeat(" ", gap) + right
		}
	}
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	if pending > 0 {
		style = style.Foreground(lipgloss.Color("214"))
	}
	rendered := style.Render(text)
	if lipgloss.Width(rendered) < width {
		rendered += strings.Repeat(" ", width-lipgloss.Width(rendered))
	}
	return rendered
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
	viewH := h - statusHeight - inputStripHeight - inputHeight - helpHeight - 2
	if m.mode == modeApproval {
		viewH = h - statusHeight - inputStripHeight - 5 - helpHeight
	} else if m.mode == modeUserInfo {
		viewH = h - statusHeight - inputStripHeight - inputHeight - helpHeight - 3
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
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	m.viewport.SetContent(strings.Join(m.transcript.LinesForDisplay(width), "\n"))
	m.viewport.GotoBottom()
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

func (m *model) refocusInputIfNeeded() tea.Cmd {
	if m.mode == modeChat || (m.mode == modeUserInfo && !m.userInfoUseOptions) {
		return m.input.Focus()
	}
	return nil
}

func (m *model) shutdown() {
	m.stopStream()
	m.cancel()
}
