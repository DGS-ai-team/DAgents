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
	decision string
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
	m.policyPendingDecision = ""
	m.policyShellShowAll = false
	m.input.SetValue("")
	m.policyRenderViewport()
	m.helpLine = "Esc 返回 · Tab 切页 · 1/2/3 改档位 · Enter 应用 · [/] 切换 shell · a 显示全部(shell)"
	m.statusLine = fmt.Sprintf("策略管理 · Node %s · 默认 shell=%s", snap.Platform.GOOS, snap.Platform.DefaultShell)
	return nil
}

func (m *model) exitPolicyView() {
	m.policyMode = false
	m.policySnapshot = nil
	m.policyPendingDecision = ""
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
		m.policyPendingDecision = ""
		m.policyRenderViewport()
		return m, nil
	case "[":
		m.policyCycleShell(-1)
		return m, nil
	case "]":
		m.policyCycleShell(1)
		return m, nil
	case "a":
		if m.policyTab == policyTabShell {
			m.policyShellShowAll = !m.policyShellShowAll
			m.policyClampCursor()
			m.policyRenderViewport()
		}
		return m, nil
	case "1":
		m.policyPendingDecision = "allow_auto"
		m.policyRenderViewport()
		return m, nil
	case "2":
		m.policyPendingDecision = "require_approval"
		m.policyRenderViewport()
		return m, nil
	case "3":
		m.policyPendingDecision = "deny"
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
			m.policyPendingDecision = ""
			m.policyRenderViewport()
		}
		return m, nil
	case "down":
		rows := m.policyVisibleRows()
		if m.policyCursor < len(rows)-1 {
			m.policyCursor++
			m.policyPendingDecision = ""
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
	m.policyPendingDecision = ""
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
			out = append(out, policyListRow{toolName: item.Name, decision: item.Decision})
		}
		return out
	}
	items := m.policySnapshot.Shell[m.policyShellType]
	var out []policyListRow
	for _, item := range items {
		if filter == "" && !m.policyShellShowAll {
			if item.Decision != "allow_auto" && item.Decision != "deny" {
				continue
			}
		}
		name := item.Command
		if filter != "" && !strings.Contains(strings.ToLower(name), filter) {
			continue
		}
		out = append(out, policyListRow{command: name, decision: item.Decision})
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
		if !m.policyShellShowAll {
			b.WriteString(" · 仅白+黑")
		}
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
			decision := row.decision
			if i == m.policyCursor && m.policyPendingDecision != "" {
				decision = m.policyPendingDecision
			}
			prefix := "  "
			if i == m.policyCursor {
				prefix = "> "
			}
			b.WriteString(fmt.Sprintf("%s%-22s %s\n", prefix, label, policyDecisionStyled(decision)))
		}
	}
	m.policyText = b.String()
	m.viewport.SetContent(m.policyText)
}

func policyDecisionLabel(decision string) string {
	switch decision {
	case "allow_auto":
		return "白名单"
	case "deny":
		return "黑名单"
	default:
		return "需审批"
	}
}

func policyDecisionStyled(decision string) string {
	label := policyDecisionLabel(decision)
	switch decision {
	case "allow_auto":
		return "\033[" + tuishared.ThemePolicyAllow + "m" + label + "\033[0m"
	case "deny":
		return "\033[" + tuishared.ThemePolicyDeny + "m" + label + "\033[0m"
	default:
		return "\033[" + tuishared.ThemePolicyApproval + "m" + label + "\033[0m"
	}
}

func (m *model) policyApplyCurrent() error {
	rows := m.policyVisibleRows()
	if len(rows) == 0 {
		return fmt.Errorf("无选中项")
	}
	row := rows[m.policyCursor]
	decision := m.policyPendingDecision
	if decision == "" {
		decision = row.decision
	}
	if row.toolName == protectedPolicyTool && decision == "deny" {
		return fmt.Errorf("%s 不能设为黑名单", protectedPolicyTool)
	}
	if m.policyTab == policyTabTools {
		if err := m.client.UpdateToolPolicy(m.ctx, []nodeapi.PolicyToolUpdate{
			{Name: row.toolName, Decision: decision},
		}); err != nil {
			return err
		}
		m.policyUpdateToolLocal(row.toolName, decision)
	} else {
		if err := m.client.UpdateShellPolicy(m.ctx, m.policyShellType, []nodeapi.PolicyShellUpdate{
			{Command: row.command, Decision: decision},
		}); err != nil {
			return err
		}
		m.policyUpdateShellLocal(row.command, decision)
	}
	m.policyPendingDecision = ""
	m.statusLine = fmt.Sprintf("已更新 %s → %s", firstNonEmpty(row.toolName, row.command), policyDecisionLabel(decision))
	m.policyRenderViewport()
	return nil
}

func (m *model) policyUpdateToolLocal(name, decision string) {
	if m.policySnapshot == nil {
		return
	}
	for i, item := range m.policySnapshot.Tools {
		if item.Name == name {
			m.policySnapshot.Tools[i].Decision = decision
			m.policySnapshot.Tools[i].Configured = true
			return
		}
	}
	m.policySnapshot.Tools = append(m.policySnapshot.Tools, nodeapi.PolicyToolEntry{
		Name: name, Decision: decision, Configured: true,
	})
}

func (m *model) policyUpdateShellLocal(command, decision string) {
	if m.policySnapshot == nil {
		return
	}
	items := m.policySnapshot.Shell[m.policyShellType]
	for i, item := range items {
		if item.Command == command {
			items[i].Decision = decision
			items[i].Configured = true
			m.policySnapshot.Shell[m.policyShellType] = items
			return
		}
	}
	items = append(items, nodeapi.PolicyShellEntry{Command: command, Decision: decision, Configured: true})
	m.policySnapshot.Shell[m.policyShellType] = items
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
