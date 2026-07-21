package promptcontext

import (
	"strings"
)

// Content 为侧车 Markdown 与长期记忆正文（通常来自 SQLite）。
type Content struct {
	Soul     string
	User     string
	Custom   string
	LongTerm string
}

// NewContentReader 从内存正文构造 Reader（不读盘）。
func NewContentReader(c Content) *Reader {
	return &Reader{
		content: &c,
		cache:   make(map[string]cachedFile),
	}
}

// SetContent 切换为内存正文源（清空文件缓存）。
func (r *Reader) SetContent(c Content) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := c
	r.content = &cp
	r.cache = make(map[string]cachedFile)
}

func (r *Reader) readContentField(kind string) string {
	if r == nil || r.content == nil {
		return ""
	}
	var text string
	switch kind {
	case soulFile:
		text = r.content.Soul
	case userFile:
		text = r.content.User
	case customFile:
		text = r.content.Custom
	case "long_term":
		text = r.content.LongTerm
	}
	return strings.TrimSpace(text)
}
