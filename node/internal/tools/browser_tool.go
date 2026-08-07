package tools

import (
	"context"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

func (r *Registry) browserToolDefs() []ToolDef {
	return browserTaskToolDefs()
}

func (r *Registry) registerBrowserTools() {
	retired := []string{
		"browser_start", "browser_stop", "browser_navigate", "browser_click",
		"browser_click_coordinate", "browser_fill", "browser_press",
		"browser_screenshot", "browser_wait", "browser_snapshot",
		"browser_search", "browser_go_back", "browser_scroll", "browser_find_text",
		"browser_switch_tab", "browser_close_tab", "browser_extract", "browser_evaluate",
		"browser_find_elements", "browser_search_page", "browser_upload_file",
		"browser_dropdown_options", "browser_select_dropdown",
	}
	if r.browser == nil || !r.browser.Enabled() {
		for _, name := range append([]string{
			"browser_run_task", "browser_task_status", "browser_task_cancel",
		}, retired...) {
			delete(r.handlers, name)
		}
		return
	}
	for _, name := range retired {
		delete(r.handlers, name)
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
