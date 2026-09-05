package tools

import (
	"context"
	"crypto/sha256"
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

type computerUseActionArgs struct {
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

type computerUseArgs struct {
	Action    string                  `json:"action"`
	X         *int                    `json:"x"`
	Y         *int                    `json:"y"`
	FromX     *int                    `json:"from_x"`
	FromY     *int                    `json:"from_y"`
	Button    string                  `json:"button"`
	ScrollY   int                     `json:"scroll_y"`
	Key       string                  `json:"key"`
	Modifiers []string                `json:"modifiers"`
	Text      string                  `json:"text"`
	FrameID   string                  `json:"frame_id"`
	Actions   []computerUseActionArgs `json:"actions"`
}

type screenGeometry struct {
	Width      int
	Height     int
	Bounds     screen.Bounds
	CapturedAt time.Time
	FrameID    string
}

type desktopCaptureInfo struct {
	FrameID string
	Stable  bool
}

const maxComputerUseBatchActions = 4

func screenCaptureToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "screen_capture",
			Description: "截取当前主机的真实虚拟桌面；启用多模态时会把图像附加到下一次模型输入，否则仍作为工具结果图片展示。" +
				" 启用多模态后收到的截图会叠加像素坐标网格，顶部和左侧数字表示截图坐标；返回截图像素尺寸与操作坐标系，computer_use 的坐标必须基于最近一次截图。" +
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
	properties := computerUseActionProperties()
	topProperties := make(map[string]any, len(properties)+2)
	for key, value := range properties {
		topProperties[key] = value
	}
	topProperties["frame_id"] = map[string]any{
		"type":        "string",
		"description": "可选。screen_capture 返回的截图帧 ID，用于防止坐标落在过期画面上",
	}
	topProperties["actions"] = map[string]any{
		"type":        "array",
		"minItems":    1,
		"maxItems":    maxComputerUseBatchActions,
		"description": "最多 4 步的短动作序列；每一步使用与单次 action 相同的字段，坐标动作应放在序列首步",
		"items": map[string]any{
			"type":                 "object",
			"properties":           properties,
			"required":             []string{"action"},
			"additionalProperties": false,
		},
	}
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "computer_use",
			Description: "在当前主机桌面执行一次鼠标或键盘动作，或执行一个受限的短动作序列，并自动返回动作后的屏幕截图。" +
				" 调用坐标动作前必须先调用 screen_capture；x/y 与 from_x/from_y 使用最近截图返回的像素坐标系，不是操作系统原始坐标。" +
				" 截图中的坐标网格可用于定位目标，顶部和左侧的数字是截图像素坐标。" +
				" action 支持 move、click、double_click、drag、scroll、key、type_text。" +
				" action 与 actions 二选一；actions 最多 4 步，所有坐标都必须基于同一 frame_id，且一个序列最多包含一个 move/click/double_click/drag/scroll 坐标动作，该坐标动作必须是首步。" +
				" 如果后续坐标依赖动作后的新画面，应拆分为新的 computer_use 调用；整个序列只在末尾返回截图并作为一个审批单元。" +
				" scroll_y 为 -20..20，正数向下、负数向上；key 可配 modifiers（ctrl/alt/shift/meta）。" +
				" 桌面操作默认进入审批，不得用于输入密码、验证码、支付信息或绕过系统安全提示。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type":                 "object",
				"properties":           topProperties,
				"additionalProperties": false,
			}),
		},
	}
}

