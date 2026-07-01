package tools

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

func (r *Registry) stashBrowserVisionFromScreenshot(ctx context.Context, relPath, toolName string) {
	if r == nil || !r.multimodalEnabled {
		return
	}
	relPath = strings.TrimSpace(relPath)
	if relPath == "" {
		return
	}
	payload, err := r.buildVisionPayloadFromPath(relPath, "auto", browserVisionPrompt(toolName, relPath))
	if err != nil {
		return
	}
	r.stashReadImageVision(toolCallIDFromContext(ctx), payload)
}

func browserVisionPrompt(toolName, relPath string) string {
	switch strings.TrimSpace(toolName) {
	case "browser_snapshot":
		return fmt.Sprintf("browser_snapshot 已附带页面截图 %q。请根据截图判断页面布局与可点击区域；优先用 browser_click_coordinate(x,y) 操作，index 作辅助。", relPath)
	case "browser_screenshot":
		return fmt.Sprintf("browser_screenshot 已保存 %q 并注入视觉上下文。请根据截图继续浏览器任务。", relPath)
	case "browser_navigate":
		return fmt.Sprintf("browser_navigate 后已附带页面截图 %q。请根据截图继续操作。", relPath)
	default:
		return fmt.Sprintf("浏览器截图 %q 已加载，请根据图像内容继续任务。", relPath)
	}
}

func (r *Registry) buildVisionPayloadFromPath(relPath, detail, prompt string) (*ReadImageVisionPayload, error) {
	absPath, err := r.resolvePath(relPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("目标是目录：%q", relPath)
	}
	mime, err := imageMIMEForPath(absPath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	if len(data) > readImageMaxBytes {
		return nil, fmt.Errorf("图片过大（max %d bytes）", readImageMaxBytes)
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		detail = "auto"
	}
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	return &ReadImageVisionPayload{
		RelPath: relPath,
		Detail:  detail,
		DataURL: dataURL,
		Prompt:  strings.TrimSpace(prompt),
	}, nil
}
