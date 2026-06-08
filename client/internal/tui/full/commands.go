package full

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	"github.com/DGS-ai-team/DAgents/client/internal/version"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
)

func (m *model) execCommand(line string) (quit bool, err error) {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false, nil
	}
	cmd := strings.ToLower(strings.TrimPrefix(parts[0], "/"))
	switch cmd {
	case "help", "h", "?":
		m.transcript.Add("[system] " + strings.TrimSpace(`命令: /status /context /compress /sessions /switch /new /clear /cancel /children /skill /tools /reasoning /quit`))
		m.syncViewport()
	case "context":
		err = m.enterContextView()
	case "compress":
		err = m.runCompress()
	case "skill":
		err = m.handleSkillCommand(parts[1:])
	case "status":
		err = m.appendStatus()
	case "sessions", "ls":
		err = m.appendSessions()
	case "switch":
		if len(parts) < 2 {
			return false, fmt.Errorf("用法: /switch <session_id>")
		}
		err = m.switchSession(parts[1])
	case "new":
		err = m.switchSession("")
	case "clear":
		err = m.client.ClearSessionContext(m.ctx, m.currentSession())
		if err == nil {
			m.messagesTotalTokens = 0
			m.resetUsageStrip()
			m.transcript.Add("[system] 已清空对话上下文")
			m.syncViewport()
		}
	case "cancel":
		err = m.cancelTurn()
		if err == nil {
			m.turn.FinishTurn()
			m.statusLine = "已取消在途 turn"
		}
	case "children", "child":
		err = m.appendChildren()
		if err == nil {
			m.statusLine = "已刷新子 Agent 列表"
		}
	case "tools":
		if len(parts) > 1 && strings.EqualFold(parts[1], "verbose") {
			m.toolFold.SetVerbose(true)
			m.statusLine = "tool 输出：详细"
		} else if len(parts) > 1 && strings.EqualFold(parts[1], "brief") {
			m.toolFold.SetVerbose(false)
			m.statusLine = "tool 输出：折叠"
		} else {
			mode := "折叠"
			if m.toolFold.Verbose() {
				mode = "详细"
			}
			m.statusLine = "tool 输出: " + mode
		}
	case "reasoning":
		if len(parts) > 1 {
			switch strings.ToLower(parts[1]) {
			case "on", "true", "1":
				m.showReasoning = true
			case "off", "false", "0":
				m.showReasoning = false
			default:
				return false, fmt.Errorf("用法: /reasoning on|off")
			}
		}
		mode := "关闭"
		if m.showReasoning {
			mode = "开启"
		}
		m.statusLine = "reasoning 显示: " + mode
	case "quit", "exit", "q":
		return true, nil
	default:
		return false, fmt.Errorf("未知命令 %q", cmd)
	}
	return false, err
}

func (m *model) enterContextView() error {
	ctxBody, err := m.client.GetSessionContext(m.ctx, m.currentSession())
	if err != nil {
		return err
	}
	m.applyContextTokensFromView(ctxBody)
	m.contextText = tuishared.FormatSessionContext(ctxBody)
	m.contextMode = true
	m.helpLine = "Esc 返回聊天记录"
	m.viewport.SetContent(m.contextText)
	m.statusLine = "context 视图"
	return nil
}

func (m *model) runCompress() error {
	if m.turn.Awaiting() {
		return fmt.Errorf("当前 turn 进行中，请稍后再试")
	}
	m.statusLine = "正在压缩上下文…"
	m.syncViewport()
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Minute)
	defer cancel()
	res, err := m.client.CompressSessionContext(ctx, m.currentSession())
	if err != nil {
		m.statusLine = ""
		return err
	}
	line := clihitl.FormatContextCompression("context_compression_blocking", map[string]any{
		"phase":                    "end",
		"status":                   res.Status,
		"compressed_message_count": res.CompressedMessageCount,
	})
	if line == "" {
		switch res.Status {
		case "noop":
			line = "[compression] 当前上下文无需压缩（消息不足或无可压缩区间）"
		case "disabled":
			line = "[compression] 压缩未启用（Node 未配置 LLM 或 compression）"
		case "unsupported":
			line = "[compression] 子 Agent session 不支持手动压缩"
		case "in_progress":
			mode := res.TriggerLevel
			if mode == "" {
				mode = "unknown"
			}
			if res.CompressedMessageCount > 0 {
				line = fmt.Sprintf("[compression] 已有压缩任务进行中（%s，目标 %d 条）", mode, res.CompressedMessageCount)
			} else {
				line = fmt.Sprintf("[compression] 已有压缩任务进行中（%s）", mode)
			}
		default:
			line = fmt.Sprintf("[compression] 压缩结束（status=%s）", res.Status)
		}
	}
	m.transcript.Add("[system] " + line)
	if res.Status == "in_progress" {
		m.statusLine = "压缩进行中"
		m.syncViewport()
		return nil
	}
	if res.MessagesTotalTokens > 0 {
		m.messagesTotalTokens = res.MessagesTotalTokens
	} else {
		m.scheduleContextTokenRefresh()
	}
	m.statusLine = "压缩完成"
	m.syncViewport()
	return nil
}

func (m *model) exitContextView() {
	m.contextMode = false
	m.contextText = ""
	m.helpLine = "Enter 发送 · Shift+Enter 换行 · Esc 取消 turn · /help 命令 · /quit 退出"
	m.syncViewport()
	m.statusLine = ""
}

