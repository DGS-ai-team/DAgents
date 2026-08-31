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

// ExtractToolMediaPaths 从 tool 结果解析可展示图片路径（取第一条）。
func ExtractToolMediaPaths(toolName, content string, args map[string]any) (ToolMediaPath, bool) {
	all := ExtractAllToolMediaPaths(toolName, content, args)
	if len(all) == 0 {
		return ToolMediaPath{}, false
	}
	return all[0], true
}

// ExtractAllToolMediaPaths 解析工具结果中全部可展示图片路径。
func ExtractAllToolMediaPaths(toolName, content string, args map[string]any) []ToolMediaPath {
	name := strings.TrimSpace(toolName)
	switch name {
	case "show_image":
		if got, ok := extractPathToolMedia("show_image", "show_image", args, content, captionFromArgs(args)); ok {
			return []ToolMediaPath{got}
		}
		return nil
	case "read_image":
		if got, ok := extractPathToolMedia("read_image", "read_image", args, content, ""); ok {
			return []ToolMediaPath{got}
		}
		return nil
	case "screen_capture", "computer_use":
		paths := screenshotPathsFromContent(content)
		if len(paths) == 0 {
			return nil
		}
		return []ToolMediaPath{{
			RelPath: paths[len(paths)-1],
			Source:  "computer",
			Label:   name,
		}}
	default:
		if !strings.HasPrefix(name, "browser_") {
			return nil
		}
		paths := screenshotPathsFromContent(content)
		if len(paths) == 0 {
			return nil
		}
		out := make([]ToolMediaPath, 0, len(paths))
		for i, path := range paths {
			label := name
			if i > 0 {
				label = fmt.Sprintf("%s#%d", name, i+1)
			}
			out = append(out, ToolMediaPath{
				RelPath: path,
				Source:  "browser",
				Label:   label,
			})
		}
		return out
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

func screenshotPathsFromContent(content string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	add := func(raw any) {
		path := strings.TrimSpace(fmt.Sprint(raw))
		if path == "" || path == "<nil>" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	detail, _ := payload["detail"].(map[string]any)
	if detail != nil {
		appendPathList(&out, seen, detail["screenshot_paths"])
	}
	appendPathList(&out, seen, payload["screenshot_paths"])
	if detail != nil {
		if raw, ok := detail["last_screenshot_path"]; ok && raw != nil {
			add(raw)
		}
		if raw, ok := detail["screenshot_path"]; ok && raw != nil {
			add(raw)
		}
	}
	if raw, ok := payload["screenshot_path"]; ok && raw != nil {
		add(raw)
	}
	return out
}

func appendPathList(out *[]string, seen map[string]struct{}, raw any) {
	arr, ok := raw.([]any)
	if !ok {
		return
	}
	for _, item := range arr {
		path := strings.TrimSpace(fmt.Sprint(item))
		if path == "" || path == "<nil>" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		*out = append(*out, path)
	}
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
