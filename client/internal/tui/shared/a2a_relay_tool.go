package shared

import (
	"strings"
)

const (
	toolA2APendingLinePrefix = "[tool-a2a-pending|"
	toolA2AResultLinePrefix  = "[tool-a2a-result|"
)

// FormatA2ARelayApprovalPending 生成 A2A 中继审批中的工具占位行（青点样式，无动态耗时）。
func FormatA2ARelayApprovalPending(blockID, title, peerLabel, rawArgs string) []string {
	blockID = strings.TrimSpace(blockID)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "tool"
	}
	peerSuffix := FormatA2ARelayPeerSuffix(peerLabel)
	body := "▶ " + title + peerSuffix + " · 待审批"
	lines := []string{formatToolMetaLine(toolA2APendingLinePrefix, blockID, body)}
	if code := strings.TrimSpace(rawArgs); code != "" && code != "{}" {
		lines = append(lines, splitToolCallCodeLines(blockID, code)...)
	}
	return lines
}

// FormatA2ARelayToolResult 生成 A2A 中继审批提交后的工具终态行（对端执行，无 tool_result SSE）。
func FormatA2ARelayToolResult(blockID, title, peerLabel string, approved bool) []string {
	blockID = strings.TrimSpace(blockID)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "tool"
	}
	peerSuffix := FormatA2ARelayPeerSuffix(peerLabel)
	head := title + peerSuffix
	if !approved {
		head += " · 已拒绝"
	}
	summary := FormatA2ARelayApprovedSummary(peerLabel, approved)
	lines := []string{
		formatToolMetaLine(toolA2AResultLinePrefix, blockID, head),
		formatToolMetaLine(toolPreviewLinePrefix, blockID, summary),
	}
	return lines
}

// RemoveA2ARelayToolLines 移除指定块的 A2A pending/result 行。
func (t *Transcript) RemoveA2ARelayToolLines(blockID string) {
	blockID = strings.TrimSpace(blockID)
	if blockID == "" || t == nil {
		return
	}
	prefixes := []string{
		toolA2APendingLinePrefix + blockID + "]",
		toolA2AResultLinePrefix + blockID + "]",
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var kept []string
	for _, line := range t.lines {
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(line, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		kept = append(kept, line)
	}
	t.lines = kept
}

// ReplaceA2ARelayToolLines 将 A2A pending 块替换为终态行。
func (t *Transcript) ReplaceA2ARelayToolLines(blockID string, lines []string) {
	blockID = strings.TrimSpace(blockID)
	if blockID == "" {
		for _, line := range lines {
			t.Add(line)
		}
		return
	}
	t.RemoveToolPendingLines(blockID)
	t.RemoveA2ARelayToolLines(blockID)
	for _, line := range lines {
		t.Add(line)
	}
}

// IsToolA2APendingLine 是否为 A2A 中继审批 pending 行。
func IsToolA2APendingLine(line string) bool {
	return strings.HasPrefix(line, toolA2APendingLinePrefix)
}

// IsToolA2AResultLine 是否为 A2A 中继工具终态行。
func IsToolA2AResultLine(line string) bool {
	return strings.HasPrefix(line, toolA2AResultLinePrefix)
}

// ToolA2ALineBody 解析 A2A 工具元数据行正文。
func ToolA2ALineBody(line, prefix string) string {
	rest := strings.TrimPrefix(line, prefix)
	if i := strings.Index(rest, "] "); i >= 0 {
		return rest[i+2:]
	}
	return rest
}

// FormatA2ARelayPeerSuffix 根据对端展示名生成 from 后缀（纯文本）。
func FormatA2ARelayPeerSuffix(peerLabel string) string {
	label := strings.TrimSpace(peerLabel)
	if label != "" {
		return " from " + label
	}
	return " from 对端 Agent"
}

// FormatA2ARelayApprovedSummary 返回 A2A 中继工具审批提交后的终态摘要。
func FormatA2ARelayApprovedSummary(peerLabel string, approved bool) string {
	if !approved {
		return "已拒绝"
	}
	label := strings.TrimSpace(peerLabel)
	if label == "" {
		label = "对端 Agent"
	}
	return "已审批，由" + label + "执行"
}
