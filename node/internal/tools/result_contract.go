package tools

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ResultStatus is the authoritative state of a tool result.  The raw tool
// body remains backwards compatible, while the orchestrator/UI use this
// status instead of guessing from an empty body or a localized error string.
type ResultStatus string

const (
	ResultStatusSucceeded    ResultStatus = "succeeded"
	ResultStatusFailed       ResultStatus = "failed"
	ResultStatusDenied       ResultStatus = "denied"
	ResultStatusRunning      ResultStatus = "running"
	ResultStatusQueued       ResultStatus = "queued"
	ResultStatusCancelled    ResultStatus = "cancelled"
	ResultStatusTimedOut     ResultStatus = "timed_out"
	ResultStatusAwaitingUser ResultStatus = "awaiting_user"
	ResultStatusUnknown      ResultStatus = "unknown"
)

// ResultError is the stable error projection exposed with tool_result events.
type ResultError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// ResultMetadata is the common, provider-neutral result envelope metadata.
// Content is intentionally not included: each tool keeps its existing output
// body, while this metadata is attached to the event and lifecycle boundary.
type ResultMetadata struct {
	Status ResultStatus
	Error  *ResultError
}

func (m ResultMetadata) Succeeded() bool {
	return m.Status == ResultStatusSucceeded
}

func (m ResultMetadata) Denied() bool {
	return m.Status == ResultStatusDenied
}

var resultStatusPattern = regexp.MustCompile(`(?i)(^|[\s,;\[])(status|execution_status)\s*[:=]\s*([a-z_]+)`)

// ClassifyResult is the single compatibility boundary for legacy tool text.
// It understands existing JSON results, shell result headers, background
// acknowledgements and policy messages without requiring every handler to be
// rewritten at once.
func ClassifyResult(toolName, content string, rejected bool) ResultMetadata {
	trimmed := strings.TrimSpace(content)
	// A policy result may be reconstructed from persisted history without the
	// transient rejected flag. The explicit rejection marker is therefore
	// authoritative on its own; the flag remains for legacy ambiguous errors.
	if isPolicyRejection(trimmed) {
		return ResultMetadata{
			Status: ResultStatusDenied,
			Error:  &ResultError{Code: "policy_denied", Message: clipResultError(trimmed), Retryable: false},
		}
	}

	if status, errText := classifyJSONResult(trimmed); status != "" {
		return metadataForStatus(status, toolName, errText, trimmed)
	}
	if matches := resultStatusPattern.FindStringSubmatch(trimmed); len(matches) == 4 {
		if status := normalizeResultStatus(matches[3]); status != "" {
			return metadataForStatus(status, toolName, "", trimmed)
		}
	}

	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(lower, "context canceled"),
		strings.Contains(lower, "context cancelled"),
		strings.Contains(trimmed, "流式输出被用户中断"),
		strings.Contains(trimmed, "用户需要补充信息，打断了工具执行"):
		return metadataForStatus(ResultStatusCancelled, toolName, "", trimmed)
	case strings.Contains(lower, "timed out"),
		strings.Contains(lower, "timeout"),
		strings.Contains(trimmed, "超时"):
		return metadataForStatus(ResultStatusTimedOut, toolName, "", trimmed)
	case strings.HasPrefix(trimmed, "ERROR:"),
		strings.HasPrefix(strings.ToUpper(trimmed), "ERROR "):
		return metadataForStatus(ResultStatusFailed, toolName, "", trimmed)
	case rejected:
		return metadataForStatus(ResultStatusFailed, toolName, "", trimmed)
	default:
		return ResultMetadata{Status: ResultStatusSucceeded}
	}
}

// ClassifyToolResult is kept as a descriptive alias for call sites that deal
// with a model-facing tool result rather than a generic provider response.
func ClassifyToolResult(toolName, content string, rejected bool) ResultMetadata {
	return ClassifyResult(toolName, content, rejected)
}

// ResultEventFields returns stable fields that can be merged into SSE or
// audit payloads.  The legacy rejected field is only true for policy denial;
// status is authoritative for all other outcomes.
func ResultEventFields(toolName, content string, rejected bool) map[string]any {
	return ResultEventFieldsWithStatus(toolName, content, rejected, "")
}

// ResultEventFieldsWithStatus lets asynchronous bridges provide the durable
// job status when the client-facing body is intentionally a short/cleaned
// preview and no longer contains the original status marker.
func ResultEventFieldsWithStatus(toolName, content string, rejected bool, explicitStatus ResultStatus) map[string]any {
	meta := ClassifyResult(toolName, content, rejected)
	if explicitStatus != "" {
		meta = metadataForStatus(explicitStatus, toolName, "", content)
	}
	fields := map[string]any{
		"status":   string(meta.Status),
		"rejected": meta.Denied(),
	}
	if meta.Error != nil {
		fields["error"] = map[string]any{
			"code":      meta.Error.Code,
			"message":   meta.Error.Message,
			"retryable": meta.Error.Retryable,
		}
		fields["retryable"] = meta.Error.Retryable
	}
	return fields
}

