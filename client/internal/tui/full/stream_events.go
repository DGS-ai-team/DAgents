package full

import (
	"fmt"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
	tea "github.com/charmbracelet/bubbletea"
)

// syncChildAgentsMsg 触发 HTTP 对齐子 Agent 列表（SSE 重连后）。
type syncChildAgentsMsg struct{}

// childAgentsSyncedMsg 为 ListChildAgents 结果。
type childAgentsSyncedMsg struct {
	items []nodeapi.ChildAgentListItem
	err   error
}

// onStreamEvent 处理 SSE 事件；HITL 仅入队不阻塞，避免丢失父 assistant 尾部。
func (m *model) onStreamEvent(ev nodeapi.StreamEvent) {
	m.turn.NoteSeq(ev.Seq)
	if m.turn.IsStale(ev.Seq) {
		return
	}
	switch ev.Type {
	case "assistant":
		if clihitl.ShouldSkipChildRuntimeDisplay(ev.Type, ev.Data) {
			return
		}
		m.turn.MarkTurnContent()
		m.submitContentSeen = true
		m.statusMgr.Finish("prefilling")
		m.statusMgr.Finish("thinking")
		if text, ok := ev.Data["content"].(string); ok && text != "" {
			m.transcript.AppendPartial("assistant", text)
			m.notifyViewportRefresh()
		}
	case "reasoning":
		if clihitl.ShouldSkipChildRuntimeDisplay(ev.Type, ev.Data) {
			return
		}
		m.turn.MarkTurnContent()
		m.submitContentSeen = true
		m.statusMgr.Finish("prefilling")
		if !m.statusMgr.Has("thinking") {
			m.statusMgr.Start("thinking")
		}
		if !m.showReasoning {
			return
		}
		if text, ok := ev.Data["content"].(string); ok && text != "" {
			m.transcript.AppendPartial("reasoning", text)
			m.notifyViewportRefresh()
		}
	case "tool_call", "tool_result":
		if clihitl.ShouldSkipChildRuntimeDisplay(ev.Type, ev.Data) {
			return
		}
		m.turn.MarkTurnContent()
		m.submitContentSeen = true
		m.statusMgr.FinishAll()
		m.transcript.FinishPartial("assistant")
		m.transcript.FinishPartial("reasoning")
		blockID := tuishared.ToolCallIDFromEvent(ev.Data)
		if blockID == "" {
			blockID = tuishared.ToolEventID(ev.Data)
		}
		if ev.Type == "tool_call" {
			m.children.noteToolCall(ev.Data)
			if blockID = tuishared.HandleToolCallEvent(m.transcript, m.toolCallStream, ev.Data, m.toolFold.Verbose(), m.toolPending); blockID != "" {
				m.toolBlocks.Register(blockID)
			}
		} else {
			if blockID == "" {
				blockID = m.toolBlocks.NextSeqID()
			}
			m.children.noteToolResult(ev.Data)
			if m.toolCallStream != nil {
				m.toolCallStream.ForgetBlock(blockID)
			}
			var elapsed float64 = -1
			if raw := ev.Data["duration_seconds"]; raw != nil {
				switch v := raw.(type) {
				case float64:
					elapsed = v
				case int:
					elapsed = float64(v)
				case int64:
					elapsed = float64(v)
				}
				if elapsed < 0 {
					elapsed = 0
				}
			}
			if elapsed < 0 {
				elapsed = m.toolPending.ElapsedSeconds(blockID)
			}
			m.toolPending.Remove(blockID)
			m.transcript.RemoveToolPendingLines(blockID)
			m.toolBlocks.Register(blockID)
			for _, line := range tuishared.FormatToolEventWithID(ev.Type, ev.Data, blockID, m.toolFold.Verbose(), elapsed) {
				m.transcript.Add(line)
			}
		}
		m.notifyViewportRefresh()
	case "usage":
		if clihitl.ShouldSkipChildRuntimeDisplay(ev.Type, ev.Data) {
			return
		}
		m.usageStrip = tuishared.ParseUsageStrip(ev.Data)
		if suffix := tuishared.FormatInlineUsage(tuishared.ParseUsageRound(ev.Data)); suffix != "" {
			m.transcript.ApplyRoundUsage(suffix)
		}
		m.notifyStripRefresh()
		m.notifyViewportRefresh()
	case "context_compression_blocking", "context_compression_silent":
		phase := strings.TrimSpace(fmt.Sprint(ev.Data["phase"]))
		if ev.Type == "context_compression_blocking" && phase != "end" {
			m.statusMgr.Start("compression")
		} else {
			m.statusMgr.Finish("compression")
		}
		line := clihitl.FormatContextCompression(ev.Type, ev.Data)
		if line != "" {
			m.transcript.Add("[system] " + line)
		}
		m.notifyViewportRefresh()
		m.scheduleContextTokenRefresh()
	case "error":
		msg := strings.TrimSpace(fmt.Sprint(ev.Data["message"]))
		if msg == "" {
			msg = "unknown error"
		}
		m.transcript.Add("[system] error: " + msg)
		m.notifyViewportRefresh()
		m.statusMgr.FinishAll()
		if m.turn.Awaiting() {
			m.resetTurnWaitUI()
			m.turn.FinishTurn()
			m.statusLine = "回合结束"
		}
	case "temporary_agent_created":
		m.children.onCreated(ev.Data)
		if line := clihitl.FormatChildLifecycleLine(ev.Type, ev.Data); line != "" {
			m.transcript.Add("[system] " + line)
			m.notifyViewportRefresh()
		} else {
			m.notifyStripRefresh()
		}
	case "temporary_agent_completed":
		id := clihitl.ChildSessionIDFromData(ev.Data)
		m.children.onFinished(id)
		if !m.children.shouldSuppressLifecycle(id, ev.Type) {
			if line := clihitl.FormatChildLifecycleLine(ev.Type, ev.Data); line != "" {
				m.transcript.Add("[system] " + line)
				m.notifyViewportRefresh()
				break
			}
		}
		m.notifyStripRefresh()
	case "temporary_agent_cancelled":
		id := clihitl.ChildSessionIDFromData(ev.Data)
		m.children.onFinished(id)
		if !m.children.shouldSuppressLifecycle(id, ev.Type) {
			if line := clihitl.FormatChildLifecycleLine(ev.Type, ev.Data); line != "" {
				m.transcript.Add("[system] " + line)
				m.notifyViewportRefresh()
				break
			}
		}
		m.notifyStripRefresh()
	case "approval_required":
		m.enqueueApproval(ev.Data)
		m.notifyHITLChanged()
	case "user_information_required":
		if clihitl.ShouldSkipChildRuntimeDisplay(ev.Type, ev.Data) {
			return
		}
		m.enqueueUserInfo(ev.Data)
		m.notifyHITLChanged()
	case "done":
		if clihitl.ShouldSkipChildRuntimeDisplay(ev.Type, ev.Data) {
			return
		}
		m.transcript.FinishPartial("assistant")
		m.transcript.FinishPartial("reasoning")
		m.statusMgr.FinishAll()
		if m.turn.ShouldAcceptDone(ev.Seq) {
			m.resetTurnWaitUI()
			m.clearPartialToolBlocks()
			m.turn.FinishTurn()
			m.statusLine = "回合结束"
		}
		m.notifyViewportRefresh()
		m.scheduleContextTokenRefresh()
	default:
	}
}

func (m *model) notifyStripRefresh() {
	if m.program != nil {
		m.program.Send(refreshViewportMsg{})
	}
}

func (m *model) notifyHITLChanged() {
	if m.program != nil {
		m.program.Send(pendingHITLChangedMsg{})
	}
}

func (m *model) cmdSyncChildAgents() tea.Cmd {
	sid := m.currentSession()
	return func() tea.Msg {
		items, err := m.client.ListChildAgents(m.ctx, sid)
		return childAgentsSyncedMsg{items: items, err: err}
	}
}
