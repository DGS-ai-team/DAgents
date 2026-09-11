package media

import "strings"

const refScheme = "dagents-media://"

// RefURL 返回持久化消息内使用的 media 引用 URL（F-M5）。
func RefURL(mediaID string) string {
	id := strings.TrimSpace(mediaID)
	if id == "" {
		return ""
	}
	return refScheme + id
}

// ParseRefURL 解析 dagents-media:// 引用。
func ParseRefURL(raw string) (mediaID string, ok bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(raw), refScheme) {
		return "", false
	}
	id := strings.TrimSpace(raw[len(refScheme):])
	if id == "" {
		return "", false
	}
	return id, true
}

// IsRefURL 判断是否为 dagents-media 引用。
func IsRefURL(raw string) bool {
	_, ok := ParseRefURL(raw)
	return ok
}

// ParsePublicMediaURL 从 GET API 路径解析 media id（/v1/agents/{id}/media/{id}）。
func ParsePublicMediaURL(raw string) (mediaID string, ok bool) {
	raw = strings.TrimSpace(raw)
	const needle = "/media/"
	idx := strings.LastIndex(raw, needle)
	if idx < 0 {
		return "", false
	}
	id := strings.TrimSpace(raw[idx+len(needle):])
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// UserUploadRelPath 返回用户上传图片相对 workspace 的路径。
func UserUploadRelPath(sessionID, mediaID, ext string) string {
	sessionID = strings.TrimSpace(sessionID)
	mediaID = strings.TrimSpace(mediaID)
	ext = strings.TrimSpace(ext)
	if ext != "" && !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return "media/" + sessionID + "/" + mediaID + ext
}
