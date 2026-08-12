// Package promptcontext 读取侧车 Markdown 与长期记忆正文（权威来源为 SQLite，经 Content 注入）。
package promptcontext

import (
	"strings"
	"sync"
)

const (
	soulField   = "soul"
	customField = "custom"
)

// Filter 控制侧车 / 长期记忆是否注入；nil 指针表示默认启用。
// UserEnabled 已废弃（用户称呼改走 PreferredName），保留字段兼容旧调用方。
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
// 用户称呼来自 Node 配置 PreferredName，不再使用 user.md 侧车正文。
type Reader struct {
	content        *Content
	filter         Filter
	preferredName  string
	mu             sync.Mutex
}

// NewReader 构造 Reader；runtimeDir 参数保留兼容，不再读盘。
func NewReader(_ string) *Reader {
	return &Reader{}
}

// SetFilter 设置侧车注入开关（缺省全开）。UserEnabled 已忽略（用户信息改走 PreferredName）。
func (r *Reader) SetFilter(f Filter) {
	if r == nil {
		return
	}
	r.filter = f
}

// SetPreferredName 设置本机使用者称呼（Node 首配 / 通用设置）。
func (r *Reader) SetPreferredName(name string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.preferredName = strings.TrimSpace(name)
	r.mu.Unlock()
}

// ReadSoul 读取 soul；空白或缺失返回空串。
func (r *Reader) ReadSoul() string {
	if !flagOrDefault(r.filter.SoulEnabled, true) {
		return ""
	}
	return r.readContentField(soulField)
}

// ReadUser 已废弃：用户信息改由 PreferredName 注入；保留空实现以免旧调用方崩溃。
func (r *Reader) ReadUser() string {
	return ""
}

// PreferredName 返回本机使用者称呼。
func (r *Reader) PreferredName() string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.preferredName
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

// UpdateLongTerm 更新内存中的长期记忆正文；调用方应在上下文重建边界使用它。
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

// BuildStableContextSections 拼接 soul / 用户称呼 / long_term 段落（较稳定上下文）。
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
	if name := r.PreferredName(); name != "" {
		b.WriteString("\n\n## 以下是用户信息：\n\n")
		b.WriteString("请称呼用户为：")
		b.WriteString(name)
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
	case customField:
		text = c.Custom
	case "long_term":
		text = c.LongTerm
	}
	return strings.TrimSpace(text)
}
