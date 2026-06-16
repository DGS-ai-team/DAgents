package full

import (
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func (m *model) ensureA2ARelayApprovalToolBlocks(data map[string]any) {
	if !clihitl.IsA2ARelayHITL(data) {
		return
	}
	suffix := clihitl.A2ARelayToolSuffix(data)
	for _, item := range clihitl.ExtractToolApprovals(data) {
		if item.CallID == "" {
			continue
		}
		if m.toolCallStream != nil && m.toolCallStream.HasActiveBlock(item.CallID) {
			continue
		}
		if m.toolPending != nil {
			// A2A 中继不启动黄点耗时动画；仅占位展示。
			m.toolPending.Remove(item.CallID)
		}
		title := tuishared.ToolDisplayName(item.Name, item.Arguments)
		lines := tuishared.FormatA2ARelayApprovalPending(item.CallID, title, suffix, item.RawArgs)
		tuishared.UpsertToolCallLines(m.transcript, m.toolCallStream, item.CallID, "", lines)
		m.toolBlocks.Register(item.CallID)
	}
	m.notifyViewportRefresh()
}

func (m *model) finalizeA2ARelayToolBlocks(hitlData, resume map[string]any) {
	if !clihitl.IsA2ARelayHITL(hitlData) {
		return
	}
	approved, rejected := clihitl.ParseApprovalResumeSelection(resume, hitlData)
	suffix := clihitl.A2ARelayToolSuffix(hitlData)
	for _, item := range clihitl.ExtractToolApprovals(hitlData) {
		id := item.CallID
		if id == "" {
			continue
		}
		ok := approved[id]
		if rejected[id] {
			ok = false
		}
		if m.toolCallStream != nil {
			m.toolCallStream.ForgetBlock(id)
		}
		if m.toolPending != nil {
			m.toolPending.Remove(id)
		}
		title := tuishared.ToolDisplayName(item.Name, item.Arguments)
		lines := tuishared.FormatA2ARelayToolResult(id, title, suffix, ok)
		m.transcript.ReplaceA2ARelayToolLines(id, lines)
		m.toolBlocks.Register(id)
	}
	m.notifyViewportRefresh()
}
