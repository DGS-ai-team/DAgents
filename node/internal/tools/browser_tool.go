package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

// BrowserTaskDone 描述 browser_run_task(wait=false) 的终态回灌。
// ToolCallID 保留原始调用 ID，便于 side-effect 层把异步结果关联回原请求。
type BrowserTaskDone struct {
	TaskID     string
	ToolCallID string
	Status     string
	ResultText string
	ErrorText  string
}

// BrowserTaskNotifier 将 browser sidecar 的后台任务终态交给 session 层。
type BrowserTaskNotifier func(sessionID string, done BrowserTaskDone)

func (r *Registry) browserToolDefs() []ToolDef {
	return browserTaskToolDefs()
}

func (r *Registry) registerBrowserTools() {
	if r.browser == nil || !r.browser.Enabled() {
		for _, name := range []string{
			"browser_run_task", "browser_task_status", "browser_task_cancel",
		} {
			delete(r.handlers, name)
		}
		return
	}
	r.handlers["browser_run_task"] = r.execBrowserRunTask
	r.handlers["browser_task_status"] = r.execBrowserTaskStatus
	r.handlers["browser_task_cancel"] = r.execBrowserTaskCancel
}

func (r *Registry) browserSession(ctx context.Context) (string, string) {
	sid := sessionIDFromContext(ctx)
	if sid == "" {
		return "", browser.FormatToolResult(browser.ToolResult{OK: false, Error: "missing session context"})
	}
	if r.browser == nil || !r.browser.Enabled() {
		return "", browser.FormatToolResult(browser.ToolResult{OK: false, Error: "browser tools disabled (set browser.enabled: true)"})
	}
	return sid, ""
}

// SetBrowserManager 注入 BrowserManager；nil 或 disabled 时不暴露 browser_* 工具。
func (r *Registry) SetBrowserManager(mgr *browser.Manager) {
	if r == nil {
		return
	}
	r.browser = mgr
	r.registerBrowserTools()
}

// SetBrowserTaskNotifier 启用 browser_run_task(wait=false) 的自动回灌。
// 未绑定时仍保留原有显式 browser_task_status 语义，便于嵌入式调用方控制生命周期。
func (r *Registry) SetBrowserTaskNotifier(fn BrowserTaskNotifier) {
	if r == nil {
		return
	}
	r.browserTaskMu.Lock()
	r.browserTaskNotifier = fn
	r.browserTaskMu.Unlock()
}

// CloseBrowser 关闭 remote 侧全部 browser session。
func (r *Registry) CloseBrowser() error {
	if r == nil || r.browser == nil {
		return nil
	}
	return r.browser.Close()
}

func (r *Registry) browserToolsEnabled() bool {
	return r != nil && r.browser != nil && r.browser.Enabled()
}

func (r *Registry) browserTaskNotifierSnapshot() BrowserTaskNotifier {
	if r == nil {
		return nil
	}
	r.browserTaskMu.Lock()
	defer r.browserTaskMu.Unlock()
	return r.browserTaskNotifier
}

func browserTaskStatus(detail map[string]any) (status string, terminal bool, success bool, errText string) {
	status = strings.ToLower(strings.TrimSpace(fmt.Sprint(detail["status"])))
	switch status {
	case "completed":
		terminal = true
		success, _ = detail["success"].(bool)
		if _, ok := detail["success"]; !ok {
			success = true
		}
		if !success {
			errText = strings.TrimSpace(fmt.Sprint(detail["error"]))
			if errText == "<nil>" {
				errText = "浏览器任务执行失败"
			}
			return "failed", terminal, success, errText
		}
		return "succeeded", terminal, success, ""
	case "failed", "cancelled":
		terminal = true
		errText = strings.TrimSpace(fmt.Sprint(detail["error"]))
		if errText == "<nil>" {
			errText = "浏览器任务" + map[string]string{"failed": "执行失败", "cancelled": "已取消"}[status]
		}
		return status, terminal, false, errText
	default:
		return status, false, false, ""
	}
}

// watchBrowserTask 在 wait=false 返回后轮询 sidecar 终态，并把完成结果转为
// session 的 async_tool_result。显式 browser_task_status 仍可随时查询，不会重复回灌。
func (r *Registry) watchBrowserTask(parentSessionID, companionSessionKey, taskID, toolCallID string) {
	parentSessionID = strings.TrimSpace(parentSessionID)
	companionSessionKey = strings.TrimSpace(companionSessionKey)
	taskID = strings.TrimSpace(taskID)
	if r == nil || parentSessionID == "" || companionSessionKey == "" || taskID == "" {
		return
	}
	notifier := r.browserTaskNotifierSnapshot()
	if notifier == nil || r.browser == nil {
		return
	}
	key := parentSessionID + "\x00" + taskID
	r.browserTaskMu.Lock()
	if _, exists := r.browserTaskWatchers[key]; exists {
		r.browserTaskMu.Unlock()
		return
	}
	r.browserTaskWatchers[key] = struct{}{}
	r.browserTaskMu.Unlock()

	go func() {
		defer func() {
			r.browserTaskMu.Lock()
			delete(r.browserTaskWatchers, key)
			r.browserTaskMu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 24*time.Hour)
		defer cancel()
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			out, err := r.browser.TaskStatus(ctx, companionSessionKey, taskID)
			if err == nil && out.Detail != nil {
				status, terminal, _, errText := browserTaskStatus(out.Detail)
				if terminal {
					notifier(parentSessionID, BrowserTaskDone{
						TaskID:     taskID,
						ToolCallID: toolCallID,
						Status:     status,
						ResultText: browser.FormatToolResult(out),
						ErrorText:  errText,
					})
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
