package turn

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

type pendingApprovalCall struct {
	tc            llm.ToolCall
	duplicateMeta *hooks.DuplicateMeta
}

// BuildApprovalToolItem 构造 HITL execute_tool item 的展示字段（用于 hitl_required / hydrate transcript）。
func BuildApprovalToolItem(tc llm.ToolCall, duplicateMeta *hooks.DuplicateMeta) map[string]any {
	return buildApprovalToolItem(tc, duplicateMeta)
}

// buildApprovalToolItem 构造 HITL execute_tool item 的展示字段（用于 hitl_required）。
func buildApprovalToolItem(tc llm.ToolCall, duplicateMeta *hooks.DuplicateMeta) map[string]any {
	args := parseJSONArgs(tc.Function.Arguments)
	var reason, risk string
	if duplicateMeta != nil {
		reason = hooks.FormatDuplicateApprovalReason(tc.Function.Name, duplicateMeta)
		risk = "low"
	} else {
		reason, risk = describeApprovalMeta(tc.Function.Name, args)
	}
	item := map[string]any{
		"id":            tc.ID,
		"name":          tc.Function.Name,
		"arguments":     args,
		"raw_arguments": tc.Function.Arguments,
	}
	if reason != "" {
		item["approval_reason"] = reason
	}
	if risk != "" {
		item["risk_level"] = risk
	}
	if duplicateMeta != nil {
		item["duplicate_meta"] = map[string]any{
			"window_seconds":               duplicateMeta.WindowSeconds,
			"previous_tool_call_id":        duplicateMeta.PreviousToolCallID,
			"previous_executed_at_unix_ms": duplicateMeta.PreviousExecutedAtUnixMs,
			"seconds_since_previous":       duplicateMeta.SecondsSincePrevious,
			"args_fingerprint":             duplicateMeta.ArgsFingerprint,
			"result_preview":               duplicateMeta.ResultPreview,
		}
	}
	if isTriggerSessionApprovalTool(tc.Function.Name) {
		item["approval_mode"] = "trigger_session"
	}
	return item
}

func isTriggerSessionApprovalTool(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "trigger_create":
		return true
	default:
		return false
	}
}

func describeApprovalMeta(toolName string, args map[string]any) (reason, risk string) {
	name := strings.ToLower(strings.TrimSpace(toolName))
	switch name {
	case "bash_run":
		risk = "high"
		cmd := strings.TrimSpace(fmt.Sprint(args["command"]))
		if cmd == "" {
			return "将执行 Shell 命令（参数未提供 command）", risk
		}
		return "将执行 Shell 命令: " + truncateRunes(cmd, 160), risk
	case "terminal_command":
		risk = "high"
		terminal := firstNonEmpty(args, "terminal_id")
		cmd := firstNonEmpty(args, "command")
		if terminal == "" {
			return "在已打开终端上执行命令（terminal_id 未提供）", risk
		}
		return "在终端 " + terminal + " 上执行命令: " + truncateRunes(cmd, 160), risk
	case "terminal_upload", "terminal_download":
		risk = "high"
		terminal := firstNonEmpty(args, "terminal_id")
		local := firstNonEmpty(args, "local_path")
		remote := firstNonEmpty(args, "remote_path")
		direction := "上传"
		if name == "terminal_download" {
			direction = "下载"
		}
		return direction + "终端文件: " + terminal + " · " + truncateRunes(local+" → "+remote, 200), risk
	case "write_file", "search_replace":
		risk = "medium"
		path := firstNonEmpty(args, "path", "file_path")
		if path == "" {
			return "将修改本地文件", risk
		}
		return "将修改本地文件: " + path, risk
	case "trigger_create":
		risk = "medium"
		label := firstNonEmpty(args, "name", "id")
		if label == "" {
			return "将创建定时触发器，可能在后台自动运行", risk
		}
		return "将创建定时触发器: " + label, risk
	case "trigger_update":
		risk = "medium"
		label := firstNonEmpty(args, "id", "name")
		if label == "" {
			return "将更新定时触发器配置", risk
		}
		return "将更新定时触发器: " + label, risk
	case "trigger_delete":
		risk = "high"
		label := firstNonEmpty(args, "id", "name", "trigger_id")
		if label == "" {
			return "将删除定时触发器", risk
		}
		return "将删除定时触发器: " + label, risk
	default:
		risk = "medium"
		return "该工具被策略标记为需用户确认后执行", risk
	}
}

func firstNonEmpty(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if args == nil {
			return ""
		}
		s := strings.TrimSpace(fmt.Sprint(args[key]))
		if s != "" && s != "<nil>" {
			return s
		}
	}
	return ""
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
