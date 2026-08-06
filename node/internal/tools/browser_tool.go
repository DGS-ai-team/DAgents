package tools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/browser"
)

func (r *Registry) browserToolDefs() []ToolDef {
	// 任务级工具始终暴露（由 enabled_groups=browser 过滤到主 Agent）。
	defs := browserTaskToolDefs()
	if r != nil && r.multimodalEnabled {
		return append(defs, browserVisualToolDefs()...)
	}
	return append(defs, browserNonVisualToolDefs()...)
}

func browserNonVisualToolDefs() []ToolDef {
	base := []ToolDef{
		browserStartToolDef(),
		browserStopToolDef(),
		browserNavigateToolDef(false),
		browserClickToolDef(false),
		browserFillToolDef(false),
		browserPressToolDef(),
		browserScreenshotToolDef(false),
		browserWaitToolDef(false),
		browserSnapshotToolDef(false),
	}
	return append(base, browserExtendedToolDefs()...)
}

func browserVisualToolDefs() []ToolDef {
	base := []ToolDef{
		browserStartToolDef(),
		browserStopToolDef(),
		browserNavigateToolDef(true),
		browserClickToolDef(true),
		browserClickCoordinateToolDef(),
		browserFillToolDef(true),
		browserPressToolDef(),
		browserScreenshotToolDef(true),
		browserWaitToolDef(true),
		browserSnapshotToolDef(true),
	}
	return append(base, browserExtendedToolDefs()...)
}

func browserStartToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_start",
			Description: "启动绑定当前 session 的 Chromium（默认 headed）。同 session 重复调用为幂等。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"headed": map[string]any{
						"type":        "boolean",
						"description": "是否 headed；省略时用 config browser.headed",
					},
					"viewport_width": map[string]any{
						"type":        "integer",
						"minimum":     320,
						"description": "视口宽，默认 1280",
					},
					"viewport_height": map[string]any{
						"type":        "integer",
						"minimum":     240,
						"description": "视口高，默认 720",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func browserStopToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_stop",
			Description: "关闭当前 session 的 Chromium 并释放资源。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			}),
		},
	}
}

func browserNavigateToolDef(visual bool) ToolDef {
	desc := "在当前 browser session 中打开 URL（仅 http/https）。"
	if visual {
		desc += " 多模态已启用：导航后请调用 browser_snapshot 获取截图（自动注入视觉模型）。"
	}
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_navigate",
			Description: desc,
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"url": map[string]any{
						"type":        "string",
						"description": "目标 URL",
					},
					"wait_until": map[string]any{
						"type":        "string",
						"enum":        []string{"load", "domcontentloaded", "networkidle", "commit"},
						"description": "导航等待条件，默认 load",
					},
				},
				"required":             []string{"url"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserClickToolDef(visual bool) ToolDef {
	desc := "点击可交互元素。须先 browser_snapshot；优先用返回的 index（browser-use 非视觉主路径），selector 仅作 fallback。"
	if visual {
		desc = "点击可交互元素。视觉模式：优先 browser_click_coordinate（依据截图坐标）；index/selector 作 fallback。须先 browser_snapshot。"
	}
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_click",
			Description: desc,
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "browser_snapshot.llm_representation 中的元素编号（推荐）",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "fallback：CSS selector（index 不可用时可试）",
					},
					"selector_fallbacks": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "fallback 备选 CSS selector",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func browserClickCoordinateToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "browser_click_coordinate",
			Description: "在视口像素坐标 (x,y) 点击。多模态视觉模式主路径：须先 browser_snapshot 查看截图，" +
				"根据截图判断目标位置后调用。坐标原点在视口左上角。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"x": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"description": "视口 X 坐标（像素）",
					},
					"y": map[string]any{
						"type":        "integer",
						"minimum":     0,
						"description": "视口 Y 坐标（像素）",
					},
					"button": map[string]any{
						"type":        "string",
						"enum":        []string{"left", "right", "middle"},
						"description": "鼠标键，默认 left",
					},
				},
				"required":             []string{"x", "y"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserFillToolDef(visual bool) ToolDef {
	desc := "向输入框填写文本。须先 browser_snapshot；优先 index，selector 为 fallback。"
	if visual {
		desc = "向输入框填写文本。须先 browser_snapshot；可先 browser_click_coordinate 聚焦输入框，再 fill；index/selector 作 fallback。"
	}
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_fill",
			Description: desc,
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "browser_snapshot 中的元素编号（推荐）",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "fallback：CSS selector",
					},
					"text": map[string]any{
						"type":        "string",
						"description": "填写内容",
					},
				},
				"required":             []string{"text"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserPressToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_press",
			Description: "在当前页面按下键盘键（如 Enter、Tab）。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"key": map[string]any{
						"type":        "string",
						"description": "Playwright 键名，如 Enter",
					},
				},
				"required":             []string{"key"},
				"additionalProperties": false,
			}),
		},
	}
}

