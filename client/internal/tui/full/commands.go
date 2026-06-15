package full

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	"github.com/DGS-ai-team/DAgents/client/internal/probe"
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
		m.transcript.AddSystemPanel("命令", tuishared.FormatHelpPanelBody())
		m.syncViewport()
	case "context":
		err = m.enterContextView()
	case "policy":
		err = m.enterPolicyView()
	case "triggers", "trigger":
		err = m.appendTriggers()
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
		} else if len(parts) > 1 && strings.EqualFold(parts[1], "expand") {
			if id := m.toolBlocks.ExpandLast(); id != "" {
				m.syncViewport()
				m.statusLine = "已展开 " + id
			}
		} else if len(parts) > 1 && strings.EqualFold(parts[1], "collapse") {
			if id := m.toolBlocks.CollapseLast(); id != "" {
				m.syncViewport()
				m.statusLine = "已收起 " + id
			}
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
	case "thinking":
		err = m.handleThinkingCommand(parts[1:])
	case "quit", "exit", "q":
		m.printResumeHint()
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
	m.contextText = tuishared.FormatSessionContextPanel(ctxBody)
	m.contextMode = true
	m.viewportFollowTail = false
	m.helpLine = "Esc 返回 · PgUp/PgDn 或 ↑/↓ 滚动"
	m.refreshContextViewportContent(false, 0)
	m.viewport.GotoTop()
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
		"prompt_tokens":            res.PromptTokens,
		"completion_tokens":        res.CompletionTokens,
		"token_reduction_rate":     res.TokenReductionRate,
		"prompt_cache_hit_tokens":  res.PromptCacheHitTokens,
		"prompt_cache_miss_tokens": res.PromptCacheMissTokens,
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
		m.transcript.AddSystemPanel("Skills", tuishared.FormatSkillsPanelBody(sk))
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
		body := append([]string{tuishared.PanelNote("已加载 " + args[1])}, tuishared.FormatSkillsPanelBody(sk)...)
		m.transcript.AddSystemPanel("Skills", body)
		m.syncViewport()
	case "unload":
		if len(args) < 2 {
			return fmt.Errorf("用法: /skill unload NAME")
		}
		sk, err := m.client.UnloadSessionSkill(m.ctx, sid, args[1])
		if err != nil {
			return err
		}
		body := append([]string{tuishared.PanelNote("已卸载 " + args[1])}, tuishared.FormatSkillsPanelBody(sk)...)
		m.transcript.AddSystemPanel("Skills", body)
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
	m.toolBlocks.Reset()
	m.toolPending.Reset()
	m.statusMgr.Reset()
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
	body := tuishared.FormatStatusPanelBody(
		m.probe.AgentID, m.probe.Version, version.Version, m.currentSession(), m.llmSettings(), ctxBody,
	)
	m.transcript.AddSystemPanel("Status", body)
	m.syncViewport()
	return nil
}

func (m *model) appendSessions() error {
	items, err := m.client.ListSessions(m.ctx)
	if err != nil {
		return err
	}
	title := fmt.Sprintf("Sessions (%d)", len(items))
	m.transcript.AddSystemPanel(title, tuishared.FormatSessionsPanelBody(items, m.currentSession()))
	m.syncViewport()
	return nil
}

func (m *model) appendTriggers() error {
	items, err := m.client.ListTriggers(m.ctx)
	if err != nil {
		return err
	}
	title := fmt.Sprintf("Triggers (%d)", len(items))
	m.transcript.AddSystemPanel(title, tuishared.FormatTriggersPanelBody(items))
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
	m.transcript.AddSystemPanel("Children", tuishared.FormatChildAgentsPanelBody(items, awaiting))
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

func (m *model) llmSettings() nodeapi.LLMSettings {
	return nodeapi.LLMSettings{
		Provider:          m.probe.LLM.Provider,
		Model:             m.probe.LLM.Model,
		Mock:              m.probe.LLM.Mock,
		ThinkingSupported: m.probe.LLM.ThinkingSupported,
		Thinking:          m.probe.LLM.Thinking,
		ReasoningEffort:   m.probe.LLM.ReasoningEffort,
	}
}

func (m *model) applyLLMSettings(settings *nodeapi.LLMSettings) {
	if settings == nil {
		return
	}
	m.probe.LLM = probe.LLMInfo{
		Provider:          settings.Provider,
		Model:             settings.Model,
		Mock:              settings.Mock,
		ThinkingSupported: settings.ThinkingSupported,
		Thinking:          settings.Thinking,
		ReasoningEffort:   settings.ReasoningEffort,
	}
}

func (m *model) handleThinkingCommand(args []string) error {
	if !m.probe.LLM.ThinkingSupported {
		return fmt.Errorf("当前 provider 不支持 thinking 控制（需 deepseek 或 qwen）")
	}
	if len(args) == 0 {
		m.statusLine = "thinking: " + tuishared.FormatLLMThinkingSummary(m.probe.LLM)
		return nil
	}
	var patch nodeapi.LLMSettingsPatch
	switch strings.ToLower(args[0]) {
	case "on", "enabled", "true", "1":
		v := "enabled"
		patch.Thinking = &v
	case "off", "disabled", "false", "0":
		v := "disabled"
		patch.Thinking = &v
	case "effort":
		if len(args) < 2 {
			return fmt.Errorf("用法: /thinking effort high|max")
		}
		v := strings.ToLower(args[1])
		if v != "high" && v != "max" {
			return fmt.Errorf("用法: /thinking effort high|max")
		}
		patch.ReasoningEffort = &v
	default:
		return fmt.Errorf("用法: /thinking on|off 或 /thinking effort high|max")
	}
	settings, err := m.client.PatchLLMSettings(m.ctx, patch)
	if err != nil {
		return err
	}
	m.applyLLMSettings(settings)
	m.statusLine = "thinking: " + tuishared.FormatLLMThinkingSummary(m.probe.LLM)
	m.notifyStripRefresh()
	return nil
}
