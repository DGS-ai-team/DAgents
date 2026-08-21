package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const readImageMaxBytes = 10 << 20 // 与 llm.MaxImageBytes 一致

const readImageResultPrefix = "[READ_IMAGE]"

var imageFileSuffixes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

type readImageArgs struct {
	Path   string  `json:"path"`
	Detail *string `json:"detail"`
}

func readImageToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "read_image",
			Description: "读取图片并附加到下一次模型输入，供视觉模型分析。" +
				" 支持 .jpg/.jpeg/.png/.gif/.webp，单文件最大 10MB；仅在启用多模态时可用。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "图片路径（必填）",
					},
					"detail": map[string]any{
						"type":        "string",
						"enum":        []string{"auto", "low", "high"},
						"description": "OpenAI image_url detail，默认 auto",
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execReadImage(ctx context.Context, raw json.RawMessage) (string, error) {
	if !r.multimodalEnabled {
		return formatReadImageError("多模态未启用（config multimodal.enabled）"), nil
	}
	var args readImageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	relPath := strings.TrimSpace(args.Path)
	if relPath == "" {
		return formatReadImageError("path 不能为空"), nil
	}
	absPath, err := r.resolvePath(relPath)
	if err != nil {
		return formatReadImageError(err.Error()), nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return formatReadImageError(fmt.Sprintf("文件不存在：%q", relPath)), nil
		}
		return formatReadImageError(err.Error()), nil
	}
	if info.IsDir() {
		return formatReadImageError(fmt.Sprintf("目标是目录：%q", relPath)), nil
	}
	mime, err := imageMIMEForPath(absPath)
	if err != nil {
		return formatReadImageError(err.Error()), nil
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return formatReadImageError(err.Error()), nil
	}
	if len(data) > readImageMaxBytes {
		return formatReadImageError(fmt.Sprintf("图片过大（max %d bytes）", readImageMaxBytes)), nil
	}
	detail := "auto"
	if args.Detail != nil {
		d := strings.ToLower(strings.TrimSpace(*args.Detail))
		switch d {
		case "auto", "low", "high":
			detail = d
		default:
			return formatReadImageError(fmt.Sprintf("invalid detail %q", *args.Detail)), nil
		}
	}
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	r.registerToolMedia(ctx, toolCallIDFromContext(ctx), relPath, "read_image", "read_image", "")
	r.stashReadImageVision(toolCallIDFromContext(ctx), &ReadImageVisionPayload{
		RelPath: relPath,
		Detail:  detail,
		DataURL: dataURL,
	})
	return formatReadImageSuccess(relPath, mime, len(data), detail), nil
}

func imageMIMEForPath(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	mime, ok := imageFileSuffixes[ext]
	if !ok {
		if ext == "" {
			return "", fmt.Errorf("不支持读取该后缀文件：<no-suffix>")
		}
		return "", fmt.Errorf("不支持读取该后缀文件：%s", ext)
	}
	return mime, nil
}

func formatReadImageSuccess(relPath, mime string, bytes int, detail string) string {
	return strings.Join([]string{
		readImageResultPrefix,
		fmt.Sprintf("path=%s", relPath),
		fmt.Sprintf("mime=%s", mime),
		fmt.Sprintf("bytes=%d", bytes),
		fmt.Sprintf("detail=%s", detail),
		"status=ok",
		"图像已加载；下一则 user 消息将附带 image_url 供视觉模型分析。",
	}, "\n")
}

func formatReadImageError(msg string) string {
	return strings.Join([]string{
		readImageResultPrefix,
		"status=error",
		"ERROR: " + strings.TrimSpace(msg),
	}, "\n")
}

// IsReadImageTool 判断工具名是否为 read_image。
func IsReadImageTool(name string) bool {
	return strings.TrimSpace(name) == "read_image"
}
