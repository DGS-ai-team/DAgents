// Package promptcontext 读取侧车 Markdown 与长期记忆正文（权威来源为 SQLite，经 Content 注入）。
package promptcontext

import (
	"strings"
	"sync"
)

const (
	soulField   = "soul"
	userField   = "user"
	customField = "custom"
)

// Filter 控制侧车 / 长期记忆是否注入；nil 指针表示默认启用。
type Filter struct {
	SoulEnabled     *bool
	UserEnabled     *bool
	CustomEnabled   *bool
	LongTermEnabled *bool
}

func flagOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// Reader 从内存 Content 读取侧车与长期记忆（由 agents.db 在 runtime 启动时注入）。
type Reader struct {
	content *Content
	filter  Filter
	mu      sync.Mutex
}

// NewReader 构造 Reader；runtimeDir 参数保留兼容，不再读盘。
func NewReader(_ string) *Reader {
	return &Reader{}
}

// SetFilter 设置侧车注入开关（缺省全开）。
func (r *Reader) SetFilter(f Filter) {
	if r == nil {
		return
	}
	r.filter = f
}

// ReadSoul 读取 soul；空白或缺失返回空串。
func (r *Reader) ReadSoul() string {
	if !flagOrDefault(r.filter.SoulEnabled, true) {
		return ""
	}
	return r.readContentField(soulField)
}

// ReadUser 读取 user。
func (r *Reader) ReadUser() string {
	if !flagOrDefault(r.filter.UserEnabled, true) {
		return ""
	}
	return r.readContentField(userField)
}

// ReadCustom 读取 custom。
func (r *Reader) ReadCustom() string {
	if !flagOrDefault(r.filter.CustomEnabled, true) {
		return ""
	}
	return r.readContentField(customField)
}

// ReadLongTermMemory 读取长期记忆（不存在或空白时不注入）。
func (r *Reader) ReadLongTermMemory() string {
	if !flagOrDefault(r.filter.LongTermEnabled, true) {
		return ""
	}
	return r.readContentField("long_term")
}

// UpdateLongTerm 更新内存中的长期记忆正文（remember 写入后同步注入，非 DB 重载）。
func (r *Reader) UpdateLongTerm(text string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.content == nil {
		r.content = &Content{}
	}
	r.content.LongTerm = strings.TrimSpace(text)
}

// BuildStableContextSections 拼接 soul / user / long_term 段落（较稳定上下文）。
func (r *Reader) BuildStableContextSections() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	if soul := r.ReadSoul(); soul != "" {
		b.WriteString("\n\n## 以下是你的设定：\n\n")
		b.WriteString(soul)
		b.WriteByte('\n')
	}
	if user := r.ReadUser(); user != "" {
		b.WriteString("\n\n## 以下是用户信息与偏好：\n\n")
		b.WriteString(user)
		b.WriteByte('\n')
	}
	if mem := r.ReadLongTermMemory(); mem != "" {
		b.WriteString("\n\n## 以下是长期记忆：\n\n")
		b.WriteString(mem)
		b.WriteByte('\n')
	}
	return b.String()
}

// BuildCustomSection 拼接 custom 临时/专项指令段。
func (r *Reader) BuildCustomSection() string {
	if r == nil {
		return ""
	}
	custom := r.ReadCustom()
	if custom == "" {
		return ""
	}
	return "\n\n## 以下是用户侧追加的临时/专项指令：\n\n" + custom + "\n"
}

func (r *Reader) readContentField(kind string) string {
	if r == nil || r.content == nil {
		return ""
	}
	r.mu.Lock()
	c := *r.content
	r.mu.Unlock()
	var text string
	switch kind {
	case soulField:
		text = c.Soul
	case userField:
		text = c.User
	case customField:
		text = c.Custom
	case "long_term":
		text = c.LongTerm
	}
	return strings.TrimSpace(text)
}
