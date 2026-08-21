package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const showImageResultPrefix = "[SHOW_IMAGE]"

type showImageArgs struct {
	Path    string `json:"path"`
	Caption string `json:"caption"`
}

func showImageToolDef() ToolDef {
	return ToolDef{
		Type: "function",
		Function: FunctionDef{
			Name: "show_image",
			Description: "向用户界面展示已有图片（缩略图 + 可选说明）。" +
				" 支持.jpg/.jpeg/.png/.gif/.webp，单文件最大 10MB。",
			Parameters: injectCallPurposeParam(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{
						"type":        "string",
						"description": "图片路径（必填）",
					},
					"caption": map[string]any{
						"type":        "string",
						"description": "可选说明文字，显示在缩略图旁",
					},
				},
				"required":             []string{"path"},
				"additionalProperties": false,
			}),
		},
	}
}

func (r *Registry) execShowImage(ctx context.Context, raw json.RawMessage) (string, error) {
	var args showImageArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	relPath := strings.TrimSpace(args.Path)
	if relPath == "" {
		return formatShowImageError("path 不能为空"), nil
	}
	absPath, err := r.resolvePath(relPath)
	if err != nil {
		return formatShowImageError(err.Error()), nil
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return formatShowImageError(fmt.Sprintf("文件不存在：%q", relPath)), nil
		}
		return formatShowImageError(err.Error()), nil
	}
	if info.IsDir() {
		return formatShowImageError(fmt.Sprintf("目标是目录：%q", relPath)), nil
	}
	mime, err := imageMIMEForPath(absPath)
	if err != nil {
		return formatShowImageError(err.Error()), nil
	}
	if info.Size() <= 0 || info.Size() > readImageMaxBytes {
		return formatShowImageError(fmt.Sprintf("图片过大或为空（max %d bytes）", readImageMaxBytes)), nil
	}
	caption := strings.TrimSpace(args.Caption)
	r.registerToolMedia(ctx, toolCallIDFromContext(ctx), relPath, "show_image", "show_image", caption)
	displayPath := relPath
	if filepath.IsAbs(relPath) {
		displayPath = filepath.ToSlash(absPath)
	}
	return formatShowImageSuccess(displayPath, mime, int(info.Size()), caption), nil
}

func formatShowImageSuccess(relPath, mime string, bytes int, caption string) string {
	lines := []string{
		showImageResultPrefix,
		fmt.Sprintf("path=%s", relPath),
		fmt.Sprintf("mime=%s", mime),
		fmt.Sprintf("bytes=%d", bytes),
		"status=ok",
	}
	if caption != "" {
		lines = append(lines, fmt.Sprintf("caption=%s", caption))
	}
	lines = append(lines, "图片已注册，将在对话区展示。")
	return strings.Join(lines, "\n")
}

func formatShowImageError(msg string) string {
	return strings.Join([]string{
		showImageResultPrefix,
		"status=error",
		"ERROR: " + strings.TrimSpace(msg),
	}, "\n")
}

// IsShowImageTool 判断工具名是否为 show_image。
func IsShowImageTool(name string) bool {
	return strings.TrimSpace(name) == "show_image"
}
