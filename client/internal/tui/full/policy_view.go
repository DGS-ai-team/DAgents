package full

import (
	"fmt"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	tuishared "github.com/DGS-ai-team/DAgents/client/internal/tui/shared"
	tea "github.com/charmbracelet/bubbletea"
)

type policyTab int

const (
	policyTabTools policyTab = iota
	policyTabShell
)

const protectedPolicyTool = "ask_user_information"

var policyShellOrder = []string{"bash", "cmd", "powershell"}

type policyListRow struct {
	toolName string
	command  string
	mode     string
}

func policyEntryMode(mode, decision string) string {
	if m := strings.TrimSpace(mode); m != "" {
		return m
	}
	return policyDecisionToMode(decision)
}

func policyDecisionToMode(decision string) string {
	switch decision {
	case "allow_auto":
		return "never"
	case "deny":
		return "deny"
	case "require_approval":
		return "always"
	default:
		return "rule"
	}
}

func (m *model) enterPolicyView() error {
	snap, err := m.client.GetPolicy(m.ctx, "")
	if err != nil {
		return err
	}
	m.policyMode = true
	m.policySnapshot = snap
	m.policyTab = policyTabTools
	m.policyShellType = snap.Platform.DefaultShell
	if m.policyShellType == "" {
		m.policyShellType = "bash"
	}
	m.policyCursor = 0
	m.policyPendingMode = ""
	m.policyShellShowAll = false
	m.input.SetValue("")
	m.policyRenderViewport()
	m.helpLine = "Esc 返回 · Tab 切页 · 1/2/3/4 改档位 · Enter 应用 · [/] 切换 shell · + 添加(shell) · d 删除(shell)"
	m.statusLine = fmt.Sprintf("策略管理 · Node %s · 默认 shell=%s", snap.Platform.GOOS, snap.Platform.DefaultShell)
	return nil
}

func (m *model) exitPolicyView() {
	m.policyMode = false
	m.policySnapshot = nil
	m.policyPendingMode = ""
	m.input.SetValue("")
	m.helpLine = "Enter 发送 · Shift+Enter 换行 · Esc 取消 turn · /help 命令 · /quit 退出"
	m.syncViewport()
	m.statusLine = ""
}

func (m *model) handlePolicyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc":
		m.exitPolicyView()
		return m, nil
	case "tab":
		if m.policyTab == policyTabTools {
			m.policyTab = policyTabShell
		} else {
			m.policyTab = policyTabTools
		}
		m.policyCursor = 0
		m.policyPendingMode = ""
		m.policyRenderViewport()
		return m, nil
	case "[":
		m.policyCycleShell(-1)
		return m, nil
	case "]":
		m.policyCycleShell(1)
		return m, nil
	case "+":
		if m.policyTab == policyTabShell {
			if err := m.policyAddShellFromFilter(); err != nil {
				m.errLine = err.Error()
			} else {
				m.errLine = ""
			}
		}
		return m, nil
	case "d":
		if m.policyTab == policyTabShell {
			if err := m.policyDeleteShellCurrent(); err != nil {
				m.errLine = err.Error()
			} else {
				m.errLine = ""
			}
		}
		return m, nil
	case "1":
		m.policyPendingMode = "never"
		m.policyRenderViewport()
		return m, nil
	case "2":
		m.policyPendingMode = "always"
		m.policyRenderViewport()
		return m, nil
	case "3":
		m.policyPendingMode = "rule"
		m.policyRenderViewport()
		return m, nil
	case "4":
		m.policyPendingMode = "deny"
		m.policyRenderViewport()
		return m, nil
	case "enter":
		if err := m.policyApplyCurrent(); err != nil {
			m.errLine = err.Error()
		} else {
			m.errLine = ""
		}
		return m, nil
	case "up":
		if m.policyCursor > 0 {
			m.policyCursor--
			m.policyPendingMode = ""
			m.policyRenderViewport()
		}
		return m, nil
	case "down":
		rows := m.policyVisibleRows()
		if m.policyCursor < len(rows)-1 {
			m.policyCursor++
			m.policyPendingMode = ""
			m.policyRenderViewport()
		}
		return m, nil
	default:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.policyClampCursor()
		m.policyRenderViewport()
		return m, cmd
	}
}

