package hitl

import (
	"encoding/json"
	"fmt"
	"strings"
)

const defaultApprovalArgsMaxLen = 700

// formatApprovalArgs 将工具参数格式化为多行 JSON（超长截断）。
func formatApprovalArgs(rawArgs string, argMap map[string]any, maxLen int) string {
	if maxLen <= 0 {
		maxLen = defaultApprovalArgsMaxLen
	}
	var payload any
	if strings.TrimSpace(rawArgs) != "" && rawArgs != "{}" {
		if err := json.Unmarshal([]byte(rawArgs), &payload); err != nil {
			return truncateApprovalText(rawArgs, maxLen)
		}
	} else if len(argMap) > 0 {
		payload = argMap
	} else {
		return ""
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return truncateApprovalText(fmt.Sprint(payload), maxLen)
	}
	return truncateApprovalText(string(b), maxLen)
}

func truncateApprovalText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" || len(text) <= maxLen {
		return text
	}
	return text[:maxLen] + "..."
}

func mapStringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(raw))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}

func writeApprovalItemDetails(b *strings.Builder, it ToolApprovalItem, indent string) {
	if it.Risk != "" {
		fmt.Fprintf(b, "%s风险: %s\n", indent, it.Risk)
	}
	if it.Reason != "" {
		fmt.Fprintf(b, "%s原因: %s\n", indent, it.Reason)
	}
	args := formatApprovalArgs(it.RawArgs, nil, defaultApprovalArgsMaxLen)
	if args == "" && it.Arguments != nil {
		args = formatApprovalArgs("", it.Arguments, defaultApprovalArgsMaxLen)
	}
	if args != "" {
		fmt.Fprintf(b, "%s参数:\n", indent)
		for _, line := range strings.Split(args, "\n") {
			fmt.Fprintf(b, "%s  %s\n", indent, line)
		}
	}
}