func (m *model) handleSkillCommand(args []string) error {
	sid := m.currentSession()
	if len(args) == 0 {
		sk, err := m.client.ListSessionSkills(m.ctx, sid)
		if err != nil {
			return err
		}
		m.transcript.Add("[system] " + tuishared.FormatSessionSkills(sk))
		m.syncViewport()
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "load":
		if len(args) < 2 {
			return fmt.Errorf("用法: /skill load NAME")
		}
		sk, err := m.client.LoadSessionSkill(m.ctx, sid, args[1])
		if err != nil {
			return err
		}
		m.transcript.Add("[system] 已加载 skill " + args[1])
		m.transcript.Add("[system] " + tuishared.FormatSessionSkills(sk))
		m.syncViewport()
	case "unload":
		if len(args) < 2 {
			return fmt.Errorf("用法: /skill unload NAME")
		}
		sk, err := m.client.UnloadSessionSkill(m.ctx, sid, args[1])
		if err != nil {
			return err
		}
		m.transcript.Add("[system] 已卸载 skill " + args[1])
		m.transcript.Add("[system] " + tuishared.FormatSessionSkills(sk))
		m.syncViewport()
	default:
		return fmt.Errorf("未知 skill 子命令 %q（可用 load/unload）", args[0])
	}
	return nil
}

func (m *model) switchSession(requested string) error {
	m.stopStream()
	id, err := m.client.CreateSession(m.ctx, requested)
	if err != nil {
		return err
	}
	m.sessionMu.Lock()
	m.sessionID = id
	m.sessionMu.Unlock()
	m.turn.Reset()
	m.resetHITLQueue()
	m.resetHITLState()
	m.children.reset()
	m.messagesTotalTokens = -1
	m.resetUsageStrip()
	m.transcript.Add("[system] 已切换 session=" + id)
	m.syncViewport()
	m.restartStream()
	m.statusLine = "session 已切换"
	return nil
}

func (m *model) appendStatus() error {
	ctxBody, err := m.client.GetSessionContext(m.ctx, m.currentSession())
	if err != nil {
		return err
	}
	line := fmt.Sprintf(
		"status agent=%s node=%s client=%s msgs=%d queue=%d active_turn=%v",
		m.probe.AgentID, m.probe.Version, version.Version,
		ctxBody.MessagesCount, ctxBody.QueuePending, ctxBody.HasActiveTurn,
	)
	if ctxBody.TurnState != "" {
		line += " state=" + ctxBody.TurnState
	}
	m.transcript.Add("[system] " + line)
	m.syncViewport()
	return nil
}

func (m *model) appendSessions() error {
	items, err := m.client.ListSessions(m.ctx)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		m.transcript.Add("[system] (无 session)")
		m.syncViewport()
		return nil
	}
	cur := m.currentSession()
	for _, s := range items {
		mark := " "
		if s.SessionID == cur {
			mark = "*"
		}
		preview := s.FirstUserMessage
		if len(preview) > 40 {
			preview = preview[:40] + "..."
		}
		m.transcript.Add(fmt.Sprintf("[system] %s %s msgs=%d %s", mark, s.SessionID, s.MessageCount, preview))
	}
	m.syncViewport()
	return nil
}

func (m *model) appendChildren() error {
	items, err := m.client.ListChildAgents(m.ctx, m.currentSession())
	if err != nil {
		return err
	}
	m.children.replaceFromAPI(items)
	for _, p := range m.hitlQueue {
		if p.kind != hitlPendingApproval {
			continue
		}
		if id := clihitl.ChildSessionIDFromData(p.data); id != "" {
			m.children.setAwaitingApproval(id, true)
		}
	}
	awaiting := m.children.awaitingApprovalMap()
	m.transcript.Add("[system] " + tuishared.FormatChildAgentsList(items, awaiting))
	m.syncViewport()
	return nil
}

func (m *model) restartStream() {
	m.stopStream()
	streamCtx, cancel := context.WithCancel(m.ctx)
	m.streamCancel = cancel
	m.streamDone = make(chan struct{})
	go func() {
		defer close(m.streamDone)
		runSSELoop(streamCtx, m)
	}()
}

func (m *model) stopStream() {
	if m.streamCancel == nil {
		return
	}
	m.streamCancel()
	<-m.streamDone
	m.streamCancel = nil
}

func runSSELoop(ctx context.Context, m *model) {
	const reconnectDelay = 5 * time.Second
	lastSeq := 0
	for {
		if ctx.Err() != nil {
			return
		}
		fromSeq := lastSeq
		m.sseConnected = true
		m.sseDetail = "订阅中"
		if m.program != nil {
			m.program.Send(refreshViewportMsg{})
			m.program.Send(syncChildAgentsMsg{})
			m.program.Send(refreshContextTokensMsg{})
		}

		err := m.client.StreamEvents(ctx, m.currentSession(), fromSeq, func(ev nodeapi.StreamEvent) bool {
			if ev.Seq > lastSeq {
				lastSeq = ev.Seq
			}
			m.onStreamEvent(ev)
			return true
		})
		m.sseConnected = false
		if ctx.Err() != nil {
			return
		}
		detail := "已断开"
		if err != nil {
			detail = fmt.Sprintf("断开: %v", err)
		}
		m.sseDetail = detail + " · " + strconv.Itoa(int(reconnectDelay.Seconds())) + "s 后重连"
		if m.program != nil {
			m.program.Send(streamErrMsg{line: m.sseDetail})
			m.program.Send(refreshViewportMsg{})
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(reconnectDelay):
		}
	}
}
