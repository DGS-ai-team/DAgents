package shared

import (
	"fmt"
	"strings"
	"time"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	"github.com/DGS-ai-team/DAgents/client/internal/probe"
)

const (
	panelKindSection   = "section"
	panelKindKV        = "kv"
	panelKindLoaded    = "loaded"
	panelKindAvailable = "avail"
	panelKindSessCur   = "sess-curr"
	panelKindSess      = "sess"
	panelKindPreview   = "preview"
	panelKindEmpty     = "empty"
	panelKindNote      = "note"
	panelKindChild     = "child"
	panelKindDetail    = "detail"
	panelKindHelp      = "help"
)

// PanelNote 构造面板内操作提示行（load/unload 等）。
func PanelNote(text string) string {
	return panelLine(panelKindNote, text)
}

func panelLine(kind string, parts ...string) string {
	return kind + "|" + strings.Join(parts, "|")
}

func panelKV(label, value string) string {
	return panelLine(panelKindKV, label, value)
}

// FormatStatusPanelBody 格式化 /status 面板正文。
func FormatStatusPanelBody(agentID, nodeVersion, sessionID string, llm nodeapi.LLMSettings, ctx *nodeapi.SessionContext) []string {
	if ctx == nil {
		ctx = &nodeapi.SessionContext{}
	}
	turn := orDash(ctx.TurnState)
	if turn == "-" && ctx.HasActiveTurn {
		turn = "active"
	} else if turn == "-" {
		turn = "idle"
	}
	if ctx.RunTurnPhase != "" && ctx.RunTurnPhase != "idle" {
		turn += " · " + ctx.RunTurnPhase
	}
	lines := []string{
		panelKV("agent", orDash(agentID)),
		panelKV("model", orDash(llm.Model)),
		panelKV("version", orDash(nodeVersion)),
		panelKV("session", orDash(sessionID)),
		panelKV("messages", fmt.Sprintf("%d", ctx.MessagesCount)),
		panelKV("queue", fmt.Sprintf("%d", ctx.QueuePending)),
		panelKV("turn", turn),
	}
	if llm.ThinkingSupported {
		lines = append(lines, panelKV("thinking", FormatLLMThinkingSummary(probe.LLMInfo{
			ThinkingSupported: llm.ThinkingSupported,
			Thinking:          llm.Thinking,
			ReasoningEffort:   llm.ReasoningEffort,
		})))
	}
	return lines
}

// FormatSessionsPanelBody 格式化 /sessions 面板正文。
func FormatSessionsPanelBody(items []nodeapi.SessionSummary, currentID string) []string {
	if len(items) == 0 {
		return []string{panelLine(panelKindEmpty, "(无 session)")}
	}
	var active, persisted []nodeapi.SessionSummary
	for _, s := range items {
		if s.Active {
			active = append(active, s)
		} else {
			persisted = append(persisted, s)
		}
	}
	var lines []string
	lines = append(lines, panelLine(panelKindSection, fmt.Sprintf("内存中 (%d)", len(active))))
	if len(active) == 0 {
		lines = append(lines, panelLine(panelKindEmpty, "(无)"))
	} else {
		for _, s := range active {
			lines = append(lines, sessionPanelLines(s, s.SessionID == currentID)...)
		}
	}
	lines = append(lines, panelLine(panelKindSection, fmt.Sprintf("已持久化 (%d)", len(persisted))))
	if len(persisted) == 0 {
		lines = append(lines, panelLine(panelKindEmpty, "(无)"))
	} else {
		for _, s := range persisted {
			lines = append(lines, sessionPanelLines(s, s.SessionID == currentID)...)
		}
	}
	return lines
}

func sessionPanelLines(s nodeapi.SessionSummary, current bool) []string {
	state := "idle"
	if s.Active {
		state = "active"
	}
	if s.HasActiveTurn {
		state += " · turn"
	}
	meta := fmt.Sprintf("msgs=%d", s.MessageCount)
	if s.Active {
		meta += fmt.Sprintf(" pending=%d phase=%s", s.QueuePending, orDash(s.RunTurnPhase))
	} else if strings.TrimSpace(s.UpdatedAt) != "" {
		meta += " updated=" + strings.TrimSpace(s.UpdatedAt)
	}
	kind := panelKindSess
	if current {
		kind = panelKindSessCur
	}
	lines := []string{panelLine(kind, s.SessionID, state, meta)}
	preview := strings.TrimSpace(s.FirstUserMessage)
	if preview != "" {
		if len(preview) > 48 {
			preview = preview[:48] + "..."
		}
		lines = append(lines, panelLine(panelKindPreview, preview))
	}
	return lines
}

