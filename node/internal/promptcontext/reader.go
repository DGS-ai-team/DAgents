// Package promptcontext 读取 Agent 的提示侧车正文（权威来源为 SQLite，经 Content 注入）。
package promptcontext

import (
	"strings"
	"sync"
)

const (
	soulField   = "soul"
	customField = "custom"
)

// Filter 控制侧车正文是否注入；nil 指针表示默认启用。
type Filter struct {
	SoulEnabled   *bool
	CustomEnabled *bool
}

func flagOrDefault(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}

// Reader 从内存 Content 读取提示侧车正文（由 agents.db 在 runtime 启动时注入）。
// 用户称呼来自 Node 配置 PreferredName。
type Reader struct {
	content       *Content
	filter        Filter
	preferredName string
	mu            sync.RWMutex
}

// NewReader 构造一个以内存 Content 为数据源的 Reader。
func NewReader() *Reader {
	return &Reader{}
}

// SetFilter 设置侧车注入开关（缺省全开）。
func (r *Reader) SetFilter(f Filter) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.filter = f
	r.mu.Unlock()
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
	r.mu.RLock()
	enabled := flagOrDefault(r.filter.SoulEnabled, true)
	r.mu.RUnlock()
	if !enabled {
		return ""
	}
	return r.readContentField(soulField)
}

// PreferredName 返回本机使用者称呼。
func (r *Reader) PreferredName() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.preferredName
}

// ReadCustom 读取 custom。
func (r *Reader) ReadCustom() string {
	r.mu.RLock()
	enabled := flagOrDefault(r.filter.CustomEnabled, true)
	r.mu.RUnlock()
	if !enabled {
		return ""
	}
	return r.readContentField(customField)
}

// BuildStableContextSections 拼接 soul / 用户称呼段落（较稳定上下文）。
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
	r.mu.RLock()
	c := *r.content
	r.mu.RUnlock()
	var text string
	switch kind {
	case soulField:
		text = c.Soul
	case customField:
		text = c.Custom
	}
	return strings.TrimSpace(text)
}
