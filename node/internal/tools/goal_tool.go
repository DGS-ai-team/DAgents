package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type GoalCheckpoint struct {
	Summary           string         `json:"summary"`
	Completed         []string       `json:"completed,omitempty"`
	NextSteps         []string       `json:"next_steps,omitempty"`
	Evidence          []string       `json:"evidence,omitempty"`
	Artifacts         []string       `json:"artifacts,omitempty"`
	Done              bool           `json:"done"`
	ExternalCondition string         `json:"external_condition,omitempty"`
	NextWakeAt        *time.Time     `json:"next_wake_at,omitempty"`
	Decision          map[string]any `json:"decision,omitempty"`
}

func goalCheckpointToolDef() ToolDef {
	decision := map[string]any{
		"type":        "object",
		"description": "终态决策；必须与本次运行的真实结果一致。",
		"properties": map[string]any{
			"outcome":                 map[string]any{"type": "string", "enum": []string{"progress", "no_change", "completed", "blocked"}},
			"summary":                 map[string]any{"type": "string"},
			"evidence":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"next_action":             map[string]any{"type": "string", "enum": []string{"at", "event", "none", "needs_input"}},
			"next_wake_at":            map[string]any{"type": "string", "format": "date-time"},
			"next_wake_after_seconds": map[string]any{"type": "integer", "minimum": 1, "maximum": 2678400},
			"event":                   map[string]any{"type": "object", "properties": map[string]any{"source_id": map[string]any{"type": "string"}, "filter": map[string]any{"type": "object"}}, "required": []string{"source_id"}, "additionalProperties": false},
			"reason":                  map[string]any{"type": "string"},
			"expected_progress":       map[string]any{"type": "string"},
		},
		"required":             []string{"outcome", "next_action", "reason", "summary"},
		"additionalProperties": false,
		"allOf":                []any{map[string]any{"if": map[string]any{"properties": map[string]any{"next_action": map[string]any{"enum": []string{"at", "event"}}}}, "then": map[string]any{"required": []string{"expected_progress"}}}},
	}
	params := map[string]any{"type": "object", "properties": map[string]any{
		"summary":            map[string]any{"type": "string"},
		"completed":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"next_steps":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"evidence":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"artifacts":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"external_condition": map[string]any{"type": "string"},
		"next_wake_at":       map[string]any{"type": "string", "format": "date-time"},
		"done":               map[string]any{"type": "boolean"}, "decision": decision,
	}, "required": []string{"summary"}, "additionalProperties": false}
	return ToolDef{Type: "function", Function: FunctionDef{Name: "goal_checkpoint", Description: "记录当前长期目标运行的进度、证据、产物和终态决策；只能更新当前 Goal/Run。完成或停止前必须提供 decision。", Parameters: injectCallPurposeParam(params)}}
}

func (r *Registry) execGoalCheckpoint(ctx context.Context, raw json.RawMessage) (string, error) {
	if r.goalCheckpoint == nil {
		return "", fmt.Errorf("goal runtime unavailable")
	}
	var cp GoalCheckpoint
	if err := json.Unmarshal(raw, &cp); err != nil {
		return "", err
	}
	gid := GoalIDFromContext(ctx)
	rid := RunIDFromContext(ctx)
	if gid == "" || rid == "" {
		return "", fmt.Errorf("goal checkpoint requires active goal run")
	}
	if err := r.goalCheckpoint(ctx, gid, rid, cp); err != nil {
		return "", err
	}
	b, _ := json.Marshal(map[string]any{"ok": true, "goal_id": gid, "run_id": rid, "checkpoint": cp})
	return string(b), nil
}