func (m *model) policyCycleShell(delta int) {
	if m.policyTab != policyTabShell {
		return
	}
	idx := 0
	for i, s := range policyShellOrder {
		if s == m.policyShellType {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(policyShellOrder)) % len(policyShellOrder)
	m.policyShellType = policyShellOrder[idx]
	m.policyCursor = 0
	m.policyPendingMode = ""
	m.policyRenderViewport()
}

func (m *model) policyVisibleRows() []policyListRow {
	if m.policySnapshot == nil {
		return nil
	}
	filter := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if m.policyTab == policyTabTools {
		var out []policyListRow
		for _, item := range m.policySnapshot.Tools {
			if filter != "" && !strings.Contains(strings.ToLower(item.Name), filter) {
				continue
			}
			out = append(out, policyListRow{
				toolName: item.Name,
				mode:     policyEntryMode(item.Mode, item.Decision),
			})
		}
		return out
	}
	items := m.policySnapshot.Shell[m.policyShellType]
	var out []policyListRow
	for _, item := range items {
		name := item.Command
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		out = append(out, policyListRow{
			command: name,
			mode:    policyEntryMode(item.Mode, item.Decision),
		})
	}
	return out
}

func (m *model) policyClampCursor() {
	rows := m.policyVisibleRows()
	if len(rows) == 0 {
		m.policyCursor = 0
		return
	}
	if m.policyCursor >= len(rows) {
		m.policyCursor = len(rows) - 1
	}
}

func (m *model) policyRenderViewport() {
	rows := m.policyVisibleRows()
	var b strings.Builder
	tabTools := "[工具]"
	tabShell := "[Shell]"
	if m.policyTab == policyTabTools {
		tabTools = ">工具<"
	} else {
		tabShell = ">Shell<"
	}
	b.WriteString(fmt.Sprintf("Tab %s %s", tabTools, tabShell))
	if m.policyTab == policyTabShell {
		b.WriteString(fmt.Sprintf(" · shell=%s", m.policyShellType))
		b.WriteString(" · 未列出默认需审批")
	}
	filter := strings.TrimSpace(m.input.Value())
	if filter != "" {
		fmt.Fprintf(&b, " · 过滤: %s", filter)
	}
	b.WriteString("\n\n")
	if len(rows) == 0 {
		b.WriteString("（无匹配项）\n")
	} else {
		for i, row := range rows {
			label := row.toolName
			if label == "" {
				label = row.command
			}
			mode := row.mode
			if i == m.policyCursor && m.policyPendingMode != "" {
				mode = m.policyPendingMode
			}
			prefix := "  "
			if i == m.policyCursor {
				prefix = "> "
			}
			b.WriteString(fmt.Sprintf("%s%-22s %s\n", prefix, label, policyModeStyled(mode)))
		}
	}
	m.policyText = b.String()
	m.viewport.SetContent(m.policyText)
}

func policyModeLabel(mode string) string {
	switch mode {
	case "never":
		return "自动允许"
	case "always":
		return "需审批"
	case "rule":
		return "特殊规则"
	case "deny":
		return "禁止"
	default:
		return mode
	}
}

func policyModeStyled(mode string) string {
	label := policyModeLabel(mode)
	switch mode {
	case "never":
		return "\033[" + tuishared.ThemePolicyAllow + "m" + label + "\033[0m"
	case "always":
		return "\033[" + tuishared.ThemePolicyApproval + "m" + label + "\033[0m"
	case "rule":
		return "\033[" + tuishared.ThemePolicyRule + "m" + label + "\033[0m"
	case "deny":
		return "\033[" + tuishared.ThemePolicyDeny + "m" + label + "\033[0m"
	default:
		return label
	}
}

func (m *model) policyApplyCurrent() error {
	rows := m.policyVisibleRows()
	if len(rows) == 0 {
		return fmt.Errorf("无选中项")
	}
	row := rows[m.policyCursor]
	mode := m.policyPendingMode
	if mode == "" {
		mode = row.mode
	}
	if row.toolName == protectedPolicyTool && mode == "deny" {
		return fmt.Errorf("%s 不能设为禁止", protectedPolicyTool)
	}
	if m.policyTab == policyTabTools {
		if err := m.client.UpdateToolPolicy(m.ctx, []nodeapi.PolicyToolUpdate{
			{Name: row.toolName, Mode: mode},
		}); err != nil {
			return err
		}
		m.policyUpdateToolLocal(row.toolName, mode)
	} else {
		if err := m.client.UpdateShellPolicy(m.ctx, m.policyShellType, []nodeapi.PolicyShellUpdate{
			{Command: row.command, Mode: mode},
		}); err != nil {
			return err
		}
		m.policyUpdateShellLocal(row.command, mode)
	}
	m.policyPendingMode = ""
	m.statusLine = fmt.Sprintf("已更新 %s → %s", firstNonEmpty(row.toolName, row.command), policyModeLabel(mode))
	m.policyRenderViewport()
	return nil
}

func (m *model) policyUpdateToolLocal(name, mode string) {
	if m.policySnapshot == nil {
		return
	}
	for i, item := range m.policySnapshot.Tools {
		if item.Name == name {
			m.policySnapshot.Tools[i].Mode = mode
			m.policySnapshot.Tools[i].Decision = policyModeToLegacyDecision(mode)
			m.policySnapshot.Tools[i].Configured = true
			return
		}
	}
	m.policySnapshot.Tools = append(m.policySnapshot.Tools, nodeapi.PolicyToolEntry{
		Name: name, Mode: mode, Decision: policyModeToLegacyDecision(mode), Configured: true,
	})
}

func (m *model) policyUpdateShellLocal(command, mode string) {
	if m.policySnapshot == nil {
		return
	}
	cmd := normalizeShellCommand(command)
	items := m.policySnapshot.Shell[m.policyShellType]
	for i, item := range items {
		if normalizeShellCommand(item.Command) == cmd {
			items[i].Mode = mode
			items[i].Decision = policyModeToLegacyDecision(mode)
			items[i].Configured = true
			items[i].Command = cmd
			m.policySnapshot.Shell[m.policyShellType] = items
			return
		}
	}
	items = append(items, nodeapi.PolicyShellEntry{
		Command: cmd, Mode: mode, Decision: policyModeToLegacyDecision(mode), Configured: true,
	})
	m.policySnapshot.Shell[m.policyShellType] = items
}

func policyModeToLegacyDecision(mode string) string {
	switch mode {
	case "never":
		return "allow_auto"
	case "deny":
		return "deny"
	case "always":
		return "require_approval"
	default:
		return "require_approval"
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func normalizeShellCommand(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}

func (m *model) policyAddShellFromFilter() error {
	cmd := normalizeShellCommand(m.input.Value())
	if cmd == "" {
		return fmt.Errorf("请输入要添加的命令名")
	}
	mode := m.policyPendingMode
	if mode == "" {
		mode = "never"
	}
	if err := m.client.UpdateShellPolicy(m.ctx, m.policyShellType, []nodeapi.PolicyShellUpdate{
		{Command: cmd, Mode: mode},
	}); err != nil {
		return err
	}
	m.policyUpdateShellLocal(cmd, mode)
	m.policyPendingMode = ""
	m.input.SetValue("")
	m.statusLine = fmt.Sprintf("已添加 %s → %s", cmd, policyModeLabel(mode))
	m.policyRenderViewport()
	return nil
}

func (m *model) policyDeleteShellCurrent() error {
	rows := m.policyVisibleRows()
	if len(rows) == 0 {
		return fmt.Errorf("无选中项")
	}
	row := rows[m.policyCursor]
	if strings.TrimSpace(row.command) == "" {
		return fmt.Errorf("无选中项")
	}
	if err := m.client.UpdateShellPolicy(m.ctx, m.policyShellType, []nodeapi.PolicyShellUpdate{}, row.command); err != nil {
		return err
	}
	m.policyRemoveShellLocal(row.command)
	m.policyPendingMode = ""
	m.policyClampCursor()
	m.statusLine = fmt.Sprintf("已删除 %s（未列出默认需审批）", row.command)
	m.policyRenderViewport()
	return nil
}

func (m *model) policyRemoveShellLocal(command string) {
	if m.policySnapshot == nil {
		return
	}
	cmd := normalizeShellCommand(command)
	items := m.policySnapshot.Shell[m.policyShellType]
	next := make([]nodeapi.PolicyShellEntry, 0, len(items))
	for _, item := range items {
		if normalizeShellCommand(item.Command) == cmd {
			continue
		}
		next = append(next, item)
	}
	m.policySnapshot.Shell[m.policyShellType] = next
}
