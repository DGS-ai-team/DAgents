package tools

import (
	"strings"
	"unicode/utf8"
)

// clipTextRunes 按 rune 数截断，避免切分多字节字符。
func clipTextRunes(s string, maxRunes int) (string, bool) {
	if maxRunes <= 0 || s == "" {
		return s, false
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s, false
	}
	var b strings.Builder
	b.Grow(len(s))
	n := 0
	for _, r := range s {
		if n >= maxRunes {
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String(), true
}
