package full

import (
	"fmt"
	"strings"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tea "github.com/charmbracelet/bubbletea"
)

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
		m.resolveHITL(hitlResult{resume: clihitl.BuildApprovalResume(m.hitlData, true)})
		m.resetHITLState()
		m.statusLine = "已批准全部工具"
		return m, nil
	case "n", "N", "esc":
		m.resolveHITL(hitlResult{resume: clihitl.BuildApprovalResume(m.hitlData, false)})
		m.resetHITLState()
		m.statusLine = "已拒绝全部工具"
		return m, nil
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
		m.resolveHITL(hitlResult{resume: clihitl.BuildApprovalSelectionResume(m.hitlData, m.approvalSelected)})
		m.resetHITLState()
		m.statusLine = "已提交审批选择"
		return m, nil
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
		m.resolveHITL(hitlResult{err: fmt.Errorf("用户取消")})
		m.resetHITLState()
		m.input.SetValue("")
		m.statusLine = "已取消回答"
		return m, nil
	case "enter":
		answer := strings.TrimSpace(m.input.Value())
		if answer == "" {
			m.errLine = "回答不能为空"
			return m, nil
		}
		m.resolveHITL(hitlResult{resume: clihitl.BuildUserInformationResume(m.userInfoReq, answer, nil, false)})
		m.resetHITLState()
		m.input.SetValue("")
		m.input.Placeholder = defaultInputPlaceholder
		m.statusLine = "已提交回答"
		return m, nil
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
		m.resolveHITL(hitlResult{err: fmt.Errorf("用户取消")})
		m.resetHITLState()
		m.statusLine = "已取消回答"
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
		m.resolveHITL(hitlResult{resume: rv})
		m.resetHITLState()
		m.statusLine = "已提交回答"
		return m, nil
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
	m.errLine = ""
}

const defaultInputPlaceholder = "输入消息… (/help 命令，Enter 发送，Shift+Enter 换行，Esc 取消 turn)"
