package shared

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MediaHintLines 从 tool_result / hydrate 数据提取可展示的图片 path 或 URL（F-M8）。
func MediaHintLines(data map[string]any) []string {
	if data == nil {
		return nil
	}
	seen := make(map[string]struct{})
	var lines []string
	for _, item := range mediaItemsFromData(data) {
		if line := formatMediaItemHint(item); line != "" {
			if _, ok := seen[line]; ok {
				continue
			}
			seen[line] = struct{}{}
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		return lines
	}
	name := toolNameFromEventData(data)
	content := toolContentFromEventData(data)
	if path := showImagePathHint(name, content, data); path != "" {
		return []string{"image path: " + path}
	}
	return nil
}

// UserMediaHintLines 从 hydrate user 条目提取图片 URL（F-M8）。
func UserMediaHintLines(entry map[string]any) []string {
	if entry == nil {
		return nil
	}
	data := map[string]any{"media": entry["media"], "images": entry["images"]}
	return MediaHintLines(data)
}

func mediaItemsFromData(data map[string]any) []map[string]any {
	raw, ok := data["media"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok || m == nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func formatMediaItemHint(item map[string]any) string {
	url := strings.TrimSpace(fmt.Sprint(item["url"]))
	if url == "" || url == "<nil>" {
		return ""
	}
	label := strings.TrimSpace(fmt.Sprint(item["label"]))
	if label == "" || label == "<nil>" {
		label = "image"
	}
	caption := strings.TrimSpace(fmt.Sprint(item["caption"]))
	if caption != "" && caption != "<nil>" {
		return label + ": " + url + " (" + caption + ")"
	}
	return label + ": " + url
}

func showImagePathHint(toolName, content string, data map[string]any) string {
	name := strings.TrimSpace(toolName)
	if name != "show_image" && name != "read_image" {
		if strings.HasPrefix(name, "browser_") {
			return screenshotPathFromJSON(content)
		}
		return ""
	}
	if path := pathFromToolArgs(data); path != "" {
		return path
	}
	return pathFromPrefixedToolContent(content)
}

func toolNameFromEventData(data map[string]any) string {
	if name := strings.TrimSpace(fmt.Sprint(data["tool_name"])); name != "" && name != "<nil>" {
		return name
	}
	return strings.TrimSpace(fmt.Sprint(data["name"]))
}

func toolContentFromEventData(data map[string]any) string {
	content := strings.TrimSpace(fmt.Sprint(data["content"]))
	if content != "" && content != "<nil>" {
		return content
	}
	return strings.TrimSpace(fmt.Sprint(data["output"]))
}

func pathFromToolArgs(data map[string]any) string {
	for _, key := range []string{"arguments", "args"} {
		raw, ok := data[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case map[string]any:
			if p := argStringMap(v, "path"); p != "" {
				return p
			}
			if p := argStringMap(v, "file_path"); p != "" {
				return p
			}
		case string:
			var args map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(v)), &args) == nil {
				if p := argStringMap(args, "path"); p != "" {
					return p
				}
			}
		}
	}
	return ""
}

func argStringMap(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func pathFromPrefixedToolContent(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "path=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "path="))
		}
	}
	return ""
}

func screenshotPathFromJSON(content string) string {
	content = strings.TrimSpace(content)
	if content == "" || content[0] != '{' {
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

func appendMediaHintLines(lines []string, data map[string]any, blockID string) []string {
	for _, hint := range MediaHintLines(data) {
		if blockID != "" {
			lines = append(lines, formatToolMetaLine(toolDetailLinePrefix, blockID, hint))
		} else {
			lines = append(lines, indentLines("    ", hint)...)
		}
	}
	return lines
}
