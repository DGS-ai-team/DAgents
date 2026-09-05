package triggers

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ScheduleKind 由 condition 键推断的调度类型。
type ScheduleKind string

const (
	ScheduleManual   ScheduleKind = "manual"
	ScheduleInterval ScheduleKind = "interval"
	ScheduleOnce     ScheduleKind = "once"
	ScheduleCalendar ScheduleKind = "calendar"
)

// FireStatus 单次 fire 落库状态。
type FireStatus string

const (
	FireStatusQueued  FireStatus = "queued"
	FireStatusSkipped FireStatus = "skipped"
	FireStatusError   FireStatus = "error"
)

// SessionTargetMode 触发器 fire 时会话解析策略。
type SessionTargetMode string

const (
	SessionTargetFixed        SessionTargetMode = "fixed"
	SessionTargetNewSession   SessionTargetMode = "new_session"
	SessionTargetLatestActive SessionTargetMode = "latest_active"
)

// Definition 触发器完整定义（持久化主体）。
type Definition struct {
	TriggerID         string            `json:"trigger_id"`
	Name              string            `json:"name"`
	Condition         map[string]any    `json:"condition"`
	TargetAgentID     string            `json:"target_agent_id"`
	TargetSessionID   *string           `json:"target_session_id"`             // 绑定的对话 id
	SessionTargetMode SessionTargetMode `json:"session_target_mode,omitempty"` // 会话目标解析策略
	ClientID          *string           `json:"client_id"`
	TaskTemplate      string            `json:"task_template"`
	Enabled           bool              `json:"enabled"`
	FireCount         int               `json:"fire_count"`
	LastFiredAt       *float64          `json:"last_fired_at"`
	NextFireAt        *float64          `json:"next_fire_at"`
	CreatedAt         float64           `json:"created_at"`
	UpdatedAt         float64           `json:"updated_at"`
}

// CreateInput 创建触发器入参（工具 / HTTP）。
type CreateInput struct {
	Name              string            `json:"name"`
	Condition         map[string]any    `json:"condition"`
	TargetAgentID     string            `json:"target_agent_id"`
	TargetSessionID   *string           `json:"target_session_id"`
	SessionTargetMode SessionTargetMode `json:"session_target_mode,omitempty"`
	ClientID          *string           `json:"client_id"`
	TaskTemplate      string            `json:"task_template"`
}

// UpdatePatch 部分更新；nil 字段表示不修改。
type UpdatePatch struct {
	Name            *string        `json:"name,omitempty"`
	Condition       map[string]any `json:"condition,omitempty"`
	TargetAgentID   *string        `json:"target_agent_id,omitempty"`
	TargetSessionID *string        `json:"target_session_id,omitempty"`
	ClientID        *string        `json:"client_id,omitempty"`
	TaskTemplate    *string        `json:"task_template,omitempty"`
	Enabled         *bool          `json:"enabled,omitempty"`
}

// FireRecord 单次触发历史。
type FireRecord struct {
	FireID    string         `json:"fire_id"`
	TriggerID string         `json:"trigger_id"`
	Status    FireStatus     `json:"status"`
	Reason    string         `json:"reason"`
	SessionID *string        `json:"session_id"`
	ClientID  *string        `json:"client_id"`
	Content   string         `json:"content"`
	Message   string         `json:"message"`
	Payload   map[string]any `json:"payload"`
	FiredAt   float64        `json:"fired_at"`
}

// InferScheduleKind 根据 condition 推断调度类型。

// 关键分支：interval_seconds、fire_at、schedule 互斥；同时存在时返回 error。
func InferScheduleKind(condition map[string]any) (ScheduleKind, error) {
	interval := intFromAny(condition["interval_seconds"])
	fireAt := floatFromAny(condition["fire_at"])
	hasSchedule := hasScheduleObject(condition)
	setCount := 0
	if interval > 0 {
		setCount++
	}
	if fireAt > 0 {
		setCount++
	}
	if hasSchedule {
		setCount++
	}
	if setCount > 1 {
		return "", fmt.Errorf("condition cannot combine interval_seconds, fire_at, and schedule")
	}
	if interval > 0 {
		return ScheduleInterval, nil
	}
	if fireAt > 0 {
		return ScheduleOnce, nil
	}
	if hasSchedule {
		return ScheduleCalendar, nil
	}
	return ScheduleManual, nil
}

func hasScheduleObject(condition map[string]any) bool {
	raw, ok := condition["schedule"].(map[string]any)
	return ok && len(raw) > 0
}

// EnsureScheduleCondition 校验 condition 非空且含有效调度键。

// 异常：空 condition、仅 manual、或 schedule 字段非法时返回 error。
func EnsureScheduleCondition(condition map[string]any) (ScheduleKind, error) {
	if len(condition) == 0 {
		return "", fmt.Errorf("condition is required and cannot be empty")
	}
	kind, err := InferScheduleKind(condition)
	if err != nil {
		return "", err
	}
	if kind == ScheduleManual {
		return "", fmt.Errorf("condition must include interval_seconds, fire_at, or schedule")
	}
	if kind == ScheduleCalendar {
		if _, err := ParseScheduleSpec(condition); err != nil {
			return "", err
		}
	}
	return kind, nil
}

// NewDefinitionFromCreate 由创建入参构造 Definition 并计算 next_fire_at。

// 副作用：新建资源默认 enabled=true。
func NewDefinitionFromCreate(in CreateInput, agentID string, now time.Time) (Definition, error) {
	if _, err := EnsureScheduleCondition(in.Condition); err != nil {
		return Definition{}, err
	}
	current := float64(now.UnixNano()) / 1e9
	targetAgent := strings.TrimSpace(in.TargetAgentID)
	if targetAgent == "" {
		targetAgent = strings.TrimSpace(agentID)
	}
	if targetAgent == "" {
		return Definition{}, fmt.Errorf("target_agent_id is required")
	}
	mode := in.SessionTargetMode
	if mode == "" {
		mode = SessionTargetFixed
	}
	def := Definition{
		TriggerID:         uuid.NewString(),
		Name:              in.Name,
		Condition:         cloneMap(in.Condition),
		TargetAgentID:     targetAgent,
		TargetSessionID:   copyStringPtr(in.TargetSessionID),
		SessionTargetMode: mode,
		ClientID:          copyStringPtr(in.ClientID),
		TaskTemplate:      in.TaskTemplate,
		Enabled:           true,
		CreatedAt:         current,
		UpdatedAt:         current,
	}
	return def.WithNextFire(now), nil
}

// WithNextFire 重算 next_fire_at（返回副本并刷新 updated_at）。
func (d Definition) WithNextFire(now time.Time) Definition {
	current := timeToUnixFloat(now)
	next := ComputeNextFireTime(d, now)
	d.NextFireAt = nil
	if next != nil {
		v := timeToUnixFloat(*next)
		d.NextFireAt = &v
	}
	d.UpdatedAt = current
	return d
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	default:
		return 0
	}
}

func floatFromAny(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return 0
	}
}

func cloneMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func copyStringPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