func browserScreenshotToolDef(visual bool) ToolDef {
	desc := "截取当前页 PNG 到工作区 browser/ 目录；返回相对 fs_root 路径，可配合 read_image。"
	if visual {
		desc = "截取当前页 PNG；截图会自动注入视觉模型（无需再调用 read_image）。"
	}
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_screenshot",
			Description: desc,
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"name": map[string]any{
						"type":        "string",
						"description": "文件名前缀，默认 shot",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func browserWaitToolDef(visual bool) ToolDef {
	desc := "等待 index 对应元素出现、CSS selector 可见，或页面 load state。"
	if visual {
		desc += " 视觉模式下也可在 browser_snapshot 后观察截图变化。"
	}
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_wait",
			Description: desc,
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "等待 browser_snapshot 中的元素编号出现",
					},
					"selector": map[string]any{
						"type":        "string",
						"description": "fallback：等待 CSS selector",
					},
					"load_state": map[string]any{
						"type":        "string",
						"enum":        []string{"load", "domcontentloaded", "networkidle"},
						"description": "或等待页面 load state（与 selector 二选一）",
					},
					"timeout_ms": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"description": "超时毫秒，默认 config browser.default_timeout_ms",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func browserSnapshotToolDef(visual bool) ToolDef {
	desc := "返回 browser-use 非视觉页面状态：llm_representation（[index]<tag>text</tag> 格式）及元素列表。后续 browser_click/browser_fill 须用 index。"
	if visual {
		desc = "返回页面截图（自动注入视觉模型）及 DOM 摘要（llm_representation/index 作辅助）。" +
			"视觉模式主路径：观察截图 → browser_click_coordinate(x,y)。"
	}
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name:        "browser_snapshot",
			Description: desc,
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"max_elements": map[string]any{
						"type":        "integer",
						"minimum":     1,
						"maximum":     500,
						"description": "elements 列表最多条数，默认 150；llm_representation 不受此限",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) registerBrowserTools() {
	if r.browser == nil || !r.browser.Enabled() {
		for _, name := range []string{
			"browser_run_task", "browser_task_status", "browser_task_cancel",
			"browser_start", "browser_stop", "browser_navigate", "browser_click",
			"browser_click_coordinate", "browser_fill", "browser_press",
			"browser_screenshot", "browser_wait", "browser_snapshot",
		} {
			delete(r.handlers, name)
		}
		return
	}
	r.handlers["browser_run_task"] = r.execBrowserRunTask
	r.handlers["browser_task_status"] = r.execBrowserTaskStatus
	r.handlers["browser_task_cancel"] = r.execBrowserTaskCancel
	r.handlers["browser_start"] = r.execBrowserStart
	r.handlers["browser_stop"] = r.execBrowserStop
	r.handlers["browser_navigate"] = r.execBrowserNavigate
	r.handlers["browser_click"] = r.execBrowserClick
	if r.multimodalEnabled {
		r.handlers["browser_click_coordinate"] = r.execBrowserClickCoordinate
	} else {
		delete(r.handlers, "browser_click_coordinate")
	}
	r.handlers["browser_fill"] = r.execBrowserFill
	r.handlers["browser_press"] = r.execBrowserPress
	r.handlers["browser_screenshot"] = r.execBrowserScreenshot
	r.handlers["browser_wait"] = r.execBrowserWait
	r.handlers["browser_snapshot"] = r.execBrowserSnapshot
	r.registerBrowserExtendedTools()
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

func (r *Registry) execBrowserStart(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Headed         *bool `json:"headed"`
		ViewportWidth  int   `json:"viewport_width"`
		ViewportHeight int   `json:"viewport_height"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	out, err := r.browser.Start(ctx, sid, args.Headed, args.ViewportWidth, args.ViewportHeight)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserStop(ctx context.Context, _ json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	out, err := r.browser.Stop(ctx, sid)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserNavigate(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		URL       string `json:"url"`
		WaitUntil string `json:"wait_until"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Navigate(ctx, sid, args.URL, args.WaitUntil)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserClick(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Index             int      `json:"index"`
		Selector          string   `json:"selector"`
		SelectorFallbacks []string `json:"selector_fallbacks"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Click(ctx, sid, args.Index, args.Selector, args.SelectorFallbacks)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserFill(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Index    int    `json:"index"`
		Selector string `json:"selector"`
		Text     string `json:"text"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Fill(ctx, sid, args.Index, args.Selector, args.Text)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserPress(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Press(ctx, sid, args.Key)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserScreenshot(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Name string `json:"name"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = browser.ScreenshotName("shot")
	}
	out, err := r.browser.Screenshot(ctx, sid, name)
	if err != nil {
		return "", err
	}
	if out.OK && out.ScreenshotPath != "" {
		r.stashBrowserVisionFromScreenshot(ctx, out.ScreenshotPath, "browser_screenshot")
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserWait(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		Index     int    `json:"index"`
		Selector  string `json:"selector"`
		LoadState string `json:"load_state"`
		TimeoutMS int    `json:"timeout_ms"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.Wait(ctx, sid, args.Index, args.Selector, args.LoadState, args.TimeoutMS)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserSnapshot(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	var args struct {
		MaxElements int `json:"max_elements"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	out, err := r.browser.Snapshot(ctx, sid, args.MaxElements, r.multimodalEnabled, "")
	if err != nil {
		return "", err
	}
	if out.OK && out.ScreenshotPath != "" {
		r.stashBrowserVisionFromScreenshot(ctx, out.ScreenshotPath, "browser_snapshot")
	}
	return browser.FormatToolResult(out), nil
}

func (r *Registry) execBrowserClickCoordinate(ctx context.Context, raw json.RawMessage) (string, error) {
	sid, errText := r.browserSession(ctx)
	if errText != "" {
		return errText, nil
	}
	if !r.multimodalEnabled {
		return browser.FormatToolResult(browser.ToolResult{
			OK:    false,
			Error: "browser_click_coordinate 需要 multimodal.enabled（视觉模式）",
		}), nil
	}
	var args struct {
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Button string `json:"button"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	out, err := r.browser.ClickCoordinate(ctx, sid, args.X, args.Y, args.Button)
	if err != nil {
		return "", err
	}
	return browser.FormatToolResult(out), nil
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
