package turn

import (
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// buildApprovalToolItem 构造 approval_required SSE 中的单条 tool_calls 项。

// 逻辑：
// 1. 写入 id/name/arguments/raw_arguments；
// 2. 根据工具名与参数生成人类可读的 approval_reason 与 risk_level。
func buildApprovalToolItem(tc llm.ToolCall) map[string]any {
	args := parseJSONArgs(tc.Function.Arguments)
	reason, risk := describeApprovalMeta(tc.Function.Name, args)
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
	return item
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
		if len(cmd) > 160 {
			cmd = cmd[:160] + "..."
		}
		return "将执行 Shell 命令: " + cmd, risk
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
		label := firstNonEmpty(args, "id", "name")
		if label == "" {
			return "将删除定时触发器", risk
		}
		return "将删除定时触发器: " + label, risk
	case "trigger_fire":
		risk = "medium"
		label := firstNonEmpty(args, "id", "name")
		if label == "" {
			return "将立即手动触发定时任务", risk
		}
		return "将立即触发: " + label, risk
	case "background_job_cancel":
		risk = "medium"
		return "将取消后台任务", risk
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
