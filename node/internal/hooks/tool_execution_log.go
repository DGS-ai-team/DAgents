package hooks

import (
	"sync"
	"time"
)

// ToolExecutionRecord 为本 session 最近一次成功执行的 tool 快照。
type ToolExecutionRecord struct {
	ToolName        string
	ArgsFingerprint string
	ToolCallID      string
	ExecutedAt      time.Time
	ResultPreview   string
}

// ToolExecutionLog 按 session 维护单条「上次成功执行」记录（每 Orchestrator 一份）。
type ToolExecutionLog struct {
	mu   sync.Mutex
	last *ToolExecutionRecord
}

// LastRecord 返回最近成功记录；无记录时 ok=false。
func (l *ToolExecutionLog) LastRecord() (ToolExecutionRecord, bool) {
	if l == nil {
		return ToolExecutionRecord{}, false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.last == nil {
		return ToolExecutionRecord{}, false
	}
	return *l.last, true
}

// RecordSuccess 在 tool 成功执行后更新记录。
func (l *ToolExecutionLog) RecordSuccess(toolName, fingerprint, toolCallID, resultPreview string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.last = &ToolExecutionRecord{
		ToolName:        toolName,
		ArgsFingerprint: fingerprint,
		ToolCallID:      toolCallID,
		ExecutedAt:      time.Now(),
		ResultPreview:   truncatePreview(resultPreview, 200),
	}
}

func truncatePreview(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}
