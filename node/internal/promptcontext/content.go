package promptcontext

// Content 为侧车 Markdown 正文（通常来自 SQLite）。
type Content struct {
	Soul   string
	Custom string
}

// NewContentReader 从内存正文构造 Reader。
func NewContentReader(c Content) *Reader {
	r := NewReader()
	cp := c
	r.content = &cp
	return r
}

// SetContent 切换为内存正文源。
func (r *Reader) SetContent(c Content) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := c
	r.content = &cp
}
