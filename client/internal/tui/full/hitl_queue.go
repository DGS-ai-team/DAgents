package full

import (
	"fmt"

	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tea "github.com/charmbracelet/bubbletea"
)

type hitlPendingKind int

const (
	hitlPendingApproval hitlPendingKind = iota
	hitlPendingUserInfo
)

type hitlPending struct {
	kind hitlPendingKind
	data map[string]any
}

// pendingHITLChangedMsg 通知主循环刷新 HITL 面板或状态条。
type pendingHITLChangedMsg struct{}

// hitlSubmitResultMsg 为异步 SubmitResume 结果。
type hitlSubmitResultMsg struct {
	err error
}

func (m *model) resetHITLQueue() {
	m.hitlQueue = nil
	m.hitlData = nil
}

func (m *model) enqueueApproval(data map[string]any) {
	m.hitlQueue = append(m.hitlQueue, hitlPending{kind: hitlPendingApproval, data: data})
	if id := clihitl.ChildSessionIDFromData(data); id != "" {
		m.children.setAwaitingApproval(id, true)
	}
}

func (m *model) enqueueUserInfo(data map[string]any) {
	m.hitlQueue = append(m.hitlQueue, hitlPending{kind: hitlPendingUserInfo, data: data})
}

func (m *model) pendingHITLCount() int {
	return len(m.hitlQueue)
}

// showNextHITLIfIdle 在空闲时展示队首 HITL（不阻塞 SSE）。
func (m *model) showNextHITLIfIdle() {
	if len(m.hitlQueue) == 0 {
		if m.mode == modeApproval || m.mode == modeUserInfo {
			m.resetHITLState()
		}
		return
	}
	if m.mode == modeApproval || m.mode == modeUserInfo {
		return
	}
	head := m.hitlQueue[0]
	switch head.kind {
	case hitlPendingApproval:
		m.initApprovalState(head.data)
		m.hitlPrompt = clihitl.FormatApprovalPrompt(head.data)
		m.mode = modeApproval
		if clihitl.IsChildAgentApproval(head.data) {
			m.statusLine = "子任务等待审批…"
		} else {
			m.statusLine = "等待审批…"
		}
	case hitlPendingUserInfo:
		m.initUserInfoState(head.data)
		if m.userInfoReq != nil {
			m.hitlPrompt = m.userInfoReq.Question
		} else {
			m.hitlPrompt = clihitl.FormatUserInformationQuestion(head.data)
		}
		m.mode = modeUserInfo
		m.input.SetValue("")
		if m.userInfoUseOptions {
			m.input.Placeholder = "使用 ↑/↓ + Space 选择选项"
		} else {
			m.input.Placeholder = "输入回答后 Enter 提交 · Esc 取消"
		}
		m.statusLine = "等待用户回答…"
	default:
	}
}

func (m *model) popHITLQueueHead() {
	if len(m.hitlQueue) == 0 {
		return
	}
	head := m.hitlQueue[0]
	if head.kind == hitlPendingApproval {
		if id := clihitl.ChildSessionIDFromData(head.data); id != "" {
			m.children.setAwaitingApproval(id, false)
		}
	}
	m.hitlQueue = m.hitlQueue[1:]
}

func (m *model) approvalQueueHint() string {
	n := len(m.hitlQueue)
	if n <= 1 {
		return ""
	}
	return fmt.Sprintf("（队列 %d）", n)
}

func (m *model) renderInputStripStyled() string {
	active, pending := m.children.counts()
	var left string
	if active == 0 && pending == 0 {
		left = "子 Agent: —"
	} else {
		left = fmt.Sprintf("子 Agent: %d 活跃", active)
		if pending > 0 {
			left += fmt.Sprintf(" · %d 待审批", pending)
		}
	}
	if hint := m.approvalQueueHint(); hint != "" {
		left += " " + hint
	}
	return left
}

func (m *model) cmdSubmitResume(resume map[string]any) tea.Cmd {
	sid := m.currentSession()
	client := m.client
	ctx := m.ctx
	return func() tea.Msg {
		err := client.SubmitResume(ctx, sid, resume)
		return hitlSubmitResultMsg{err: err}
	}
}

func (m *model) finishApprovalInteraction(resume map[string]any, statusLine string) (tea.Model, tea.Cmd) {
	m.popHITLQueueHead()
	m.resetHITLState()
	m.statusLine = statusLine
	m.showNextHITLIfIdle()
	return m, m.cmdSubmitResume(resume)
}

func (m *model) finishUserInfoInteraction(resume map[string]any, statusLine string) (tea.Model, tea.Cmd) {
	m.popHITLQueueHead()
	m.resetHITLState()
	m.input.SetValue("")
	m.input.Placeholder = defaultInputPlaceholder
	m.statusLine = statusLine
	m.showNextHITLIfIdle()
	return m, m.cmdSubmitResume(resume)
}
