package hooks

import (
	"fmt"
	"strings"
	"time"
)

const ApprovalSubtypeDuplicateToolCall = "duplicate_tool_call"

// DuplicateMeta 为 duplicate 审批 SSE 结构化元数据（配合 approval_reason 展示）。
type DuplicateMeta struct {
	WindowSeconds            int
	PreviousToolCallID       string
	PreviousExecutedAtUnixMs int64
	SecondsSincePrevious     int
	ArgsFingerprint          string
	ResultPreview            string
}

// BuildDuplicateMeta 根据上次记录与当前指纹构造元数据。
func BuildDuplicateMeta(last ToolExecutionRecord, fingerprint string, windowSeconds int, now time.Time) DuplicateMeta {
	elapsed := now.Sub(last.ExecutedAt)
	sec := int(elapsed / time.Second)
	if sec < 0 {
		sec = 0
	}
	if windowSeconds <= 0 {
		windowSeconds = 60
	}
	return DuplicateMeta{
		WindowSeconds:            windowSeconds,
		PreviousToolCallID:       last.ToolCallID,
		PreviousExecutedAtUnixMs: last.ExecutedAt.UnixMilli(),
		SecondsSincePrevious:     sec,
		ArgsFingerprint:          fingerprint,
		ResultPreview:            last.ResultPreview,
	}
}

// FormatDuplicateApprovalReason 生成重复调用审批的人类可读原因（走标准 execute_tool 审批）。
func FormatDuplicateApprovalReason(toolName string, meta *DuplicateMeta) string {
	if meta == nil {
		return "与上次完全相同的重复工具调用"
	}
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "工具"
	}
	window := meta.WindowSeconds
	if window <= 0 {
		window = 60
	}
	msg := fmt.Sprintf("【重复调用】%s 与 %d 秒前（%d 秒窗口内）参数完全一致", name, meta.SecondsSincePrevious, window)
	if meta.PreviousToolCallID != "" {
		msg += fmt.Sprintf("，上次 call_id=%s", meta.PreviousToolCallID)
	}
	if preview := strings.TrimSpace(meta.ResultPreview); preview != "" {
		msg += fmt.Sprintf("；上次结果摘要: %s", preview)
	}
	return msg
}
