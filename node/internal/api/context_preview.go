package api

// truncateContextPreview 按 rune 截断 context 消息预览，避免在多字节字符中间切断。
func truncateContextPreview(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	if maxRunes == 1 {
		return "…"
	}
	return string(r[:maxRunes-1]) + "…"
}
