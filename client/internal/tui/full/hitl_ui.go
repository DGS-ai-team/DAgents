package full

import (
	"strings"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tea "github.com/charmbracelet/bubbletea"
)

// defaultInputPlaceholder 为 chat 模式输入框占位符。
//
// 须以 ASCII 字符开头：bubbles textarea 在渲染 placeholder 时对 plines[0][0]
// 按字节取首字符作假光标，UTF-8 中文首字会被截断为乱码（如「输」→「è」）。
const defaultInputPlaceholder = "> 输入消息... (/help 命令, Enter 发送, Shift+Enter 换行, Esc 取消 turn)"

func (m *model) initApprovalState(data map[string]any) {
	m.hitlData = data
	m.approvalItems = clihitl.ExtractToolApprovals(data)
	m.approvalSelected = make(map[string]bool, len(m.approvalItems))
	m.approvalCursor = 0
	m.approvalTriggerDecided = make(map[string]string)
	m.approvalTriggerRejected = make(map[string]bool)
	m.approvalTriggerOptionCursor = 0
}

func (m *model) initUserInfoState(data map[string]any) {
	m.hitlData = data
	m.userInfoReq = clihitl.ExtractUserInformationRequest(data)
	m.userInfoSelected = make(map[string]bool)
	m.userInfoCursor = 0
	m.userInfoUseOptions = m.userInfoReq != nil && len(m.userInfoReq.Options) > 0
}

func (m *model) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if clihitl.HasTriggerSessionApprovalItems(m.approvalItems) {
		return m.handleTriggerSessionApprovalKey(msg)
	}
	switch msg.String() {
	case "y", "Y":
		return m.finishApprovalInteraction(
			clihitl.BuildApprovalResume(m.hitlData, true),
			"已批准全部工具",
		)
	case "n", "N", "esc":
		return m.finishApprovalInteraction(
			clihitl.BuildApprovalResume(m.hitlData, false),
			"已拒绝全部工具",
		)
	case "up", "k":
		if m.approvalCursor > 0 {
			m.approvalCursor--
		}
		return m, nil
	case "down", "j":
		if m.approvalCursor < len(m.approvalItems)-1 {
			m.approvalCursor++
		}
		return m, nil
	case " ":
		if len(m.approvalItems) == 0 {
			return m, nil
		}
		id := m.approvalItems[m.approvalCursor].CallID
		m.approvalSelected[id] = !m.approvalSelected[id]
		return m, nil
	case "enter":
		resolved := clihitl.ResolveApprovalSelection(m.approvalItems, m.approvalSelected, m.approvalCursor)
		return m.finishApprovalInteraction(
			clihitl.BuildApprovalSelectionResume(m.hitlData, resolved),
			"已提交审批选择",
		)
	default:
		return m, nil
	}
}

func (m *model) handleTriggerSessionApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	options := clihitl.TriggerSessionOptions()
	switch msg.String() {
	case "y", "Y":
		return m.finishApprovalInteraction(
			clihitl.BuildTriggerSessionQuickResume(m.hitlData, m.approvalItems, true),
			"已批准（同会话）",
		)
	case "n", "N", "esc":
		return m.finishApprovalInteraction(
			clihitl.BuildTriggerSessionQuickResume(m.hitlData, m.approvalItems, false),
			"已拒绝",
		)
	case "up", "k":
		if m.approvalTriggerOptionCursor > 0 {
			m.approvalTriggerOptionCursor--
		}
		return m, nil
	case "down", "j":
		if m.approvalTriggerOptionCursor < len(options)-1 {
			m.approvalTriggerOptionCursor++
		}
		return m, nil
	case "enter":
		current := triggerSessionCurrentItem(m.approvalItems, m.approvalTriggerDecided, m.approvalTriggerRejected)
		if current == nil {
			return m.finishApprovalInteraction(
				clihitl.BuildTriggerSessionApprovalResume(m.hitlData, m.approvalItems, m.approvalTriggerDecided, m.approvalTriggerRejected),
				"已提交审批选择",
			)
		}
		opt := options[m.approvalTriggerOptionCursor]
		if strings.TrimSpace(opt.Target) == "" {
			m.approvalTriggerRejected[current.CallID] = true
		} else {
			m.approvalTriggerDecided[current.CallID] = opt.Target
		}
		m.approvalTriggerOptionCursor = 0
		if triggerSessionCurrentItem(m.approvalItems, m.approvalTriggerDecided, m.approvalTriggerRejected) == nil {
			return m.finishApprovalInteraction(
				clihitl.BuildTriggerSessionApprovalResume(m.hitlData, m.approvalItems, m.approvalTriggerDecided, m.approvalTriggerRejected),
				"已提交审批选择",
			)
		}
		return m, nil
	default:
		return m, nil
	}
}

func triggerSessionCurrentItem(items []clihitl.ToolApprovalItem, decided map[string]string, rejected map[string]bool) *clihitl.ToolApprovalItem {
	for i := range items {
		it := items[i]
		if !clihitl.IsTriggerSessionApprovalItem(it) {
			continue
		}
		if rejected[it.CallID] {
			continue
		}
		if _, ok := decided[it.CallID]; ok {
			continue
		}
		return &items[i]
	}
	return nil
}

func (m *model) handleUserInfoKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.userInfoUseOptions && m.userInfoReq != nil {
		return m.handleUserInfoOptionsKey(msg)
	}
	switch msg.String() {
	case "esc":
		m.popHITLQueueHead()
		m.resetHITLState()
		m.input.SetValue("")
		m.statusLine = "已取消回答"
		m.showNextHITLIfIdle()
		return m, m.refocusInputIfNeeded()
	case "enter":
		answer := strings.TrimSpace(m.input.Value())
		if answer == "" {
			m.errLine = "回答不能为空"
			return m, nil
		}
		return m.finishUserInfoInteraction(
			clihitl.BuildUserInformationResume(m.userInfoReq, answer, nil, false),
			"已提交回答",
		)
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m *model) handleUserInfoOptionsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	req := m.userInfoReq
	if req == nil {
		return m, nil
	}
	switch msg.String() {
	case "esc":
		m.popHITLQueueHead()
		m.resetHITLState()
		m.statusLine = "已取消回答"
		m.showNextHITLIfIdle()
		return m, nil
	case "up", "k":
		if m.userInfoCursor > 0 {
			m.userInfoCursor--
		}
		return m, nil
	case "down", "j":
		if m.userInfoCursor < len(req.Options)-1 {
			m.userInfoCursor++
		}
		return m, nil
	case " ":
		if len(req.Options) == 0 {
			return m, nil
		}
		id := req.Options[m.userInfoCursor].ID
		if req.AllowMultiple {
			m.userInfoSelected[id] = !m.userInfoSelected[id]
		} else {
			m.userInfoSelected = map[string]bool{id: true}
		}
		return m, nil
	case "enter":
		rv, err := clihitl.BuildUserInformationResumeFromOptions(req, m.userInfoSelected)
		if err != nil {
			m.errLine = err.Error()
			return m, nil
		}
		return m.finishUserInfoInteraction(rv, "已提交回答")
	default:
		return m, nil
	}
}

func (m *model) resetHITLState() {
	m.mode = modeChat
	m.approvalItems = nil
	m.approvalSelected = nil
	m.approvalCursor = 0
	m.approvalTriggerDecided = nil
	m.approvalTriggerRejected = nil
	m.approvalTriggerOptionCursor = 0
	m.userInfoReq = nil
	m.userInfoSelected = nil
	m.userInfoCursor = 0
	m.userInfoUseOptions = false
	m.hitlData = nil
	m.errLine = ""
}
