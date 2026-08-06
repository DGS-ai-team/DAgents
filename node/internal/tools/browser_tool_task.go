package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

const companionBrowserSuffix = "-browser"

func companionBrowserAgentID(parentAgentID string) string {
	parentAgentID = strings.TrimSpace(parentAgentID)
	if parentAgentID == "" {
		return ""
	}
	if strings.HasSuffix(parentAgentID, companionBrowserSuffix) {
		return parentAgentID
	}
	return parentAgentID + companionBrowserSuffix
}

func browserTaskToolDefs() []ToolDef {
	return []ToolDef{
		browserRunTaskToolDef(),
		browserTaskStatusToolDef(),
		browserTaskCancelToolDef(),
	}
}

func browserRunTaskToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "browser_run_task",
			Description: "向本 Agent 的浏览器伴生派发一个网页任务（sidecar browser-use.Agent 闭环执行）。" +
				"立即返回 task_id；用 browser_task_status 轮询直至 status=completed|failed|cancelled。" +
				"任务描述请写清目标、约束与期望输出格式。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task": map[string]any{
						"type":        "string",
						"description": "自然语言目标，例如「打开 example.com 并提取标题；用中文回报」",
					},
					"max_steps": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     200,
						"description": "browser-use Agent 最大步数，默认由 sidecar 决定",
					},
				},
				"required":             []string{"task"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserTaskStatusToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "browser_task_status",
			Description: "查询浏览器伴生任务状态。completed 时 detail 含：" +
				"summary（给主 Agent 的结论，同 extracted_content）、success、steps、urls、" +
				"screenshot_paths、errors、duration_seconds；可直接引用 summary 回复用户。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "string",
						"description": "browser_run_task 返回的 task_id；省略则查最近一次任务",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func browserTaskCancelToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_task_cancel",
			Description: "取消浏览器伴生上运行中的任务。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"task_id": map[string]any{
						"type":        "string",
						"description": "要取消的 task_id；省略则取消当前运行中任务",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

// browserCompanionSessionKey 将主 Agent session 映射为伴生 Chrome session_key。
func (r *Registry) browserCompanionSessionKey(ctx context.Context) (string, string) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return "", errText
	}
	companionID := companionBrowserAgentID(sid)
	if companionID == "" {
		return "", browser.FormatToolResult(browser.ToolResult{OK: false, Error: "cannot resolve browser companion id"})
	}
	return companionID, ""
}

func (r *Registry) execBrowserRunTask(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserCompanionSessionKey(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Task     string `json:"task"`
		MaxSteps int    `json:"max_steps"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.RunTask(ctx, sid, args.Task, args.MaxSteps)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserTaskStatus(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserCompanionSessionKey(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		TaskID string `json:"task_id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	out, err := r.browser.TaskStatus(ctx, sid, strings.TrimSpace(args.TaskID))
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserTaskCancel(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserCompanionSessionKey(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		TaskID string `json:"task_id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	out, err := r.browser.TaskCancel(ctx, sid, strings.TrimSpace(args.TaskID))
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}
