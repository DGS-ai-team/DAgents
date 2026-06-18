package full

import (
	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
	tea "github.com/charmbracelet/bubbletea"
)

// refreshContextTokensMsg 触发异步拉取 session context token 统计。
type refreshContextTokensMsg struct{}

// contextTokensSyncedMsg 为 GET /context 的 messages_total_tokens 结果。
type contextTokensSyncedMsg struct {
	tokens int
}

func (m *model) clearUsageStrip() {
	m.usageStrip = tuishared.UsageStripSnapshot{}
}

func (m *model) applyUsageFromSSE(data map[string]any) {
	tuishared.ApplyUsageRoundToStrip(&m.usageStrip, data)
}

func (m *model) scheduleContextTokenRefresh() {
	if m.program != nil {
		m.program.Send(refreshContextTokensMsg{})
	}
}

func (m *model) cmdRefreshContextTokens() tea.Cmd {
	sid := m.currentSession()
	client := m.client
	ctx := m.ctx
	return func() tea.Msg {
		body, err := client.GetSessionContext(ctx, sid)
		if err != nil {
			return contextTokensSyncedMsg{tokens: -1}
		}
		return contextTokensSyncedMsg{tokens: body.MessagesTotalTokens}
	}
}

func (m *model) applyContextTokensFromView(body *nodeapi.SessionContext) {
	if body == nil {
		return
	}
	m.messagesTotalTokens = body.MessagesTotalTokens
}
