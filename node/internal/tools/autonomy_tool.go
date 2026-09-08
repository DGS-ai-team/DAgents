package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AutonomyView is deliberately transport-neutral: the API layer may return
// the current Goal plus scheduler usage without making tools import HTTP.
type AutonomyView = any

type AutonomyUpdate struct {
	Objective  *string
	Acceptance *string
	NextWakeAt *time.Time
	Pause      bool
}

type AutonomyGetFunc func(context.Context, string) (AutonomyView, error)
type AutonomyUpdateFunc func(context.Context, string, AutonomyUpdate) (AutonomyView, error)

func autonomyGetToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "autonomy_get",
		Description: "查看当前 Auto Agent 自有长期任务、进度、调度限制和使用量。任务上下文由当前 Agent 绑定，不接受 Agent ID。",
		Parameters:  injectCallPurposeParam(map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false}),
	}}
}

func autonomyUpdateToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{
		Name:        "autonomy_update",
		Description: "调整当前 Auto Agent 已配置任务的目标、完成条件、下次唤醒时间或暂停。不能创建任务、恢复任务、改预算/次数/频率或重置使用量。",
		Parameters: injectCallPurposeParam(map[string]any{"type": "object", "properties": map[string]any{
			"objective":    map[string]any{"type": "string"},
			"acceptance":   map[string]any{"type": "string"},
			"next_wake_at": map[string]any{"type": "string", "format": "date-time"},
			"pause":        map[string]any{"type": "boolean"},
		}, "additionalProperties": false}),
	}}
}

func (r *Registry) execAutonomyGet(ctx context.Context, _ json.RawMessage) (string, error) {
	if r == nil || !r.autonomyEnabled || r.autonomyGet == nil {
		return "", fmt.Errorf("autonomy is unavailable")
	}
	if GoalIDFromContext(ctx) != "" {
		return "", fmt.Errorf("autonomy tools are unavailable during a managed goal run")
	}
	v, err := r.autonomyGet(ctx, r.agentID)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Registry) execAutonomyUpdate(ctx context.Context, raw json.RawMessage) (string, error) {
	if r == nil || !r.autonomyEnabled || r.autonomyUpdate == nil {
		return "", fmt.Errorf("autonomy is unavailable")
	}
	if GoalIDFromContext(ctx) != "" {
		return "", fmt.Errorf("autonomy tools are unavailable during a managed goal run")
	}
	var in struct {
		Objective  *string    `json:"objective"`
		Acceptance *string    `json:"acceptance"`
		NextWakeAt *time.Time `json:"next_wake_at"`
		Pause      *bool      `json:"pause"`
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return "", fmt.Errorf("invalid autonomy update: %w", err)
	}
	if in.Objective == nil && in.Acceptance == nil && in.NextWakeAt == nil && in.Pause == nil {
		return "", fmt.Errorf("autonomy update is empty")
	}
	if in.Objective != nil && strings.TrimSpace(*in.Objective) == "" {
		return "", fmt.Errorf("objective cannot be empty")
	}
	if in.Acceptance != nil && strings.TrimSpace(*in.Acceptance) == "" {
		return "", fmt.Errorf("acceptance cannot be empty")
	}
	u := AutonomyUpdate{Objective: in.Objective, Acceptance: in.Acceptance, NextWakeAt: in.NextWakeAt}
	if in.Pause != nil {
		u.Pause = *in.Pause
	}
	v, err := r.autonomyUpdate(ctx, r.agentID, u)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
