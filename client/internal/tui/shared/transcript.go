// Package shared 提供 REPL 与全屏 TUI 共用的 transcript / tool 格式化。
package shared

import (
	"fmt"
	"strings"
	"sync"
)

const defaultTranscriptCap = 2000

// Transcript 保存可回溯的终端输出行（供 REPL /history 或全屏 viewport）。
type Transcript struct {
	mu      sync.Mutex
	lines   []string
	cap     int
	partial *partialEntry
}

type partialEntry struct {
	role string
	buf  strings.Builder
}

// NewTranscript 创建 transcript；cap≤0 时用默认上限。
func NewTranscript(cap int) *Transcript {
	if cap <= 0 {
		cap = defaultTranscriptCap
	}
	return &Transcript{cap: cap}
}

// Add 追加一行；超 cap 时丢弃最旧行。
func (t *Transcript) Add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.cap {
		t.lines = t.lines[len(t.lines)-t.cap:]
	}
}

// Tail 返回末尾 n 行；n≤0 时返回全部。
func (t *Transcript) Tail(n int) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if n <= 0 || n >= len(t.lines) {
		out := make([]string, len(t.lines))
		copy(out, t.lines)
		return out
	}
	out := make([]string, n)
	copy(out, t.lines[len(t.lines)-n:])
	return out
}

// Len 返回当前行数。
func (t *Transcript) Len() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.lines)
}

// FormatTail 将末尾 n 行格式化为可打印文本。
func (t *Transcript) FormatTail(n int) string {
	lines := t.Tail(n)
	if len(lines) == 0 {
		return "(无历史输出)"
	}
	return strings.Join(lines, "\n")
}

// Lines 返回全部行副本（供 viewport 渲染）。
func (t *Transcript) Lines() []string {
	return t.Tail(0)
}

// AppendPartial 追加流式 assistant 片段。
func (t *Transcript) AppendPartial(role, text string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.partial == nil || t.partial.role != role {
		t.partial = &partialEntry{role: role}
	}
	t.partial.buf.WriteString(text)
}

// FinishPartial 将流式片段落为一行。
func (t *Transcript) FinishPartial(role string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.partial == nil || t.partial.role != role {
		return
	}
	line := "[" + role + "] " + t.partial.buf.String()
	t.lines = append(t.lines, line)
	if len(t.lines) > t.cap {
		t.lines = t.lines[len(t.lines)-t.cap:]
	}
	t.partial = nil
}

// ToolFold 控制 tool 事件折叠展示。
type ToolFold struct {
	mu      sync.Mutex
	verbose bool
}

// SetVerbose 切换是否展开 tool JSON。
func (f *ToolFold) SetVerbose(on bool) {
	f.mu.Lock()
	f.verbose = on
	f.mu.Unlock()
}

// Verbose 返回当前是否展开。
func (f *ToolFold) Verbose() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.verbose
}

// Format 将 tool_call/tool_result 格式化为单行或详细文本。
func (f *ToolFold) Format(eventType string, data map[string]any) string {
	f.mu.Lock()
	verbose := f.verbose
	f.mu.Unlock()

	if verbose {
		return fmt.Sprintf("[%s] %v", eventType, data)
	}
	name := strings.TrimSpace(fmt.Sprint(data["name"]))
	if name == "" {
		name = strings.TrimSpace(fmt.Sprint(data["tool_name"]))
	}
	preview := strings.TrimSpace(fmt.Sprint(data["content"]))
	if preview == "" {
		preview = strings.TrimSpace(fmt.Sprint(data["output"]))
	}
	if len(preview) > 80 {
		preview = preview[:80] + "..."
	}
	if name != "" && preview != "" {
		return fmt.Sprintf("[%s] %s → %s", eventType, name, preview)
	}
	if name != "" {
		return fmt.Sprintf("[%s] %s", eventType, name)
	}
	return fmt.Sprintf("[%s] (折叠，/tools verbose 展开)", eventType)
}
