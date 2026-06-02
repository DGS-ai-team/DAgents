package full

import (
	"strings"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tea "github.com/charmbracelet/bubbletea"
)

const defaultInputPlaceholder = "输入消息… (/help 命令，Enter 发送，Shift+Enter 换行，Esc 取消 turn)"

func (m *model) initApprovalState(data map[string]any) {
	m.hitlData = data
	m.approvalItems = clihitl.ExtractToolApprovals(data)
	m.approvalSelected = make(map[string]bool, len(m.approvalItems))
	m.approvalCursor = 0
}

func (m *model) initUserInfoState(data map[string]any) {
	m.hitlData = data
	m.userInfoReq = clihitl.ExtractUserInformationRequest(data)
	m.userInfoSelected = make(map[string]bool)
	m.userInfoCursor = 0
	m.userInfoUseOptions = m.userInfoReq != nil && len(m.userInfoReq.Options) > 0
}

func (m *model) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m.finishApprovalInteraction(
			clihitl.BuildApprovalSelectionResume(m.hitlData, m.approvalSelected),
			"已提交审批选择",
		)
	default:
		return m, nil
	}
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
		return m, nil
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
	m.userInfoReq = nil
	m.userInfoSelected = nil
	m.userInfoCursor = 0
	m.userInfoUseOptions = false
	m.hitlData = nil
	m.errLine = ""
}
