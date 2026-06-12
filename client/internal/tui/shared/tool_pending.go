package shared

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// ToolPendingEntry 为执行中 tool 占位信息。
type ToolPendingEntry struct {
	Title   string
	Started time.Time
}

// ToolPendingTracker 跟踪 tool_call 与 tool_result 之间的 pending 块。
type ToolPendingTracker struct {
	mu      sync.Mutex
	entries map[string]ToolPendingEntry
}

// NewToolPendingTracker 创建 tracker。
func NewToolPendingTracker() *ToolPendingTracker {
	return &ToolPendingTracker{entries: make(map[string]ToolPendingEntry)}
}

// Reset 清空（切换 session）。
func (t *ToolPendingTracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.entries = make(map[string]ToolPendingEntry)
	t.mu.Unlock()
}

// Register 登记 pending tool。
func (t *ToolPendingTracker) Register(id, title string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = "tool"
	}
	t.mu.Lock()
	t.entries[id] = ToolPendingEntry{Title: title, Started: time.Now()}
	t.mu.Unlock()
}

// Remove 移除 pending（tool_result 到达）。
func (t *ToolPendingTracker) Remove(id string) {
	id = strings.TrimSpace(id)
	if id == "" || t == nil {
		return
	}
	t.mu.Lock()
	delete(t.entries, id)
	t.mu.Unlock()
}

// Len 返回当前 pending 数量。
func (t *ToolPendingTracker) Len() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

// FormatPendingLine 将 pending 存储行格式化为带动态耗时与省略号的展示文本（不含前缀）。
func (t *ToolPendingTracker) FormatPendingLine(storedLine string) string {
	if t == nil {
		return strings.TrimPrefix(storedLine, toolPendingLinePrefix)
	}
	id := ToolBlockIDFromMetaLine(storedLine)
	if id == "" {
		return storedLine
	}
	t.mu.Lock()
	entry, ok := t.entries[id]
	t.mu.Unlock()
	if !ok {
		if idx := strings.Index(storedLine, "] "); idx >= 0 {
			return strings.TrimPrefix(storedLine[idx+2:], "▶ ")
		}
		return storedLine
	}
	elapsed := time.Since(entry.Started).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	frame := int(elapsed*2) % 3
	dots := strings.Repeat(".", frame+1)
	for len(dots) < 3 {
		dots += " "
	}
	title := strings.TrimPrefix(entry.Title, "▶ ")
	title = strings.TrimPrefix(title, "调用 ")
	return fmt.Sprintf("▶ %s%s %ds", title, dots, int(elapsed))
}

// ElapsedSeconds 返回指定 call 已执行秒数；不存在时返回 -1。
func (t *ToolPendingTracker) ElapsedSeconds(id string) float64 {
	id = strings.TrimSpace(id)
	if id == "" || t == nil {
		return -1
	}
	t.mu.Lock()
	entry, ok := t.entries[id]
	t.mu.Unlock()
	if !ok {
		return -1
	}
	sec := time.Since(entry.Started).Seconds()
	if sec < 0 {
		return 0
	}
	return sec
}
