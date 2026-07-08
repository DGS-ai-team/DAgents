package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolMediaPath 描述工具结果中可注册为 UI media 的图片路径（F-M2/M4）。
type ToolMediaPath struct {
	RelPath string
	Source  string
	Label   string
	Caption string
}

// ExtractToolMediaPaths 从 tool 结果解析可展示图片路径。
func ExtractToolMediaPaths(toolName, content string, args map[string]any) (ToolMediaPath, bool) {
	name := strings.TrimSpace(toolName)
	switch name {
	case "show_image":
		return extractPathToolMedia("show_image", "show_image", args, content, captionFromArgs(args))
	case "read_image":
		return extractPathToolMedia("read_image", "read_image", args, content, "")
	default:
		if !strings.HasPrefix(name, "browser_") {
			return ToolMediaPath{}, false
		}
		if path := screenshotPathFromContent(content); path != "" {
			return ToolMediaPath{
				RelPath: path,
				Source:  "browser",
				Label:   name,
			}, true
		}
		return ToolMediaPath{}, false
	}
}

func extractPathToolMedia(source, label string, args map[string]any, content, caption string) (ToolMediaPath, bool) {
	path := pathFromArgs(args)
	if path == "" {
		path = pathFromPrefixedContent(content)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return ToolMediaPath{}, false
	}
	return ToolMediaPath{
		RelPath: path,
		Source:  source,
		Label:   label,
		Caption: strings.TrimSpace(caption),
	}, true
}

func pathFromArgs(args map[string]any) string {
	if args == nil {
		return ""
	}
	if p := argString(args, "path"); p != "" {
		return p
	}
	return argString(args, "file_path")
}

func captionFromArgs(args map[string]any) string {
	return argString(args, "caption")
}

func argString(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func pathFromPrefixedContent(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "path=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "path="))
		}
	}
	return ""
}

func screenshotPathFromContent(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return ""
	}
	raw, ok := payload["screenshot_path"]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

// ParseToolArgumentsMap 解析 tool call arguments JSON。
func ParseToolArgumentsMap(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil || len(out) == 0 {
		return nil
	}
	return out
}
