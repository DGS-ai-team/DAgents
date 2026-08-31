package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/computeruse"
	"github.com/DGS-ai-team/DAgents/node/internal/screen"
)

type screenCaptureArgs struct {
	Detail string `json:"detail"`
}

type computerUseArgs struct {
	Action    string   `json:"action"`
	X         *int     `json:"x"`
	Y         *int     `json:"y"`
	FromX     *int     `json:"from_x"`
	FromY     *int     `json:"from_y"`
	Button    string   `json:"button"`
	ScrollY   int      `json:"scroll_y"`
	Key       string   `json:"key"`
	Modifiers []string `json:"modifiers"`
	Text      string   `json:"text"`
}

type screenGeometry struct {
	Width      int
	Height     int
	Bounds     screen.Bounds
	CapturedAt time.Time
}

func screenCaptureToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "screen_capture",
			Description: "截取当前主机的真实虚拟桌面；使用多模态模型时会把图像附加到下一次模型输入，否则仍作为工具结果图片展示。" +
				" 多模态模型收到的截图会叠加像素坐标网格，顶部和左侧数字表示截图坐标；返回截图像素尺寸与操作坐标系，computer_use 的坐标必须基于最近一次截图。" +
				" 多显示器会合并为一张图；图形会话不可用时明确失败。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"detail": map[string]any{
						"type":        "string",
						"enum":        []string{"auto", "low", "high"},
						"description": "视觉模型图像细节级别，默认 high",
					},
				},
				"additionalProperties": false,
			}),
		},
	}
}

func computerUseToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "computer_use",
			Description: "在当前主机桌面执行一次鼠标或键盘动作，并自动返回动作后的屏幕截图。" +
				" 调用坐标动作前必须先调用 screen_capture；x/y 与 from_x/from_y 使用最近截图返回的像素坐标系，不是操作系统原始坐标。" +
				" 截图中的坐标网格可用于定位目标，顶部和左侧的数字是截图像素坐标。" +
				" action 支持 move、click、double_click、drag、scroll、key、type_text。" +
				" scroll_y 为 -20..20，正数向下、负数向上；key 可配 modifiers（ctrl/alt/shift/meta）。" +
				" 桌面操作默认进入审批，不得用于输入密码、验证码、支付信息或绕过系统安全提示。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"action": map[string]any{
						"type": "string",
						"enum": []string{
							computeruse.ActionMove,
							computeruse.ActionClick,
							computeruse.ActionDoubleClick,
							computeruse.ActionDrag,
							computeruse.ActionScroll,
							computeruse.ActionKey,
							computeruse.ActionTypeText,
						},
						"description": "单次桌面动作",
					},
					"x":      map[string]any{"type": "integer", "description": "最近截图坐标系中的目标 x"},
					"y":      map[string]any{"type": "integer", "description": "最近截图坐标系中的目标 y"},
					"from_x": map[string]any{"type": "integer", "description": "drag 起点 x"},
					"from_y": map[string]any{"type": "integer", "description": "drag 起点 y"},
					"button": map[string]any{
						"type":        "string",
						"enum":        []string{"left", "middle", "right"},
						"description": "鼠标按钮，默认 left",
					},
					"scroll_y": map[string]any{
						"type":        "integer",
						"minimum":     -20,
						"maximum":     20,
						"description": "滚动格数；正数向下，负数向上",
					},
					"key": map[string]any{"type": "string", "description": "key 动作的按键名"},
					"modifiers": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string", "enum": []string{"ctrl", "alt", "shift", "meta"}},
						"maxItems":    4,
						"description": "key 动作的组合修饰键",
					},
					"text": map[string]any{
						"type":        "string",
						"maxLength":   4000,
						"description": "type_text 写入活动控件的文本",
					},
				},
				"required":             []string{"action"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execScreenCapture(ctx context.Context, raw json.RawMessage) (string, error) {
	var args screenCaptureArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	detail := strings.ToLower(strings.TrimSpace(args.Detail))
	if detail == "" {
		detail = "high"
	}
	if detail != "auto" && detail != "low" && detail != "high" {
		return "", fmt.Errorf("invalid detail %q", args.Detail)
	}
	r.desktopMu.Lock()
	defer r.desktopMu.Unlock()
	frame, path, err := r.captureDesktopForTool(ctx, "screen_capture", detail)
	if err != nil {
		return desktopFailure("screen_capture", err), err
	}
	return desktopSuccess("screen_capture", "capture", frame, path, map[string]any{
		"backend":         screen.Default().Backend(),
		"vision_attached": r.multimodalEnabled,
		"coordinate_grid": coordinateGridMetadata(frame.Width, frame.Height, r.multimodalEnabled),
	}), nil
}

func (r *Registry) execComputerUse(ctx context.Context, raw json.RawMessage) (string, error) {
	var args computerUseArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if !r.multimodalEnabled {
		err := fmt.Errorf("computer_use requires a multimodal model so it can inspect each screenshot")
		return desktopFailure("computer_use", err), err
	}
	r.desktopMu.Lock()
	defer r.desktopMu.Unlock()

	backend := computeruse.Default()
	status := backend.Status()
	if !status.Available {
		err := fmt.Errorf("computer use unavailable: %s", status.Reason)
		return desktopFailure("computer_use", err), err
	}
	if display := screen.Default().Status(); !display.Available {
		err := fmt.Errorf("computer use unavailable: %s", display.Reason)
		return desktopFailure("computer_use", err), err
	}
	action, err := r.resolveComputerAction(ctx, args)
	if err != nil {
		return desktopFailure("computer_use", err), err
	}
	if err := backend.Execute(ctx, action); err != nil {
		return desktopFailure("computer_use", err), err
	}
	if err := waitForDesktopPaint(ctx, 160*time.Millisecond); err != nil {
		return desktopFailure("computer_use", err), err
	}
	frame, path, err := r.captureDesktopForTool(ctx, "computer_use", "high")
	if err != nil {
		return desktopFailure("computer_use", fmt.Errorf("action completed but follow-up screenshot failed: %w", err)), err
	}
	extra := map[string]any{
		"backend":         status.Backend,
		"vision_attached": r.multimodalEnabled,
		"coordinate_grid": coordinateGridMetadata(frame.Width, frame.Height, r.multimodalEnabled),
	}
	if action.HasPoint {
		extra["os_point"] = map[string]any{"x": action.X, "y": action.Y}
	}
	return desktopSuccess("computer_use", action.Name, frame, path, extra), nil
}

func (r *Registry) resolveComputerAction(ctx context.Context, args computerUseArgs) (computeruse.Action, error) {
	action := computeruse.Action{
		Name:      strings.ToLower(strings.TrimSpace(args.Action)),
		Button:    strings.ToLower(strings.TrimSpace(args.Button)),
		ScrollY:   args.ScrollY,
		Key:       strings.ToLower(strings.TrimSpace(args.Key)),
		Modifiers: append([]string(nil), args.Modifiers...),
		Text:      args.Text,
	}
	needsPoint := action.Name == computeruse.ActionMove ||
		action.Name == computeruse.ActionClick ||
		action.Name == computeruse.ActionDoubleClick ||
		action.Name == computeruse.ActionDrag
	hasPoint := args.X != nil || args.Y != nil
	if hasPoint && (args.X == nil || args.Y == nil) {
		return action, fmt.Errorf("x and y must be provided together")
	}
	if needsPoint && !hasPoint {
		return action, fmt.Errorf("x and y are required for %s", action.Name)
	}
	key := desktopSessionKey(ctx)
	geometry, hasGeometry := r.desktopFrames[key]
	if !hasGeometry {
		return action, fmt.Errorf("call screen_capture before computer_use")
	}
	if !geometry.CapturedAt.IsZero() && time.Since(geometry.CapturedAt) > 5*time.Minute {
		return action, fmt.Errorf("the last screenshot is stale; call screen_capture again")
	}
	if hasPoint {
		x, y, err := mapScreenPoint(geometry, *args.X, *args.Y)
		if err != nil {
			return action, err
		}
		action.X, action.Y, action.HasPoint = x, y, true
	}
	if args.FromX != nil || args.FromY != nil {
		if args.FromX == nil || args.FromY == nil {
			return action, fmt.Errorf("from_x and from_y must be provided together")
		}
		x, y, err := mapScreenPoint(geometry, *args.FromX, *args.FromY)
		if err != nil {
			return action, err
		}
		action.FromX, action.FromY, action.HasFrom = x, y, true
	}
	if err := action.Validate(); err != nil {
		return action, err
	}
	return action, nil
}

func mapScreenPoint(geometry screenGeometry, x, y int) (int, int, error) {
	if geometry.Width <= 0 || geometry.Height <= 0 || geometry.Bounds.Width <= 0 || geometry.Bounds.Height <= 0 {
		return 0, 0, fmt.Errorf("invalid screen geometry; call screen_capture again")
	}
	if x < 0 || x >= geometry.Width || y < 0 || y >= geometry.Height {
		return 0, 0, fmt.Errorf("point (%d,%d) is outside screenshot %dx%d", x, y, geometry.Width, geometry.Height)
	}
	osX := geometry.Bounds.X + int(math.Round(float64(x)*float64(geometry.Bounds.Width)/float64(geometry.Width)))
	osY := geometry.Bounds.Y + int(math.Round(float64(y)*float64(geometry.Bounds.Height)/float64(geometry.Height)))
	osX = min(geometry.Bounds.X+geometry.Bounds.Width-1, max(geometry.Bounds.X, osX))
	osY = min(geometry.Bounds.Y+geometry.Bounds.Height-1, max(geometry.Bounds.Y, osY))
	return osX, osY, nil
}

func (r *Registry) captureDesktopForTool(ctx context.Context, source, detail string) (screen.Frame, string, error) {
	frame, err := screen.Default().Capture(ctx)
	if err != nil {
		return screen.Frame{}, "", err
	}
	path, err := persistDesktopFrame(ctx, frame)
	if err != nil {
		return screen.Frame{}, "", err
	}
	key := desktopSessionKey(ctx)
	if r.desktopFrames == nil {
		r.desktopFrames = make(map[string]screenGeometry)
	}
	pruneDesktopFrames(r.desktopFrames, frame.At.Add(-10*time.Minute))
	r.desktopFrames[key] = screenGeometry{
		Width:      frame.Width,
		Height:     frame.Height,
		Bounds:     frame.Bounds,
		CapturedAt: frame.At,
	}
	callID := toolCallIDFromContext(ctx)
	r.registerToolMedia(ctx, callID, path, source, source, "")
	if r.multimodalEnabled {
		modelJPEG, err := screen.AnnotateCoordinates(frame.JPEG)
		if err != nil {
			return frame, path, fmt.Errorf("annotate coordinate grid: %w", err)
		}
		gridStep := screen.CoordinateGridStep(frame.Width, frame.Height)
		prompt := fmt.Sprintf(
			"%s 已返回当前桌面。图像坐标系为 %dx%d，截图已叠加坐标网格，网格间距为 %d 像素，顶部和左侧数字表示截图像素坐标；后续 computer_use 的坐标必须直接使用这张图中的像素位置。原始虚拟桌面范围为 x=%d, y=%d, width=%d, height=%d。请根据图像继续任务。",
			source,
			frame.Width,
			frame.Height,
			gridStep,
			frame.Bounds.X,
			frame.Bounds.Y,
			frame.Bounds.Width,
			frame.Bounds.Height,
		)
		r.stashReadImageVision(callID, &ReadImageVisionPayload{
			RelPath: path,
			Detail:  detail,
			DataURL: "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(modelJPEG),
			Prompt:  prompt,
		})
	}
	return frame, path, nil
}

var desktopFilePartPattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func persistDesktopFrame(ctx context.Context, frame screen.Frame) (string, error) {
	session := desktopFilePartPattern.ReplaceAllString(desktopSessionKey(ctx), "-")
	if session == "" {
		session = "session"
	}
	root := filepath.Join(os.TempDir(), "dagents", "screenshots")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", fmt.Errorf("create screenshot root: %w", err)
	}
	cleanupOldDesktopFrameTree(root, time.Now().Add(-24*time.Hour))
	dir := filepath.Join(root, session)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create screenshot directory: %w", err)
	}
	callID := desktopFilePartPattern.ReplaceAllString(toolCallIDFromContext(ctx), "-")
	if callID == "" {
		callID = "capture"
	}
	name := fmt.Sprintf("%d-%s.jpg", time.Now().UTC().UnixNano(), callID)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, frame.JPEG, 0o600); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}
	return path, nil
}

