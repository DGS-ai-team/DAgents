package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type GoalCheckpoint struct {
	Summary           string     `json:"summary"`
	Completed         []string   `json:"completed,omitempty"`
	NextSteps         []string   `json:"next_steps,omitempty"`
	Evidence          []string   `json:"evidence,omitempty"`
	Artifacts         []string   `json:"artifacts,omitempty"`
	Done              bool       `json:"done"`
	ExternalCondition string     `json:"external_condition,omitempty"`
	NextWakeAt        *time.Time `json:"next_wake_at,omitempty"`
}

func goalCheckpointToolDef() ToolDef {
	return ToolDef{Type: "function", Function: FunctionDef{Name: "goal_checkpoint", Description: "记录当前长期目标运行的进度、证据、产物和下一步；只能更新当前 Goal/Run。", Parameters: injectCallPurposeParam(map[string]any{"type": "object", "properties": map[string]any{"summary": map[string]any{"type": "string"}, "completed": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "next_steps": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "evidence": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "artifacts": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "external_condition": map[string]any{"type": "string"}, "next_wake_at": map[string]any{"type": "string", "format": "date-time"}, "done": map[string]any{"type": "boolean"}}, "required": []string{"summary"}, "additionalProperties": false})}}
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