// FormatSkillsPanelBody 格式化 /skill 面板正文。
func FormatSkillsPanelBody(sk *nodeapi.SessionSkills) []string {
	if sk == nil {
		return []string{panelLine(panelKindEmpty, "(无 skills 数据)")}
	}
	lines := []string{panelKV("session", orDash(sk.SessionID))}
	loadedNames := map[string]struct{}{}
	lines = append(lines, panelLine(panelKindSection, fmt.Sprintf("已加载 (%d)", len(sk.LoadedSkills))))
	if len(sk.LoadedSkills) == 0 {
		lines = append(lines, panelLine(panelKindEmpty, "(无)"))
	} else {
		for _, raw := range sk.LoadedSkills {
			name, desc := skillRowFields(raw)
			if name != "" && name != "-" {
				loadedNames[name] = struct{}{}
			}
			lines = append(lines, panelLine(panelKindLoaded, name, desc))
		}
	}
	lines = append(lines, panelLine(panelKindSection, fmt.Sprintf("可用 (%d)", len(sk.AvailableSkills))))
	if len(sk.AvailableSkills) == 0 {
		lines = append(lines, panelLine(panelKindEmpty, "(无)"))
	} else {
		for _, raw := range sk.AvailableSkills {
			name, desc := skillRowFields(raw)
			if _, ok := loadedNames[name]; ok {
				desc = strings.TrimSpace(desc + " [loaded]")
			}
			lines = append(lines, panelLine(panelKindAvailable, name, desc))
		}
	}
	return lines
}

// FormatSessionSkills 保留纯文本格式（日志/测试）；TUI 请用 FormatSkillsPanelBody。
func FormatSessionSkills(sk *nodeapi.SessionSkills) string {
	lines := FormatSkillsPanelBody(sk)
	var out []string
	out = append(out, "Skills")
	for _, line := range lines {
		out = append(out, formatPanelBodyPlain(line))
	}
	return strings.Join(out, "\n")
}

func skillRowFields(raw any) (name, desc string) {
	m, ok := raw.(map[string]any)
	if !ok {
		return fmt.Sprint(raw), ""
	}
	name = strings.TrimSpace(fmt.Sprint(m["skill_name"]))
	if name == "" {
		name = strings.TrimSpace(fmt.Sprint(m["name"]))
	}
	if name == "" {
		name = "-"
	}
	desc = skillDescriptionFromRow(m)
	return name, desc
}

// FormatChildAgentsPanelBody 格式化 /children 面板正文。
func FormatChildAgentsPanelBody(items []nodeapi.ChildAgentListItem, awaitingApproval map[string]bool) []string {
	if len(items) == 0 {
		return []string{panelLine(panelKindEmpty, "活跃临时 Agent: (无)")}
	}
	lines := []string{panelLine(panelKindSection, fmt.Sprintf("活跃临时 Agent (%d)", len(items)))}
	for i, it := range items {
		status := strings.TrimSpace(it.Status)
		if status == "" {
			status = "active"
		}
		if awaitingApproval != nil && awaitingApproval[it.ChildSessionID] {
			status += " · 待审批"
		}
		tools := strings.Join(it.AllowedTools, ", ")
		if tools == "" {
			tools = "-"
		}
		purpose := strings.TrimSpace(it.Purpose)
		if purpose == "" {
			purpose = "-"
		}
		lines = append(lines, panelLine(panelKindChild, fmt.Sprintf("%d. %s", i+1, it.ChildSessionID)))
		lines = append(lines, panelLine(panelKindDetail, fmt.Sprintf("purpose=%s tools=%s status=%s", purpose, tools, status)))
		lines = append(lines, panelLine(panelKindDetail, fmt.Sprintf("turns=%d/%d expires=%s", it.TurnCount, it.MaxTurns, orDash(it.ExpiresAt))))
	}
	return lines
}

// FormatChildAgentsList 保留纯文本格式（Python TUI 等同源）；Go TUI 请用 FormatChildAgentsPanelBody。
func FormatChildAgentsList(items []nodeapi.ChildAgentListItem, awaitingApproval map[string]bool) string {
	lines := FormatChildAgentsPanelBody(items, awaitingApproval)
	var out []string
	for _, line := range lines {
		out = append(out, formatPanelBodyPlain(line))
	}
	return strings.Join(out, "\n")
}

