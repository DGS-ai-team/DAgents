package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	// RunInBackgroundKey 为各工具通用可选参数：true 时后台并行执行。
	RunInBackgroundKey = "run_in_background"
	// CallPurposeKey 为各工具通用必填参数：简短说明调用目的（Client 首行展示）。
	CallPurposeKey = "call_purpose"
)

type sessionContextKey struct{}

type triggerSessionTargetContextKey struct{}

// WithSession 将 session_id 写入 context，供后台任务完成回调使用。
func WithSession(ctx context.Context, sessionID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, sessionContextKey{}, strings.TrimSpace(sessionID))
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

// runInBackgroundProperty 返回写入 tool schema 的通用参数字段定义。
func runInBackgroundProperty() map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": "可选，默认 false（同步串行等待结果）。true 时后台并行执行并立即返回 job_id，完成后自动回灌。",
	}
}

// callPurposeProperty 返回 call_purpose 字段 schema。
func callPurposeProperty() map[string]any {
	return map[string]any{
		"type":        "string",
		"description": "必填。一句话说明本次调用该工具的目的（用户界面首行展示，如 bash(此内容)）。",
	}
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

// injectRunInBackgroundParam 为 tool parameters 注入 call_purpose 与 run_in_background 字段。
func injectRunInBackgroundParam(params map[string]any) map[string]any {
	params = injectCallPurposeParam(params)
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	props, ok := params["properties"].(map[string]any)
	if !ok || props == nil {
		props = map[string]any{}
		params["properties"] = props
	}
	props[RunInBackgroundKey] = runInBackgroundProperty()
	return params
}

// ParseToolCallArguments 解析 run_in_background，并剥离 call_purpose / run_in_background 后返回 handler 用 JSON。
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
