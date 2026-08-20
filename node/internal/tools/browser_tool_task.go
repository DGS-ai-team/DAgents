package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

const companionBrowserSuffix = "-browser"

// BrowserCompanionExistsFunc 校验伴生 Agent 记录是否存在（由 API 层注入）。
type BrowserCompanionExistsFunc func(ctx context.Context, companionAgentID string) (bool, error)

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
			Description: "向本 Agent 的浏览器伴生派发网页任务（sidecar browser-use.Agent）。" +
				"默认 wait=true：阻塞至任务完成并返回 summary；长任务可设 wait=false 后用 browser_task_status 轮询。" +
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
					"wait": map[string]any{
						"type":        "boolean",
						"description": "是否等待任务结束再返回；默认 true。false 时立即返回 task_id",
					},
					"wait_timeout_seconds": map[string]any{
						"type":        "integer",
						"minimum":     5,
						"maximum":     1800,
						"description": "wait=true 时的最长等待秒数，默认 300",
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
			Name:        "browser_task_status",
			Description: "查询浏览器伴生任务状态，用于 browser_run_task(wait=false) 或等待超时的任务。完成时返回摘要、成功状态、步数、URL、截图路径和错误信息。",
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

// SetBrowserCompanionExists 注入伴生记录存在性检查；nil 表示跳过校验。
func (r *Registry) SetBrowserCompanionExists(fn BrowserCompanionExistsFunc) {
	if r == nil {
		return
	}
	r.browserCompanionExists = fn
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
	if r.browserCompanionExists != nil {
		ok, err := r.browserCompanionExists(ctx, companionID)
		if err != nil {
			return "", browser.FormatToolResult(browser.ToolResult{OK: false, Error: "browser companion check failed: " + err.Error()})
		}
		if !ok {
			return "", browser.FormatToolResult(browser.ToolResult{
				OK: false,
				Error: "浏览器伴生不存在（" + companionID + "）。" +
					"请确认本 Agent 已启用 browser 工具组后重新打开/重载 Agent，或重建 Agent。",
			})
		}
	}
	return companionID, ""
}

func (r *Registry) registerBrowserTaskScreenshots(ctx context.Context, toolName string, out browser.ToolResult) {
	if r == nil || !out.OK {
		return
	}
	payload := browser.FormatToolResult(out)
	for _, spec := range ExtractAllToolMediaPaths(toolName, payload, nil) {
		r.registerToolMedia(ctx, toolCallIDFromContext(ctx), spec.RelPath, spec.Source, spec.Label, spec.Caption)
	}
}

func (r *Registry) execBrowserRunTask(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserCompanionSessionKey(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Task               string `json:"task"`
		MaxSteps           int    `json:"max_steps"`
		Wait               *bool  `json:"wait"`
		WaitTimeoutSeconds int    `json:"wait_timeout_seconds"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	wait := true
	if args.Wait != nil {
		wait = *args.Wait
	}
	out, err := r.browser.RunTaskWithOpts(ctx, sid, args.Task, browser.RunTaskOpts{
		MaxSteps:       args.MaxSteps,
		Wait:           wait,
		WaitTimeoutSec: args.WaitTimeoutSeconds,
	})
	if err != nil {
		return "", err
	}
	r.registerBrowserTaskScreenshots(ctx, "browser_run_task", out)
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
	r.registerBrowserTaskScreenshots(ctx, "browser_task_status", out)
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
