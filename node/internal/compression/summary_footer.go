package compression

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/history"
)

// FinalizeCompressionSummary 在压缩摘要末尾追加 JSONL 审计文件指引（启用 raw message history 时）。
func FinalizeCompressionSummary(summary, sessionID string, journalEnabled bool, at time.Time, relativeRoot ...string) string {
	summary = strings.TrimSpace(summary)
	if summary == "" || !journalEnabled {
		return summary
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return summary
	}
	rel := history.RuntimeJournalRelativePath(sid, at)
	location := "<runtime_root>/" + rel
	note := "该目录不属于 Agent workspace"
	if len(relativeRoot) > 0 && strings.TrimSpace(relativeRoot[0]) != "" {
		location = "<workspace_root>/" + filepath.ToSlash(filepath.Join(strings.TrimSpace(relativeRoot[0]), history.JournalRelativePath(sid, at)))
		note = "该文件属于当前 Agent 的 workspace 私有状态"
	}
	return summary + "\n\nNode 已将原始消息记录到 " + location + "（" + note + "）。"
}