// NormalizeResultStatus maps provider/job spellings to the public status set.
func NormalizeResultStatus(raw string) ResultStatus {
	return normalizeResultStatus(raw)
}

// ResultProtocolPrompt returns the common result behavior for the single
// stable system-prompt section. Tool definitions should not repeat it.
func ResultProtocolPrompt() string {
	return "返回结果的统一状态由 Node tool_result 事件以及模型可见的 [TOOL_RESULT_METADATA] 元数据给出：succeeded、failed、denied、running、queued、cancelled、timed_out、awaiting_user 或 unknown；失败时同时提供 error.code、error.message、error.retryable。优先依据 status 判断，不要仅根据正文是否为空或是否含有本地化错误词判断成功。元数据后的正文仍保留本工具约定的字段与输出。"
}

// ResultDescriptionSuffixForTool adds only the small tool-specific decoding
// hint that is otherwise easy for a model to miss in a long description. The
// common result protocol is intentionally not repeated here.
func ResultDescriptionSuffixForTool(name string) string {
	shape := ""
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "read_file":
		shape = " read_file 正文包含文件行数、当前行窗口和 next_line_offset；有下一页时继续按该 offset 读取；若 token 上限在行中间截断，next_line_offset 会指向该未完整读取的行。"
	case "write_file":
		shape = " write_file 成功正文包含写入字节数和路径；成功写入不等于后续业务验证已完成。"
	case "glob_files":
		shape = " glob_files 正文包含匹配路径、分页 offset/next_offset 或 has_more 信息；空匹配是成功的零结果。"
	case "grep_file", "grep_files":
		shape = " grep 正文包含匹配行和分页信息；无匹配是成功的零结果，不应当作工具失败。"
	case "search_replace":
		shape = " search_replace 正文明确给出成功、替换次数和必要的诊断；成功为 false 时不要声称已修改。"
	case "read_image", "show_image":
		shape = " 图片工具正文包含 path 和 status=ok/error；成功还可能伴随媒体或视觉输入副作用。"
	case "screen_capture", "computer_use":
		shape = " 桌面工具正文为 JSON，包含 status、coordinate_space、virtual_bounds 和动作后截图；computer_use 坐标始终基于最近截图的 coordinate_space。"
	case "terminal_config_list", "terminal_list":
		shape = " 列表工具成功返回 JSON 数组或对象；空列表是成功的零结果。"
	case "terminal_open":
		shape = " 成功返回 terminal_id；后续 terminal_input/read/terminate 必须使用该 ID。"
	case "terminal_input":
		shape = " 成功表示输入已写入终端，不代表命令已经完成；需要 terminal_read 读取证据。"
	case "terminal_read", "terminal_terminate":
		shape = " 正文为 JSON，重点字段包括 output、output_bytes、output_empty、next_seq 和 exited；output_empty 不等于未执行。"
	case "terminal_command":
		shape = " 正文为 JSON，优先依据 status 和 exit_code 判断命令结果；stdout/stderr 为空可能是成功的零输出，output_truncated=true 时不能声称已看到完整输出。"
	case "terminal_upload", "terminal_download":
		shape = " 传输正文包含 transfer_id、status、bytes、sha256 及本地/远端路径；校验 sha256 后再宣称传输完成。"
	case "background_job_status", "background_job_cancel":
		shape = " 正文包含 job_id、status 和输出/错误摘要；后台任务完成通常还会通过 async_tool_result 自动回灌。"
	case "load_skills", "unload_skills", "clear_skills":
		shape = " skills 结果为 JSON，包含 action、requested、loaded_skills、rejected、session_state_applied_boundary、model_context_applied_boundary、hooks_status、hooks_loaded 和 hooks_failed；以 returned loaded_skills 作为下一步会话技能状态，并按 model_context_applied_boundary 判断正文何时可见。"
	case "list_available_skills":
		shape = " list_available_skills 结果为 JSON 元数据页，包含 status、catalog_revision、query、skills、has_more 和 next_cursor；skills 只含可见 Skill 的名称、目录名和 description，不包含 SKILL.md 正文。"
	case "trigger_list", "trigger_get", "trigger_create", "trigger_update", "trigger_delete":
		shape = " 触发器正文为 JSON，包含 ok 及 trigger 或错误信息；写操作成功后再用 get/list 验证。"
	case "browser_run_task", "browser_task_status", "browser_task_cancel":
		shape = " 浏览器正文为 JSON，包含 ok、detail.status、摘要、截图/URL 和 error；detail.status 优先于 ok 判断任务终态。"
	case "linux_file_upload", "linux_file_download":
		shape = " 传输正文包含 transfer_id、status、bytes、sha256 及本地/远端路径；校验 sha256 后再宣称传输完成。"
	case "wecom_send_markdown", "wecom_send_file":
		shape = " 企微正文为 JSON，包含 ok、message 和必要的 remote_id/error；ok=true 才表示已受理。"
	case "ask_user_information":
		shape = " 该工具通常进入 awaiting_user，不应当把等待用户输入当作失败。"
	case "remember":
		shape = " 记忆写入可能进入 awaiting_user 处理冲突；只有明确成功结果才能认为已写入。"
	}
	return shape
}

