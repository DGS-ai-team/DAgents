package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/triggers"
)

// SetTriggerRuntime 注入触发器 store / scheduler 与默认 target_agent_id。
func (r *Registry) SetTriggerRuntime(store *triggers.Store, sched *triggers.Scheduler, agentID string) {
	r.triggerStore = store
	r.triggerSched = sched
	r.agentID = strings.TrimSpace(agentID)
}

// triggerConditionSchema keeps the model-facing condition contract structured
// without relying on provider-specific oneOf/anyOf support. Runtime validation
// remains authoritative in node/internal/triggers.
func triggerConditionSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "调度条件必须且只能选择 interval_seconds、fire_at 或 schedule 之一；cmd 仅用于 schedule 门控。",
		"properties": map[string]any{
			"interval_seconds": map[string]any{
				"type":        "integer",
				"minimum":     1,
				"description": "周期触发间隔（秒）。",
			},
			"fire_at": map[string]any{
				"type":        "number",
				"description": "单次触发时间，Unix 秒时间戳。",
			},
			"schedule": map[string]any{
				"type":        "object",
				"description": "日历触发：daily 需要 hour/minute；weekly 还需要 weekday（0=周日）；monthly 还需要 day（-1=最后一天）。",
				"properties": map[string]any{
					"kind": map[string]any{
						"type":        "string",
						"enum":        []string{"daily", "weekly", "monthly"},
						"description": "日历类型。",
					},
					"hour": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"maximum":     23,
						"description": "小时（0-23）。",
					},
					"minute": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"maximum":     59,
						"description": "分钟（0-59）。",
					},
					"weekday": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"maximum":     6,
						"description": "星期几（0=周日，6=周六），仅 weekly 使用。",
					},
					"day": map[string]any{
						"type":        "integer",
						"minimum":     -31,
						"maximum":     31,
						"description": "月份日期；正数为当月日期，-1 表示最后一天，仅 monthly 使用。",
					},
				},
				"required":             []string{"kind", "hour", "minute"},
				"additionalProperties": false,
			},
			"cmd": map[string]any{
				"type":        "string",
				"description": "可选的 bash 门控命令；仅 schedule 自动触发前执行，退出码为 0 才投递任务。",
			},
		},
		"additionalProperties": false,
	}
}

func triggerListToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "trigger_list",
			Description: "查看已配置的触发器列表；只读，不会执行或投递任务",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"include_disabled": map[string]any{
						"type":        "boolean",
						"description": "是否包含 enabled=false 的触发器（默认 true）",
					},
				},
				"required":             []string{},
				"additionalProperties": false,
			}),
		},
	}
}

func triggerGetToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "trigger_get",
			Description: "查看单个触发器配置与 next_fire_at；不执行触发",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"trigger_id": map[string]any{
						"type":        "string",
						"description": "触发器 ID（必填）",
					},
				},
				"required":             []string{"trigger_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func triggerCreateToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "trigger_create",
			Description: "新建触发器规则；condition 必须且只能选择一种调度方式。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "触发器显示名称（必填）",
					},
					"task_template": map[string]any{
						"type":        "string",
						"description": "触发时投递的任务正文模板（必填）；须自带必要上下文，避免触发后再次向用户追问",
					},
					"condition": triggerConditionSchema(),
				},
				"required":             []string{"name", "task_template", "condition"},
				"additionalProperties": false,
			}),
		},
	}
}

func triggerUpdateToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "trigger_update",
			Description: "修改已有触发器；未传字段保持不变，condition 规则同 trigger_create。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"trigger_id": map[string]any{
						"type":        "string",
						"description": "要修改的触发器 ID（必填）",
					},
					"name": map[string]any{
						"type":        "string",
						"description": "新名称（可选；未传则保持不变）",
					},
					"task_template": map[string]any{
						"type":        "string",
						"description": "新任务模板（可选；未传则保持不变）",
					},
					"condition": triggerConditionSchema(),
				},
				"required":             []string{"trigger_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func triggerDeleteToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "trigger_delete",
			Description: "删除不再需要的触发器规则",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"trigger_id": map[string]any{
						"type":        "string",
						"description": "要删除的触发器 ID（必填）",
					},
				},
				"required":             []string{"trigger_id"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) requireTriggerStore() (*triggers.Store, error) {
	if r.triggerStore == nil {
		return nil, fmt.Errorf("triggers not initialized")
	}
	return r.triggerStore, nil
}

