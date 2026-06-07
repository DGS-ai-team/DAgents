package tools

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	// RunInBackgroundKey 为各工具通用可选参数：true 时后台并行执行。
	RunInBackgroundKey = "run_in_background"
)

type sessionContextKey struct{}

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

// runInBackgroundProperty 返回写入 tool schema 的通用参数字段定义。
func runInBackgroundProperty() map[string]any {
	return map[string]any{
		"type":        "boolean",
		"description": "可选，默认 false（同步串行等待结果）。true 时后台并行执行并立即返回 job_id，完成后自动回灌。",
	}
}

// injectRunInBackgroundParam 为 tool parameters 注入 run_in_background 字段。
func injectRunInBackgroundParam(params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	props, ok := params["properties"].(map[string]any)
	if !ok || props == nil {
		props = map[string]any{}
		params["properties"] = props
	}
	props[RunInBackgroundKey] = runInBackgroundProperty()
	ensureToolSchemaRequired(params)
	return params
}

// ensureToolSchemaRequired 保证 parameters 含 OpenAI JSON Schema 的 required 数组（无必填项时为 []）。
func ensureToolSchemaRequired(params map[string]any) {
	if params == nil {
		return
	}
	if _, ok := params["required"]; !ok {
		params["required"] = []string{}
	}
}

// ParseRunInBackground 解析 arguments 中的 run_in_background，并返回剥离后的 JSON 字符串。
func ParseRunInBackground(arguments string) (background bool, cleaned string) {
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
	if len(raw) == 0 {
		return background, "{}"
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return background, arguments
	}
	return background, string(b)
}