// FormatTriggersPanelBody 格式化 /triggers 面板正文。
func FormatTriggersPanelBody(items []nodeapi.TriggerDefinition) []string {
	if len(items) == 0 {
		return []string{panelLine(panelKindEmpty, "(无已配置触发器)")}
	}
	var lines []string
	for i, item := range items {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, triggerPanelLines(item)...)
	}
	return lines
}

func triggerPanelLines(item nodeapi.TriggerDefinition) []string {
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = "(未命名)"
	}
	state := "disabled"
	if item.Enabled {
		state = "enabled"
	}
	condition := formatTriggerCondition(item.Condition)
	nextFire := formatTriggerUnix(item.NextFireAt)
	lastFire := formatTriggerUnix(item.LastFiredAt)
	sessionHint := strings.TrimSpace(item.SessionTargetMode)
	if sessionHint == "" {
		sessionHint = "-"
	}
	if item.TargetSessionID != nil {
		if sid := strings.TrimSpace(*item.TargetSessionID); sid != "" {
			sessionHint += " · " + sid
		}
	}
	task := strings.TrimSpace(item.TaskTemplate)
	if len([]rune(task)) > 72 {
		task = string([]rune(task)[:71]) + "…"
	}
	lines := []string{
		panelLine(panelKindDetail, fmt.Sprintf("- %s [%s]", name, state)),
		panelLine(panelKindDetail, fmt.Sprintf("id: %s", orDash(item.TriggerID))),
		panelLine(panelKindDetail, fmt.Sprintf("调度: %s    下次: %s", condition, nextFire)),
		panelLine(panelKindDetail, fmt.Sprintf("触发 %d 次 · 上次 %s · 会话 %s", item.FireCount, lastFire, sessionHint)),
	}
	if task != "" {
		lines = append(lines, panelLine(panelKindDetail, "任务: "+task))
	}
	return lines
}

func formatTriggerCondition(condition map[string]any) string {
	if len(condition) == 0 {
		return "manual"
	}
	if v := triggerIntFromAny(condition["interval_seconds"]); v > 0 {
		return fmt.Sprintf("interval %ds", v)
	}
	if v := triggerFloatFromAny(condition["fire_at"]); v > 0 {
		return "once @ " + formatTriggerUnix(&v)
	}
	if sched, ok := condition["schedule"].(map[string]any); ok && len(sched) > 0 {
		kind := strings.TrimSpace(fmt.Sprint(sched["kind"]))
		if kind == "" {
			kind = "calendar"
		}
		return "schedule:" + kind
	}
	if cmd := strings.TrimSpace(fmt.Sprint(condition["cmd"])); cmd != "" {
		if len([]rune(cmd)) > 32 {
			cmd = string([]rune(cmd)[:31]) + "…"
		}
		return "cmd gate: " + cmd
	}
	return "manual"
}

func formatTriggerUnix(ts *float64) string {
	if ts == nil || *ts <= 0 {
		return "-"
	}
	t := time.Unix(int64(*ts), int64((*ts-float64(int64(*ts)))*1e9))
	return t.Local().Format("2006-01-02 15:04")
}

func triggerIntFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}