func isPolicyRejection(content string) bool {
	lower := strings.ToLower(strings.TrimSpace(content))
	return strings.HasPrefix(lower, "rejected:") ||
		strings.Contains(lower, "policy_denied") ||
		strings.Contains(lower, "approval denied") ||
		strings.Contains(content, "策略拒绝") ||
		strings.Contains(content, "不允许修改") ||
		strings.Contains(content, "禁止修改")
}

func classifyJSONResult(content string) (ResultStatus, string) {
	if content == "" || (content[0] != '{' && content[0] != '[') {
		return "", ""
	}
	var value any
	if err := json.Unmarshal([]byte(content), &value); err != nil {
		return "", ""
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return "", ""
	}
	if timedOut, ok := obj["wait_timed_out"].(bool); ok && timedOut {
		return ResultStatusTimedOut, stringField(obj, "error")
	}
	if detail, ok := obj["detail"].(map[string]any); ok {
		if status := normalizeResultStatus(stringField(detail, "status")); status != "" {
			return status, stringField(obj, "error")
		}
	}
	if okValue, ok := obj["ok"].(bool); ok {
		if !okValue {
			return ResultStatusFailed, stringField(obj, "error")
		}
		if status := normalizeResultStatus(stringField(obj, "status")); status != "" {
			return status, stringField(obj, "error")
		}
		return ResultStatusSucceeded, stringField(obj, "error")
	}
	if status := normalizeResultStatus(stringField(obj, "status")); status != "" {
		return status, stringField(obj, "error")
	}
	if success, ok := obj["success"].(bool); ok {
		if success {
			return ResultStatusSucceeded, stringField(obj, "error")
		}
		return ResultStatusFailed, stringField(obj, "error")
	}
	return "", ""
}

func normalizeResultStatus(raw string) ResultStatus {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ok", "success", "succeeded", "complete", "completed", "delivered":
		return ResultStatusSucceeded
	case "error", "failed", "failure":
		return ResultStatusFailed
	case "denied", "rejected":
		return ResultStatusDenied
	case "running", "in_progress", "in-progress":
		return ResultStatusRunning
	case "queued", "accepted":
		return ResultStatusQueued
	case "pending", "awaiting_user", "awaiting_hitl", "awaiting_approval":
		return ResultStatusAwaitingUser
	case "cancelled", "canceled", "interrupted":
		return ResultStatusCancelled
	case "timed_out", "timeout", "timed-out":
		return ResultStatusTimedOut
	case "unknown", "orphaned":
		return ResultStatusUnknown
	default:
		return ""
	}
}

func metadataForStatus(status ResultStatus, toolName, message, content string) ResultMetadata {
	if status == "" {
		status = ResultStatusUnknown
	}
	if status == ResultStatusSucceeded || status == ResultStatusRunning || status == ResultStatusQueued || status == ResultStatusAwaitingUser {
		return ResultMetadata{Status: status}
	}
	code := "tool_failed"
	retryable := false
	switch status {
	case ResultStatusDenied:
		code = "policy_denied"
	case ResultStatusCancelled:
		code = "tool_cancelled"
	case ResultStatusTimedOut:
		code = "tool_timeout"
		retryable = true
	case ResultStatusUnknown:
		code = "tool_state_unknown"
		retryable = true
	default:
		if strings.HasPrefix(strings.TrimSpace(content), "{") {
			code = "remote_tool_error"
		} else if strings.TrimSpace(message) != "" {
			code = "tool_result_error"
		}
	}
	if strings.TrimSpace(message) == "" {
		message = strings.TrimSpace(content)
	}
	if strings.TrimSpace(message) == "" {
		message = string(status)
	}
	_ = toolName // reserved for future per-tool error code mapping
	return ResultMetadata{
		Status: status,
		Error:  &ResultError{Code: code, Message: clipResultError(message), Retryable: retryable},
	}
}

func stringField(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	value, _ := obj[key].(string)
	return strings.TrimSpace(value)
}

func clipResultError(message string) string {
	message = strings.TrimSpace(message)
	if len([]rune(message)) <= 512 {
		return message
	}
	runes := []rune(message)
	return string(runes[:512]) + "…"
}
