package shared

import (
	"fmt"
	"strings"
)

const (
	toolA2APendingLinePrefix = "[tool-a2a-pending|"
	toolA2AResultLinePrefix  = "[tool-a2a-result|"
)

// FormatA2ARelayApprovalPending 生成 A2A 中继审批中的工具占位行（青点样式，无动态耗时）。
func FormatA2ARelayApprovalPending(blockID, title, peerSuffix, rawArgs string) []string {
	blockID = strings.TrimSpace(blockID)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "tool"
	}
	body := "▶ " + title + peerSuffix + " · 待审批"
	lines := []string{formatToolMetaLine(toolA2APendingLinePrefix, blockID, body)}
	if code := strings.TrimSpace(rawArgs); code != "" && code != "{}" {
		lines = append(lines, splitToolCallCodeLines(blockID, code)...)
	}
	return lines
}

// FormatA2ARelayToolResult 生成 A2A 中继审批提交后的工具终态行（对端执行，无 tool_result SSE）。
func FormatA2ARelayToolResult(blockID, title, peerSuffix string, approved bool) []string {
	blockID = strings.TrimSpace(blockID)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "tool"
	}
	head := title + peerSuffix
	if !approved {
		head += " · 已拒绝"
	}
	summary := "已审批，由对端执行"
	if !approved {
		summary = "已拒绝"
	}
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

// FormatA2ARelayPeerSuffix 供测试与 hitl 包对齐的纯文本后缀。
func FormatA2ARelayPeerSuffix(peerLabel string) string {
	peerLabel = strings.TrimSpace(peerLabel)
	if peerLabel != "" {
		return fmt.Sprintf(" from %s", peerLabel)
	}
	return " from 对端 Agent"
}