func triggerFloatFromAny(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

// FormatHelpPanelBody 格式化 /help 面板正文。
func FormatHelpPanelBody() []string {
	return []string{
		panelLine(panelKindHelp, "/status", "agent、session、队列深度"),
		panelLine(panelKindHelp, "/context", "只读 context 视图（Esc 返回）"),
		panelLine(panelKindHelp, "/policy", "工具/shell 策略管理（Esc 返回）"),
		panelLine(panelKindHelp, "/triggers", "查看已配置触发器"),
		panelLine(panelKindHelp, "/compress", "手动触发阻塞压缩"),
		panelLine(panelKindHelp, "/sessions", "列出 session（* 为当前）"),
		panelLine(panelKindHelp, "/switch <id>", "切换 session"),
		panelLine(panelKindHelp, "/new", "新建 session"),
		panelLine(panelKindHelp, "/clear", "清空对话上下文"),
		panelLine(panelKindHelp, "/children", "子 Agent 列表"),
		panelLine(panelKindHelp, "/skill", "skills 列表"),
		panelLine(panelKindHelp, "/skill load NAME", "加载 skill"),
		panelLine(panelKindHelp, "/skill unload NAME", "卸载 skill"),
		panelLine(panelKindHelp, "/upload skill PATH ID VER [NAME] [--publish]", "上传 skill 包至 Manage"),
		panelLine(panelKindHelp, "/upload externaltool PATH ID VER [NAME] [--platform] [--publish]", "上传外置工具"),
		panelLine(panelKindHelp, "/upload plugin PATH ID VER [NAME] [--platform] [--publish]", "上传 Hook plugin"),
		panelLine(panelKindHelp, "/tools verbose|brief", "tool 输出展开/折叠"),
		panelLine(panelKindHelp, "/tools expand|collapse", "展开/收起最近 tool 块"),
		panelLine(panelKindHelp, "/reasoning on|off", "推理流显示"),
		panelLine(panelKindHelp, "/thinking on|off", "模型思考开关（deepseek/qwen）"),
		panelLine(panelKindHelp, "/thinking effort high|max", "思考强度"),
		panelLine(panelKindHelp, "/version", "当前版本与更新检查"),
		panelLine(panelKindHelp, "/update", "查看可用升级（终端执行 dagents update）"),
		panelLine(panelKindHelp, "/quit", "退出"),
	}
}

// FormatVersionPanelBody 格式化 /version 面板正文。
func FormatVersionPanelBody(current, latest, platform, channel, message string, manageReachable, upgradeAvailable bool) []string {
	lines := []string{
		panelKV("当前版本", orDash(current)),
		panelKV("最新版本", orDash(latest)),
		panelKV("平台", orDash(platform)),
		panelKV("渠道", orDash(channel)),
	}
	if manageReachable {
		lines = append(lines, panelKV("Manage", "可达"))
	} else {
		lines = append(lines, panelKV("Manage", "不可达"))
	}
	if msg := strings.TrimSpace(message); msg != "" {
		lines = append(lines, panelLine(panelKindNote, msg))
	}
	if upgradeAvailable {
		lines = append(lines, panelLine(panelKindNote, "有新版本可用；请在终端运行: dagents update"))
	}
	return lines
}

// FormatUpdatePanelBody 格式化 /update 面板正文。
func FormatUpdatePanelBody(status *nodeapi.AgentUpdateStatus) []string {
	if status == nil {
		return []string{panelLine(panelKindNote, "无法获取更新信息")}
	}
	body := FormatVersionPanelBody(
		status.CurrentVersion,
		status.LatestVersion,
		status.Platform,
		status.Channel,
		status.Message,
		status.ManageReachable,
		status.UpgradeAvailable,
	)
	if notes := strings.TrimSpace(status.ReleaseNotes); notes != "" {
		body = append(body, panelLine(panelKindSection, "Release notes"))
		for _, line := range strings.Split(notes, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				body = append(body, panelLine(panelKindNote, line))
			}
		}
	}
	cmd := strings.TrimSpace(status.ApplyCommand)
	if cmd == "" {
		cmd = "dagents update"
	}
	if status.UpgradeAvailable {
		body = append(body, panelLine(panelKindNote, "安装: 在终端执行 "+cmd))
	}
	return body
}

func formatPanelBodyPlain(encoded string) string {
	kind, rest, ok := strings.Cut(encoded, "|")
	if !ok {
		return encoded
	}
	switch kind {
	case panelKindSection:
		return rest
	case panelKindKV:
		label, value, _ := strings.Cut(rest, "|")
		return fmt.Sprintf("  %-10s %s", label, value)
	case panelKindLoaded, panelKindAvailable:
		name, desc, _ := strings.Cut(rest, "|")
		if desc != "" {
			return fmt.Sprintf("  + %s · %s", name, desc)
		}
		return "  + " + name
	case panelKindSessCur:
		id, state, meta := splitPanelTriple(rest)
		return fmt.Sprintf("  * %s  [%s]  %s", id, state, meta)
	case panelKindSess:
		id, state, meta := splitPanelTriple(rest)
		return fmt.Sprintf("  - %s  [%s]  %s", id, state, meta)
	case panelKindPreview:
		return "      " + rest
	case panelKindEmpty:
		return "  " + rest
	case panelKindNote:
		return rest
	case panelKindChild:
		return "  " + rest
	case panelKindDetail:
		return "     " + rest
	case panelKindHelp:
		cmd, desc, _ := strings.Cut(rest, "|")
		return fmt.Sprintf("  %-22s %s", cmd, desc)
	default:
		return encoded
	}
}

func splitPanelTriple(rest string) (first, second, third string) {
	parts := strings.SplitN(rest, "|", 3)
	if len(parts) > 0 {
		first = parts[0]
	}
	if len(parts) > 1 {
		second = parts[1]
	}
	if len(parts) > 2 {
		third = parts[2]
	}
	return first, second, third
}