func pruneDesktopFrames(frames map[string]screenGeometry, before time.Time) {
	for key, geometry := range frames {
		if geometry.CapturedAt.IsZero() || geometry.CapturedAt.Before(before) {
			delete(frames, key)
		}
	}
}

func cleanupOldDesktopFrameTree(root string, before time.Time) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		cleanupOldDesktopFrames(dir, before)
		_ = os.Remove(dir) // Remove only succeeds when the session directory is empty.
	}
}

func cleanupOldDesktopFrames(dir string, before time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(before) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
}

func desktopSessionKey(ctx context.Context) string {
	if id := strings.TrimSpace(SessionIDFromContext(ctx)); id != "" {
		return id
	}
	return "default"
}

func waitForDesktopPaint(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func coordinateGridMetadata(width, height int, enabled bool) map[string]any {
	metadata := map[string]any{
		"enabled": enabled,
		"origin":  "top_left",
	}
	if enabled {
		metadata["step"] = screen.CoordinateGridStep(width, height)
		metadata["labels"] = "screenshot_pixels"
	}
	return metadata
}

func desktopSuccess(toolName, action string, frame screen.Frame, path string, extra map[string]any) string {
	payload := map[string]any{
		"ok":              true,
		"status":          "succeeded",
		"tool":            toolName,
		"action":          action,
		"screenshot_path": path,
		"coordinate_space": map[string]any{
			"width":  frame.Width,
			"height": frame.Height,
		},
		"virtual_bounds": frame.Bounds,
	}
	for key, value := range extra {
		payload[key] = value
	}
	data, _ := json.Marshal(payload)
	return string(data)
}

func desktopFailure(toolName string, err error) string {
	payload := map[string]any{
		"ok":     false,
		"status": "failed",
		"tool":   toolName,
		"error":  strings.TrimSpace(fmt.Sprint(err)),
	}
	data, _ := json.Marshal(payload)
	return string(data)
}
