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
	mu                 sync.Mutex
	lines              []string
	cap                int
	partial            *partialEntry
	pendingUsageSuffix string
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

// AddBlockGapIfNeeded 在上一条非空行后插入空行，避免连续追加产生双空行。
func (t *Transcript) AddBlockGapIfNeeded() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.lines) == 0 {
		return
	}
	if strings.TrimSpace(t.lines[len(t.lines)-1]) == "" {
		return
	}
	t.lines = append(t.lines, "")
	if len(t.lines) > t.cap {
		t.lines = t.lines[len(t.lines)-t.cap:]
	}
}

const (
	sysPanelTitlePrefix = "[sys-panel-title] "
	sysPanelBodyPrefix  = "[sys-panel-body] "
)

// AddSystemPanel 追加带标题的结构化 system 面板（供 /status /sessions /skill 等）。
func (t *Transcript) AddSystemPanel(title string, bodyLines []string) {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "System"
	}
	t.AddBlockGapIfNeeded()
	t.Add(sysPanelTitlePrefix + title)
	for _, line := range bodyLines {
		t.Add(sysPanelBodyPrefix + line)
	}
}

// Add 追加一行；超 cap 时丢弃最旧行。
func (t *Transcript) Add(line string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if strings.HasPrefix(line, "[user] ") {
		t.pendingUsageSuffix = ""
	}
	t.appendLineLocked(line)
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

// LinesForDisplay 按终端宽度渲染 transcript（彩色圆点、usage 右对齐）。
func (t *Transcript) LinesForDisplay(width int) []string {
	raw := t.Tail(0)
	if len(raw) == 0 {
		return raw
	}
	out := make([]string, len(raw))
	for i, line := range raw {
		out[i] = FormatTranscriptLineForDisplay(line, width)
	}
	return out
}

// ToolDisplayOptions 控制 tool 折叠行在 viewport 中的可见性。
type ToolDisplayOptions struct {
	Registry *ToolBlockRegistry
	Pending  *ToolPendingTracker
	Verbose  bool
}

// SnapshotLinesForDisplay 含流式 partial 缓冲，供全屏 viewport 刷新。
func (t *Transcript) SnapshotLinesForDisplay(width int, toolOpts *ToolDisplayOptions) []string {
	raw := t.Tail(0)
	if len(raw) == 0 {
		raw = nil
	} else if toolOpts != nil && toolOpts.Registry != nil {
		filtered := make([]string, 0, len(raw))
		for _, line := range raw {
			if IsToolDetailLine(line) {
				id := ToolBlockIDFromMetaLine(line)
				if !toolOpts.Registry.IsExpanded(id, toolOpts.Verbose) {
					continue
				}
			} else if IsToolPreviewLine(line) {
				id := ToolBlockIDFromMetaLine(line)
				if toolOpts.Registry.IsExpanded(id, toolOpts.Verbose) {
					continue
				}
			}
			filtered = append(filtered, line)
		}
		raw = filtered
	}
	lines := make([]string, len(raw))
	for i, line := range raw {
		displayLine := line
		if toolOpts != nil && toolOpts.Pending != nil && strings.HasPrefix(line, toolPendingLinePrefix) {
			displayLine = toolPendingLinePrefix + ToolBlockIDFromMetaLine(line) + "] " +
				toolOpts.Pending.FormatPendingLine(line)
		}
		lines[i] = FormatTranscriptLineForDisplay(displayLine, width)
	}
	t.mu.Lock()
	partial := t.partial
	var role, text string
	if partial != nil && partial.buf.Len() > 0 {
		role = partial.role
		text = partial.buf.String()
	}
	t.mu.Unlock()
	if len(lines) == 0 && text == "" {
		return lines
	}
	if text == "" {
		return lines
	}
	if role == "" {
		role = "assistant"
	}
	out := make([]string, len(lines), len(lines)+1)
	copy(out, lines)
	out = append(out, FormatTranscriptLineForDisplay("["+role+"] "+text, width))
	return out
}

// AppendPartial 追加流式 assistant/reasoning 片段；空串不创建空 partial。
func (t *Transcript) AppendPartial(role, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.partial == nil || t.partial.role != role {
		t.partial = &partialEntry{role: role}
	}
	t.partial.buf.WriteString(text)
}

// FinishPartial 将流式片段落为一行；空缓冲不落行，避免 tool-only 回合产生空白行。
func (t *Transcript) FinishPartial(role string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	suffix := ""
	if role == "assistant" && t.pendingUsageSuffix != "" {
		suffix = t.pendingUsageSuffix
		t.pendingUsageSuffix = ""
	}
	t.finishPartialLocked(role, suffix)
}

// ApplyRoundUsage 将单轮 usage 写入 assistant 存储行（\x1e 分隔）；展示层 usage 独占一行右对齐。
func (t *Transcript) ApplyRoundUsage(suffix string) {
	if strings.TrimSpace(suffix) == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.partial != nil && t.partial.role == "assistant" {
		text := t.partial.buf.String()
		t.partial = nil
		if strings.TrimSpace(text) == "" {
			t.pendingUsageSuffix = suffix
			return
		}
		t.appendLineLocked("[assistant] " + appendUsageSuffix(text, suffix))
		return
	}
	for i := len(t.lines) - 1; i >= 0; i-- {
		line := t.lines[i]
		if strings.HasPrefix(line, "[assistant] ") {
			if !strings.HasSuffix(line, suffix) {
				body := strings.TrimPrefix(line, "[assistant] ")
				t.lines[i] = "[assistant] " + appendUsageSuffix(body, suffix)
			}
			return
		}
	}
	t.pendingUsageSuffix = suffix
}

func appendUsageSuffix(content, suffix string) string {
	if suffix == "" {
		return content
	}
	plain := suffix
	if !strings.HasPrefix(plain, " ·") {
		trimmed := strings.TrimSpace(plain)
		if trimmed == "" {
			return strings.TrimRight(content, "\n\r")
		}
		plain = " · " + trimmed
	}
	return strings.TrimRight(content, "\n\r") + usageStorageSep + plain
}

func (t *Transcript) finishPartialLocked(role, suffix string) {
	if t.partial == nil || t.partial.role != role {
		return
	}
	text := t.partial.buf.String()
	t.partial = nil
	if strings.TrimSpace(text) == "" {
		return
	}
	line := "[" + role + "] " + appendUsageSuffix(text, suffix)
	t.appendLineLocked(line)
}

// ReplaceToolCallLines 替换指定 tool 块的 pending/代码预览行（用于流式 partial tool_call）。
func (t *Transcript) ReplaceToolCallLines(blockID string, lines []string) {
	blockID = strings.TrimSpace(blockID)
	if blockID == "" {
		for _, line := range lines {
			t.Add(line)
		}
		return
	}
	t.RemoveToolPendingLines(blockID)
	for _, line := range lines {
		t.Add(line)
	}
}

// RemoveToolPendingLines 移除指定 tool 块的 pending 占位行与 call 代码预览行。
func (t *Transcript) RemoveToolPendingLines(blockID string) {
	blockID = strings.TrimSpace(blockID)
	if blockID == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	prefixes := []string{
		toolPendingLinePrefix + blockID + "]",
		toolCallCodeLinePrefix + blockID + "]",
	}
	var kept []string
	for _, line := range t.lines {
		skip := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(line, prefix) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		kept = append(kept, line)
	}
	t.lines = kept
}

func (t *Transcript) appendLineLocked(line string) {
	t.lines = append(t.lines, line)
	if len(t.lines) > t.cap {
		t.lines = t.lines[len(t.lines)-t.cap:]
	}
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

// Format 将 tool_call/tool_result 格式化为用户可读文本（委托 FormatToolEvent）。
func (f *ToolFold) Format(eventType string, data map[string]any) string {
	f.mu.Lock()
	verbose := f.verbose
	f.mu.Unlock()
	lines := FormatToolEvent(eventType, data, verbose)
	if len(lines) == 0 {
		return fmt.Sprintf("[%s] (无详情)", eventType)
	}
	return strings.Join(lines, "\n")
}

func trimDisplayField(v any) string {
	if v == nil {
		return ""
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return ""
	}
	return s
}
