package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// RunInBackgroundKey 为历史/内部参数名；已不在 tool schema 暴露，ParseToolCallArguments 仍兼容剥离。
	RunInBackgroundKey = "run_in_background"
	// CallPurposeKey 为各工具通用必填参数：简短说明调用目的（Client 首行展示）。
	CallPurposeKey = "call_purpose"
)

type sessionContextKey struct{}

type triggerSessionTargetContextKey struct{}

type enabledBypassContextKey struct{}

type approvalIDContextKey struct{}

type backgroundJobIDContextKey struct{}

// WithEnabledBypass 跳过 Registry.enabledOnly 检查（子 Agent 在自身 allowlist 校验后使用）。
func WithEnabledBypass(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, enabledBypassContextKey{}, true)
}

// EnabledBypassFromContext 是否跳过 enabledOnly 门禁。
func EnabledBypassFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(enabledBypassContextKey{}).(bool)
	return v
}

// WithApprovalID marks the tool call as having passed the current HITL
// decision. Providers may use the opaque ID only as a presence signal; it is
// never sent to the remote host.
func WithApprovalID(ctx context.Context, approvalID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, approvalIDContextKey{}, strings.TrimSpace(approvalID))
}

func ApprovalIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(approvalIDContextKey{}).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// WithBackgroundJobID correlates a background tool execution with its job
// record so providers can bind the concrete Process after it starts.
func WithBackgroundJobID(ctx context.Context, jobID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, backgroundJobIDContextKey{}, strings.TrimSpace(jobID))
}

func BackgroundJobIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(backgroundJobIDContextKey{}).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// WithSession 将 session_id 写入 context，供后台任务完成回调使用。
func WithSession(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionContextKey{}, strings.TrimSpace(sessionID))
}

// SessionIDFromContext 读取 context 中的 session_id。
func SessionIDFromContext(ctx context.Context) string {
	return sessionIDFromContext(ctx)
}

func sessionIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(sessionContextKey{}).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// WithTriggerSessionTarget 写入审批通过的 trigger 投递目标（same_session / new_session / latest_active_session）。
func WithTriggerSessionTarget(ctx context.Context, target string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, triggerSessionTargetContextKey{}, strings.TrimSpace(target))
}

// TriggerSessionTargetFromContext 读取 trigger 审批投递目标。
func TriggerSessionTargetFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(triggerSessionTargetContextKey{}).(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// injectCallPurposeParam 为 tool parameters 注入 call_purpose 并加入 required。
func injectCallPurposeParam(params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	props, ok := params["properties"].(map[string]any)
	if !ok || props == nil {
		props = map[string]any{}
		params["properties"] = props
	}
	props[CallPurposeKey] = callPurposeProperty()
	ensureCallPurposeRequired(params)
	return params
}

// callPurposeProperty 返回 call_purpose 字段 schema。
func callPurposeProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "必填。一句话说明本次调用该工具的目的（用户界面首行展示，如 bash(此内容)）。",
	}
}

func ensureCallPurposeRequired(params map[string]any) {
	req := requiredStrings(params)
	if !containsString(req, CallPurposeKey) {
		req = append([]string{CallPurposeKey}, req...)
	}
	params["required"] = req
}

func requiredStrings(params map[string]any) []string {
	if params == nil {
		return nil
	}
	raw, ok := params["required"]
	if !ok || raw == nil {
		return []string{}
	}
	switch v := raw.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return []string{}
	}
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// ParseToolCallArguments 剥离 call_purpose（及历史 run_in_background）后返回 handler 用 JSON。
func ParseToolCallArguments(arguments string) (background bool, cleaned string) {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return false, "{}"
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(arguments), &raw); err != nil {
		return false, arguments
	}
	if v, ok := raw[RunInBackgroundKey]; ok {
		_ = json.Unmarshal(v, &background)
		delete(raw, RunInBackgroundKey)
	}
	delete(raw, CallPurposeKey)
	if len(raw) == 0 {
		return background, "{}"
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return background, arguments
	}
	return background, string(b)
}

// ParseRunInBackground 兼容别名；同 ParseToolCallArguments。
func ParseRunInBackground(arguments string) (background bool, cleaned string) {
	return ParseToolCallArguments(arguments)
}
