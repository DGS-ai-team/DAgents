package compression

import (
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/history"
)

// FinalizeCompressionSummary 在压缩摘要末尾追加 JSONL 审计文件指引（启用 raw message history 时）。
func FinalizeCompressionSummary(summary, sessionID string, journalEnabled bool, at time.Time) string {
	summary = strings.TrimSpace(summary)
	if summary == "" || !journalEnabled {
		return summary
	}
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return summary
	}
	rel := history.JournalRelativePath(sid, at)
	return summary + "\n\n历史的原始消息请查阅 " + rel + "。"
}
