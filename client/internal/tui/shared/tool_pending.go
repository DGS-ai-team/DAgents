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

// FormatPendingLine 将 pending 存储行格式化为带耗时的展示文本（不含前缀）。
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
		// 结果已到达但 transcript 行尚未剔除时，回退原文。
		if idx := strings.Index(storedLine, "] "); idx >= 0 {
			return strings.TrimPrefix(storedLine[idx+2:], "▶ ")
		}
		return storedLine
	}
	sec := int(time.Since(entry.Started).Seconds())
	if sec < 0 {
		sec = 0
	}
	return fmt.Sprintf("▶ %s … %ds", entry.Title, sec)
}
