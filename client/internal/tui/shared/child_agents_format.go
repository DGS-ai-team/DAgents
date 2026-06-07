package shared

import (
	"fmt"
	"strings"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
)

// FormatChildAgentsList 格式化活跃临时 Agent 列表（供 /children）。
func FormatChildAgentsList(items []nodeapi.ChildAgentListItem, awaitingApproval map[string]bool) string {
	if len(items) == 0 {
		return "活跃临时 Agent: (无)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "活跃临时 Agent (%d):\n", len(items))
	for i, it := range items {
		purpose := strings.TrimSpace(it.Purpose)
		if purpose == "" {
			purpose = "-"
		}
		tools := strings.Join(it.AllowedTools, ",")
		if tools == "" {
			tools = "-"
		}
		status := strings.TrimSpace(it.Status)
		if status == "" {
			status = "active"
		}
		if awaitingApproval != nil && awaitingApproval[it.ChildSessionID] {
			status += " · 待审批"
		}
		fmt.Fprintf(&b, "  %d. %s\n", i+1, it.ChildSessionID)
		fmt.Fprintf(&b, "     purpose=%s tools=%s status=%s\n", purpose, tools, status)
		fmt.Fprintf(&b, "     turns=%d/%d expires=%s\n", it.TurnCount, it.MaxTurns, orDash(it.ExpiresAt))
	}
	return strings.TrimRight(b.String(), "\n")
}