func computerUseActionProperties() map[string]any {
	return map[string]any{
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
	frame, path, captureInfo, err := r.captureDesktopForTool(ctx, "screen_capture", detail)
	if err != nil {
		return desktopFailure("screen_capture", err), err
	}
	return desktopSuccess("screen_capture", "capture", frame, path, map[string]any{
		"backend":         screen.Default().Backend(),
		"vision_attached": r.multimodalEnabled,
		"coordinate_grid": coordinateGridMetadata(frame.Width, frame.Height, r.multimodalEnabled),
		"frame_id":        captureInfo.FrameID,
		"frame_stable":    captureInfo.Stable,
	}), nil
}

func (r *Registry) execComputerUse(ctx context.Context, raw json.RawMessage) (string, error) {
	var args computerUseArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if !r.multimodalEnabled {
		err := fmt.Errorf("computer_use requires multimodal to be enabled so it can inspect each screenshot")
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
	actions := args.Actions
	if len(actions) > maxComputerUseBatchActions {
		err := fmt.Errorf("computer_use supports at most %d actions per call", maxComputerUseBatchActions)
		return desktopFailure("computer_use", err), err
	}
	if len(actions) > 0 && strings.TrimSpace(args.Action) != "" {
		err := fmt.Errorf("action and actions are mutually exclusive")
		return desktopFailure("computer_use", err), err
	}
	if len(actions) == 0 {
		actions = []computerUseActionArgs{args.singleAction()}
	}

	resolved := make([]computeruse.Action, 0, len(actions))
	for idx, actionArgs := range actions {
		action, err := r.resolveComputerActionValues(ctx, actionArgs, args.FrameID)
		if err != nil {
			if len(actions) > 1 {
				err = fmt.Errorf("invalid action %d: %w", idx+1, err)
			}
			return desktopFailure("computer_use", err), err
		}
		resolved = append(resolved, action)
	}
	if len(resolved) > 1 {
		if err := validateComputerUseBatch(resolved); err != nil {
			return desktopFailure("computer_use", err), err
		}
	}
	for idx, action := range resolved {
		if idx > 0 {
			if err := waitForDesktopPaint(ctx, 40*time.Millisecond); err != nil {
				return desktopFailure("computer_use", err), err
			}
		}
		if err := backend.Execute(ctx, action); err != nil {
			return desktopFailure("computer_use", fmt.Errorf("action %d (%s): %w", idx+1, action.Name, err)), err
		}
	}
	frame, path, captureInfo, err := r.captureDesktopForTool(ctx, "computer_use", "high")
	if err != nil {
		return desktopFailure("computer_use", fmt.Errorf("action completed but follow-up screenshot failed: %w", err)), err
	}
	extra := map[string]any{
		"backend":         status.Backend,
		"vision_attached": r.multimodalEnabled,
		"coordinate_grid": coordinateGridMetadata(frame.Width, frame.Height, r.multimodalEnabled),
		"frame_id":        captureInfo.FrameID,
		"frame_stable":    captureInfo.Stable,
		"action_count":    len(resolved),
	}
	if strings.TrimSpace(args.FrameID) != "" {
		extra["input_frame_id"] = strings.TrimSpace(args.FrameID)
	}
	if len(resolved) == 1 && resolved[0].HasPoint {
		extra["os_point"] = map[string]any{"x": resolved[0].X, "y": resolved[0].Y}
	} else if len(resolved) > 1 {
		names := make([]string, 0, len(resolved))
		for _, action := range resolved {
			names = append(names, action.Name)
		}
		extra["actions"] = names
	}
	actionName := resolved[0].Name
	if len(resolved) > 1 {
		actionName = "batch"
	}
	return desktopSuccess("computer_use", actionName, frame, path, extra), nil
}

func (args computerUseArgs) singleAction() computerUseActionArgs {
	return computerUseActionArgs{
		Action:    args.Action,
		X:         args.X,
		Y:         args.Y,
		FromX:     args.FromX,
		FromY:     args.FromY,
		Button:    args.Button,
		ScrollY:   args.ScrollY,
		Key:       args.Key,
		Modifiers: args.Modifiers,
		Text:      args.Text,
	}
}

func (r *Registry) resolveComputerActionValues(ctx context.Context, args computerUseActionArgs, frameID string) (computeruse.Action, error) {
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
	requestedFrameID := strings.TrimSpace(frameID)
	if requestedFrameID != "" && requestedFrameID != geometry.FrameID {
		return action, fmt.Errorf("frame_id %q does not match the latest screenshot %q; call screen_capture again", requestedFrameID, geometry.FrameID)
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

func validateComputerUseBatch(actions []computeruse.Action) error {
	if len(actions) < 2 {
		return nil
	}
	visualIndex := -1
	for idx, action := range actions {
		switch action.Name {
		case computeruse.ActionMove, computeruse.ActionClick, computeruse.ActionDoubleClick, computeruse.ActionDrag, computeruse.ActionScroll:
			if visualIndex >= 0 {
				return fmt.Errorf("computer_use action batch may contain at most one coordinate-dependent mouse/scroll action; split the call after the screen changes")
			}
			if idx > 0 {
				return fmt.Errorf("computer_use coordinate-dependent action must be first in a batch; split the call after the screen changes")
			}
			visualIndex = idx
		}
	}
	return nil
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

func (r *Registry) captureDesktopForTool(ctx context.Context, source, detail string) (screen.Frame, string, desktopCaptureInfo, error) {
	frame, stable, err := captureStableDesktop(ctx)
	if err != nil {
		return screen.Frame{}, "", desktopCaptureInfo{}, err
	}
	path, err := r.persistDesktopFrame(ctx, frame)
	if err != nil {
		return screen.Frame{}, "", desktopCaptureInfo{}, err
	}
	frameID := desktopFrameID(frame)
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
		FrameID:    frameID,
	}
	callID := toolCallIDFromContext(ctx)
	r.registerToolMedia(ctx, callID, path, source, source, "")
	if r.multimodalEnabled {
		modelJPEG, err := screen.AnnotateCoordinates(frame.JPEG)
		if err != nil {
			return frame, path, desktopCaptureInfo{FrameID: frameID, Stable: stable}, fmt.Errorf("annotate coordinate grid: %w", err)
		}
		gridStep := screen.CoordinateGridStep(frame.Width, frame.Height)
		prompt := fmt.Sprintf(
			"%s 已返回当前桌面。frame_id=%s。图像坐标系为 %dx%d，截图已叠加坐标网格，网格间距为 %d 像素，顶部和左侧数字表示截图像素坐标；后续 computer_use 的坐标必须直接使用这张图中的像素位置，并回传该 frame_id。原始虚拟桌面范围为 x=%d, y=%d, width=%d, height=%d。请根据图像继续任务。",
			source,
			frameID,
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
			FrameID: frameID,
		})
	}
	return frame, path, desktopCaptureInfo{FrameID: frameID, Stable: stable}, nil
}

func captureStableDesktop(ctx context.Context) (screen.Frame, bool, error) {
	frame, err := screen.Default().Capture(ctx)
	if err != nil {
		return screen.Frame{}, false, err
	}
	previous := sha256.Sum256(frame.JPEG)
	stable := false
	// A single immediate capture can land between two UI paints. Take a few
	// short-spaced samples and use the first identical pair as the settled
	// frame. The bounded loop keeps the latency close to the old fixed wait.
	for i := 0; i < 3; i++ {
		if err := waitForDesktopPaint(ctx, 60*time.Millisecond); err != nil {
			return screen.Frame{}, false, err
		}
		candidate, err := screen.Default().Capture(ctx)
		if err != nil {
			return screen.Frame{}, false, err
		}
		current := sha256.Sum256(candidate.JPEG)
		frame = candidate
		if current == previous {
			stable = true
			break
		}
		previous = current
	}
	return frame, stable, nil
}

func desktopFrameID(frame screen.Frame) string {
	sum := sha256.Sum256(frame.JPEG)
	return fmt.Sprintf("frame-%x", sum[:8])
}

var desktopFilePartPattern = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func (r *Registry) persistDesktopFrame(ctx context.Context, frame screen.Frame) (string, error) {
	session := desktopFilePartPattern.ReplaceAllString(desktopSessionKey(ctx), "-")
	if session == "" {
		session = "session"
	}
	if r == nil || strings.TrimSpace(r.workspaceRoot) == "" {
		return "", fmt.Errorf("workspace root is required for screenshot persistence")
	}
	owner := desktopFilePartPattern.ReplaceAllString(strings.TrimSpace(r.agentID), "-")
	if owner == "" {
		owner = session
	}
	root := filepath.Join(r.workspaceRoot, ".dagents", owner, "screenshots")
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