func (r *Registry) execTriggerList(_ context.Context, raw json.RawMessage) (string, error) {
	store, err := r.requireTriggerStore()
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	cleaned := ParseToolCallArguments(string(raw))
	var args struct {
		IncludeDisabled *bool `json:"include_disabled"`
	}
	_ = json.Unmarshal([]byte(cleaned), &args)
	includeDisabled := true
	if args.IncludeDisabled != nil {
		includeDisabled = *args.IncludeDisabled
	}
	items := store.ListTriggers()
	if !includeDisabled {
		filtered := make([]triggers.Definition, 0, len(items))
		for _, item := range items {
			if item.Enabled {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	return triggerJSON(map[string]any{"ok": true, "triggers": items}), nil
}

func (r *Registry) execTriggerGet(_ context.Context, raw json.RawMessage) (string, error) {
	store, err := r.requireTriggerStore()
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	cleaned := ParseToolCallArguments(string(raw))
	var args struct {
		TriggerID string `json:"trigger_id"`
	}
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	def, ok := store.GetTrigger(strings.TrimSpace(args.TriggerID))
	if !ok {
		return triggerJSON(map[string]any{"ok": false, "error": "trigger not found", "trigger_id": args.TriggerID}), nil
	}
	return triggerJSON(map[string]any{"ok": true, "trigger": def}), nil
}

func (r *Registry) execTriggerCreate(ctx context.Context, raw json.RawMessage) (string, error) {
	store, err := r.requireTriggerStore()
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	cleaned := ParseToolCallArguments(string(raw))
	var args struct {
		Name         string         `json:"name"`
		TaskTemplate string         `json:"task_template"`
		Condition    map[string]any `json:"condition"`
	}
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	approvalTarget := TriggerSessionTargetFromContext(ctx)
	if approvalTarget == "" {
		approvalTarget = "same_session"
	}
	sessionID := sessionIDFromContext(ctx)
	mode, targetSession := triggers.SessionConfigFromApprovalTarget(approvalTarget, sessionID)
	now := time.Now()
	def, err := triggers.NewDefinitionFromCreate(triggers.CreateInput{
		Name:              strings.TrimSpace(args.Name),
		TaskTemplate:      strings.TrimSpace(args.TaskTemplate),
		Condition:         args.Condition,
		TargetAgentID:     r.agentID,
		TargetSessionID:   targetSession,
		SessionTargetMode: mode,
	}, r.agentID, now)
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	created, err := store.CreateTrigger(def)
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return triggerJSON(map[string]any{"ok": true, "trigger": created}), nil
}

func (r *Registry) execTriggerUpdate(_ context.Context, raw json.RawMessage) (string, error) {
	store, err := r.requireTriggerStore()
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	cleaned := ParseToolCallArguments(string(raw))
	var args struct {
		TriggerID    string         `json:"trigger_id"`
		Name         *string        `json:"name"`
		TaskTemplate *string        `json:"task_template"`
		Condition    map[string]any `json:"condition"`
	}
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	patch := triggers.UpdatePatch{}
	if args.Name != nil {
		patch.Name = args.Name
	}
	if args.TaskTemplate != nil {
		patch.TaskTemplate = args.TaskTemplate
	}
	if args.Condition != nil {
		patch.Condition = args.Condition
	}
	updated, err := store.UpdateTrigger(strings.TrimSpace(args.TriggerID), patch, time.Now())
	if triggers.IsNotFound(err) {
		return triggerJSON(map[string]any{"ok": false, "error": "trigger not found", "trigger_id": args.TriggerID}), nil
	}
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	return triggerJSON(map[string]any{"ok": true, "trigger": updated}), nil
}

func (r *Registry) execTriggerDelete(_ context.Context, raw json.RawMessage) (string, error) {
	store, err := r.requireTriggerStore()
	if err != nil {
		return triggerJSON(map[string]any{"ok": false, "error": err.Error()}), nil
	}
	cleaned := ParseToolCallArguments(string(raw))
	var args struct {
		TriggerID string `json:"trigger_id"`
	}
	if err := json.Unmarshal([]byte(cleaned), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	deleted := store.DeleteTrigger(strings.TrimSpace(args.TriggerID))
	return triggerJSON(map[string]any{"ok": true, "trigger_id": args.TriggerID, "deleted": deleted}), nil
}

func triggerJSON(payload map[string]any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
	}
	return string(data)
}
