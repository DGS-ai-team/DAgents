// Package full 提供 bubbletea 全屏 TUI（上输出 / 下输入分区；SSH 交互首选）。
package full

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	"github.com/DGS-ai-team/DAgents/client/internal/probe"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
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

	policyMode            bool
	policyText            string
	policySnapshot        *nodeapi.PolicySnapshot
	policyTab             policyTab
	policyShellType       string
	policyCursor          int
	policyPendingMode string
	policyShellShowAll    bool

	approvalItems               []clihitl.ToolApprovalItem
	approvalSelected            map[string]bool
	approvalCursor              int
	approvalTriggerDecided      map[string]string
	approvalTriggerRejected     map[string]bool
	approvalTriggerOptionCursor int

	userInfoReq        *clihitl.UserInformationRequest
	userInfoSelected   map[string]bool
	userInfoCursor     int
	userInfoUseOptions bool

	sseConnected bool
	sseDetail    string
	sseFromSeq   int
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

	// viewportFollowTail 为 true 时新输出自动滚到底；用户上滚后置 false，回到底部再恢复。
	viewportFollowTail bool

	toolBlocks      *tuishared.ToolBlockRegistry
	toolPending     *tuishared.ToolPendingTracker
	toolCallStream  *tuishared.ToolCallStreamState
	statusMgr   *statusLineManager

	refreshDebounceUntil time.Time
	submitContentSeen    bool
	turnStartedAt        time.Time
	stallWarnIssued      bool
	sseTurnWarnIssued    bool

	termWidth  int
	termHeight int
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
		helpLine:   "Enter 发送 · Shift+Enter 换行 · Esc 取消 turn · /help",
		children:            newChildAgentTracker(),
		messagesTotalTokens: -1,
		turn:                tuishared.NewTurnGate(),
		viewportFollowTail:  true,
		toolBlocks:          tuishared.NewToolBlockRegistry(),
		toolPending:         tuishared.NewToolPendingTracker(),
		toolCallStream:      tuishared.NewToolCallStreamState(),
		statusMgr:           newStatusLineManager(),
	}

	if err := m.bootstrapSession(initialSession); err != nil {
		return err
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(runCtx), tea.WithMouseCellMotion())
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

	m.sseFromSeq = 0
	if err := m.hydrateSession(); err != nil {
		m.transcript.Add("[system] hydrate 失败: " + err.Error())
	}
	if m.transcript.Len() == 0 {
		welcomeBody := tuishared.FormatWelcomePanelBody(m.probe.Endpoint, m.probe.AgentID, m.probe.Version, id)
		if ctxBody, err := m.client.GetSessionContext(m.ctx, id); err == nil {
			welcomeBody = append(welcomeBody, tuishared.SkillsBloatWarningLines(ctxBody)...)
		}
		m.transcript.AddSystemPanel(tuishared.WelcomePanelTitle(m.probe.Version), welcomeBody)
	}
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
		return m, m.scheduleActiveTickIfNeeded()

	case statusTickMsg:
		m.checkTurnWaitWarnings()
		if len(m.statusMgr.Kinds()) > 0 || m.toolPending.Len() > 0 || m.turn.Awaiting() {
			m.syncViewport()
			return m, m.scheduleActiveTickIfNeeded()
		}
		return m, nil

	case tea.MouseMsg:
		if !m.contextMode && !m.policyMode {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.viewportFollowTail = false
			case tea.MouseButtonWheelDown:
				if m.viewport.AtBottom() {
					m.viewportFollowTail = true
				}
			}
			return m, cmd
		}
		return m, nil

	case streamErrMsg:
		line := msg.line
		if m.turn.Awaiting() {
			line = "SSE 断开 · turn 进行中 · " + line
			if !m.sseTurnWarnIssued {
				m.sseTurnWarnIssued = true
				line += "（重连前可能收不到 Agent 输出）"
			}
		}
		m.transcript.Add("[system] " + line)
		m.syncViewport()
		return m, m.scheduleActiveTickIfNeeded()

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
	if m.tryViewportScrollKey(msg) {
		return m, nil
	}
	if m.contextMode && msg.String() == "esc" {
		m.exitContextView()
		return m, nil
	}
	if m.policyMode {
		return m.handlePolicyKey(msg)
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
				m.resetTurnWaitUI()
				m.turn.FinishTurn()
			}
		}
		return m, nil
	case "ctrl+c":
		m.printResumeHint()
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
		m.clearPartialToolBlocks()
		m.submitContentSeen = false
		m.stallWarnIssued = false
		m.sseTurnWarnIssued = false
		m.transcript.AddBlockGapIfNeeded()
		m.transcript.Add("[user] " + text)
		m.turn.BeginSubmit()
		if !m.submitContentSeen {
			m.statusMgr.Start("prefilling")
		}
		m.syncViewportFollow()
		if err := m.client.SubmitMessage(m.ctx, m.currentSession(), text); err != nil {
			m.turn.FinishTurn()
			m.statusMgr.FinishAll()
			m.resetTurnWaitUI()
			m.errLine = err.Error()
			return m, nil
		}
		m.turnStartedAt = time.Now()
		m.errLine = ""
		return m, m.scheduleActiveTickIfNeeded()
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m *model) View() string {
	status := m.renderStatusBar()
	viewBody := m.viewport.View()

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
	if m.contextMode {
		parts = []string{status, viewBody, help}
	} else if m.mode == modeApproval {
		prompt := m.renderApprovalPrompt()
		parts = []string{status, viewBody, m.renderInputStrip(), prompt, help}
	} else if m.mode == modeUserInfo {
		parts = []string{status, viewBody, m.renderInputStrip()}
		if m.userInfoUseOptions && m.userInfoReq != nil {
			opts := lipgloss.NewStyle().Render(clihitl.FormatUserInformationOptions(m.userInfoReq, m.userInfoSelected, m.userInfoCursor))
			parts = append(parts, opts, help)
		} else {
			if q := m.renderUserInfoQuestionStrip(); q != "" {
				parts = append(parts, q)
			}
			parts = append(parts, inputBox, help)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *model) renderInputStrip() string {
	_, pending := m.children.counts()
	left := m.renderInputStripStyled()
	right := m.renderInputStripRight()
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	text := tuishared.FormatInputStripLine(left, right, width)
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

func (m *model) renderInputStripRight() string {
	var thinking string
	if m.probe != nil && m.probe.LLM.ThinkingSupported {
		thinking = tuishared.FormatLLMThinkingSummary(m.probe.LLM)
	}
	usage := tuishared.FormatInputStripUsage(m.usageStrip)
	if usage == "" {
		usage = tuishared.FormatInputStripTokens(m.messagesTotalTokens)
	}
	return tuishared.FormatInputStripRight(thinking, usage)
}

func (m *model) renderStatusBar() string {
	sse := "SSE 未连接"
	color := tuishared.ThemeSSEFail
	if m.sseConnected {
		sse = "SSE 已连接"
		color = tuishared.ThemeSSEOK
	}
	left := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render("● " + sse)
	if m.sseDetail != "" {
		left += " · " + m.sseDetail
	}
	if m.statusLine != "" {
		left += " · " + m.statusLine
	}
	if m.turn.Awaiting() && m.mode == modeChat && !m.contextMode && !m.policyMode {
		left += m.renderTurnWaitStatus()
	}
	if len(m.hitlQueue) > 0 {
		left += " · 审批/询问"
	}
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	modelName := "—"
	if m.probe != nil && strings.TrimSpace(m.probe.LLM.Model) != "" {
		modelName = m.probe.LLM.Model
	}
	sid := m.currentSession()
	right := fmt.Sprintf("model=%s", truncateID(modelName, 24))
	if width < 100 {
		right += fmt.Sprintf(" · sid=%s", truncateID(sid, 8))
	} else {
		right += fmt.Sprintf(" · session=%s", sid)
	}
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncateID(id string, max int) string {
	id = strings.TrimSpace(id)
	if max <= 0 || len(id) <= max {
		return id
	}
	return id[:max] + "…"
}

func (m *model) applySize(w, h int) {
	if w < minViewWidth {
		w = minViewWidth
	}
	if h < minViewHeight {
		h = minViewHeight
	}
	m.termWidth = w
	m.termHeight = h
	viewH := m.computeViewHeight(h)
	if viewH < 3 {
		viewH = 3
	}
	atBottom := m.viewport.AtBottom()
	yOffset := m.viewport.YOffset
	m.viewport = viewport.New(w, viewH)
	m.viewport.YPosition = 0
	m.input.SetWidth(w - 4)
	if m.contextMode {
		m.refreshContextViewportContent(false, yOffset)
	} else if m.policyMode {
		m.policyRenderViewport()
		m.viewport.SetYOffset(yOffset)
	} else {
		follow := m.viewportFollowTail && atBottom
		m.refreshViewportContent(follow, yOffset)
	}
}

// computeViewHeight 按当前 UI 模式计算 transcript/context viewport 高度。
func (m *model) computeViewHeight(termH int) int {
	if m.contextMode {
		return termH - statusHeight - helpHeight - 1
	}
	viewH := termH - statusHeight - inputStripHeight - inputHeight - helpHeight - 2
	if m.mode == modeApproval {
		promptH := m.approvalPromptHeight()
		return termH - statusHeight - inputStripHeight - promptH - helpHeight - 1
	}
	if m.mode == modeUserInfo {
		extra := 0
		if !m.userInfoUseOptions && m.renderUserInfoQuestionStrip() != "" {
			extra = 1
		}
		return termH - statusHeight - inputStripHeight - inputHeight - helpHeight - 3 - extra
	}
	return viewH
}

// tryViewportScrollKey 将 PgUp/PgDown 等交给 viewport；context 模式始终可滚，chat 模式需输入框为空。
func (m *model) tryViewportScrollKey(msg tea.KeyMsg) bool {
	if m.policyMode {
		return false
	}
	scrolled := false
	switch msg.String() {
	case "pgup":
		m.viewport.ViewUp()
		scrolled = true
		if !m.contextMode {
			m.viewportFollowTail = false
		}
	case "pgdown":
		m.viewport.ViewDown()
		scrolled = true
	case "up", "k":
		if m.contextMode || (m.mode == modeChat && strings.TrimSpace(m.input.Value()) == "") {
			delta := m.viewport.MouseWheelDelta
			if delta <= 0 {
				delta = 3
			}
			m.viewport.LineUp(delta)
			scrolled = true
			if !m.contextMode {
				m.viewportFollowTail = false
			}
		}
	case "down", "j":
		if m.contextMode || (m.mode == modeChat && strings.TrimSpace(m.input.Value()) == "") {
			delta := m.viewport.MouseWheelDelta
			if delta <= 0 {
				delta = 3
			}
			m.viewport.LineDown(delta)
			scrolled = true
		}
	default:
		return false
	}
	if scrolled && !m.contextMode && m.viewport.AtBottom() {
		m.viewportFollowTail = true
	}
	return scrolled
}

// syncViewport 刷新 transcript；用户上滚后保持阅读位置，贴底且 follow 开启时才跟随新输出。
func (m *model) syncViewport() {
	follow := m.viewportFollowTail && m.viewport.AtBottom()
	m.refreshViewportContent(follow, -1)
}

// syncViewportFollow 刷新并强制滚到底（用户主动发消息等场景）。
func (m *model) syncViewportFollow() {
	m.viewportFollowTail = true
	m.refreshViewportContent(true, -1)
}

func (m *model) refreshViewportContent(followBottom bool, preserveYOffset int) {
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	yBefore := preserveYOffset
	if yBefore < 0 {
		yBefore = m.viewport.YOffset
	}
	verbose := false
	if m.toolFold != nil {
		verbose = m.toolFold.Verbose()
	}
	toolOpts := &tuishared.ToolDisplayOptions{Verbose: verbose}
	if m.toolBlocks != nil {
		toolOpts.Registry = m.toolBlocks
	}
	if m.toolPending != nil {
		toolOpts.Pending = m.toolPending
	}
	lines := m.transcript.SnapshotLinesForDisplay(width, toolOpts)
	if m.statusMgr == nil {
		m.viewport.SetContent(strings.Join(lines, "\n"))
		if followBottom {
			m.viewport.GotoBottom()
			m.viewportFollowTail = true
			return
		}
		m.viewport.SetYOffset(yBefore)
		if m.viewport.AtBottom() {
			m.viewportFollowTail = true
		}
		return
	}
	for _, kind := range m.statusMgr.Kinds() {
		if line := m.statusMgr.FormatLine(kind); line != "" {
			lines = append(lines, tuishared.FormatTranscriptLineForDisplay(line, width))
		}
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	if followBottom {
		m.viewport.GotoBottom()
		m.viewportFollowTail = true
		return
	}
	m.viewport.SetYOffset(yBefore)
	if m.viewport.AtBottom() {
		m.viewportFollowTail = true
	}
}

// refreshContextViewportContent 刷新 /context 视图内容并可选保留滚动偏移。
func (m *model) refreshContextViewportContent(followBottom bool, preserveYOffset int) {
	yBefore := preserveYOffset
	if yBefore < 0 {
		yBefore = m.viewport.YOffset
	}
	m.viewport.SetContent(m.contextText)
	if followBottom {
		m.viewport.GotoBottom()
		return
	}
	m.viewport.SetYOffset(yBefore)
}

func (m *model) cancelTurn() error {
	cancelled, err := m.client.CancelTurn(m.ctx, m.currentSession())
	if err != nil {
		return err
	}
	if cancelled {
		m.statusMgr.FinishAll()
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

type statusTickMsg struct{}

func (m *model) notifyViewportRefresh() {
	now := time.Now()
	if now.Before(m.refreshDebounceUntil) {
		return
	}
	m.refreshDebounceUntil = now.Add(60 * time.Millisecond)
	if m.program != nil {
		m.program.Send(refreshViewportMsg{})
	}
}

func (m *model) resetTurnWaitUI() {
	m.turnStartedAt = time.Time{}
	m.stallWarnIssued = false
	m.sseTurnWarnIssued = false
}

func (m *model) clearPartialToolBlocks() {
	if m.toolCallStream != nil {
		m.toolCallStream.Reset()
	}
}

func (m *model) scheduleActiveTickIfNeeded() tea.Cmd {
	if len(m.statusMgr.Kinds()) == 0 && m.toolPending.Len() == 0 && !m.turn.Awaiting() {
		return nil
	}
	interval := time.Second
	if m.toolPending.Len() > 0 {
		interval = 500 * time.Millisecond
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return statusTickMsg{}
	})
}

func (m *model) renderTurnWaitStatus() string {
	elapsed := 0
	if !m.turnStartedAt.IsZero() {
		elapsed = int(time.Since(m.turnStartedAt).Seconds())
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	if !m.sseConnected {
		return style.Render(fmt.Sprintf(" · ⚠ 连接中断 %ds", elapsed))
	}
	if phase := m.statusMgr.ActivePhaseLabel(); phase != "" {
		return style.Render(fmt.Sprintf(" · %s %ds", phase, elapsed))
	}
	if m.submitContentSeen {
		return style.Render(fmt.Sprintf(" · 生成中 %ds", elapsed))
	}
	return style.Render(fmt.Sprintf(" · 等待响应 %ds", elapsed))
}

func (m *model) checkTurnWaitWarnings() {
	if !m.turn.Awaiting() || m.turnStartedAt.IsZero() {
		return
	}
	elapsed := time.Since(m.turnStartedAt)
	if m.stallWarnIssued {
		return
	}
	if !m.sseConnected && elapsed >= 8*time.Second {
		m.stallWarnIssued = true
		secs := int(elapsed.Seconds())
		m.transcript.Add(fmt.Sprintf("[system] 已等待 %ds 且 SSE 未连接，可能无法收到 Agent 输出；请检查 Node 或按 Esc 取消", secs))
		m.syncViewportFollow()
		return
	}
	if m.sseConnected && !m.submitContentSeen && elapsed >= 45*time.Second {
		m.stallWarnIssued = true
		secs := int(elapsed.Seconds())
		m.transcript.Add(fmt.Sprintf("[system] 已等待 %ds，Node 仍未返回内容；若异常可按 Esc 取消", secs))
		m.syncViewportFollow()
	}
}

func (m *model) renderApprovalPrompt() string {
	var body string
	if clihitl.HasTriggerSessionApprovalItems(m.approvalItems) {
		body = clihitl.FormatTriggerSessionApprovalInteractive(
			m.hitlData,
			m.approvalItems,
			m.approvalTriggerDecided,
			m.approvalTriggerRejected,
			m.approvalCursor,
			m.approvalTriggerOptionCursor,
		)
	} else {
		body = clihitl.FormatApprovalInteractive(m.hitlData, m.approvalSelected, m.approvalCursor)
	}
	borderColor := lipgloss.Color("214")
	if clihitl.IsTemporaryAgentApproval(m.hitlData) {
		borderColor = lipgloss.Color("39")
	}
	header := clihitl.ApprovalHeader(m.hitlData)
	if hint := m.approvalQueueHint(); hint != "" {
		header += hint
	}
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(width).
		Render(header + "\n" + body)
}

func (m *model) approvalPromptHeight() int {
	if m.mode != modeApproval {
		return 5
	}
	h := lipgloss.Height(m.renderApprovalPrompt())
	if h < 3 {
		h = 3
	}
	if h > 16 {
		h = 16
	}
	return h
}

func (m *model) renderUserInfoQuestionStrip() string {
	if m.userInfoReq == nil {
		return ""
	}
	q := strings.TrimSpace(m.userInfoReq.Question)
	if q == "" {
		return ""
	}
	width := m.viewport.Width
	if width <= 0 {
		width = 80
	}
	if len(q) > width-4 {
		q = truncateID(q, width-5)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("? " + q)
}

func (m *model) printResumeHint() {
	fmt.Fprintf(os.Stderr, "\n恢复会话: dagents-client tui --session %s\n", m.currentSession())
}
