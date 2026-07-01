package compression

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// compressionSourceFingerprint 为 pending 校验生成源区间指纹。
func compressionSourceFingerprint(messages []llm.Message, plan compressionPlan) string {
	start := leadingSystemSkip(messages)
	switch plan.ApplyMode {
	case compressApplyMergeNextUser:
		if plan.End+1 >= len(messages) {
			return ""
		}
		return messagesFingerprint(messages[start : plan.End+2])
	default:
		return messagesFingerprint(messages[start : plan.End+1])
	}
}

// applyCompressionReplacement 按 plan 将 summary 写回 messages，返回 status：applied / invalid / stale。
func applyCompressionReplacement(
	messages []llm.Message,
	plan compressionPlan,
	summary string,
	sourceFingerprint string,
) (merged []llm.Message, status string) {
	status = "invalid"
	start := leadingSystemSkip(messages)
	if plan.End < start || plan.End >= len(messages) || strings.TrimSpace(summary) == "" {
		return nil, status
	}

	switch plan.ApplyMode {
	case compressApplyMergeNextUser:
		if plan.End+1 >= len(messages) || messages[plan.End+1].Role != "user" {
			return nil, status
		}
		current := messagesFingerprint(messages[start : plan.End+2])
		if sourceFingerprint != "" && sourceFingerprint != current {
			return nil, "stale"
		}
		replacement := llm.UserMessage(mergeSummaryWithUser(summary, llm.MessageTextSummary(messages[plan.End+1])), llm.UserNameCompression)
		merged = append(append([]llm.Message(nil), messages[:start]...), replacement)
		merged = append(merged, messages[plan.End+2:]...)
		return merged, "applied"

	case compressApplyKeepFollowingAssistant:
		if plan.End+1 >= len(messages) || messages[plan.End+1].Role != "assistant" {
			return nil, status
		}
		current := messagesFingerprint(messages[start : plan.End+1])
		if sourceFingerprint != "" && sourceFingerprint != current {
			return nil, "stale"
		}
		replacement := llm.UserMessage(strings.TrimSpace(summary), llm.UserNameCompression)
		merged = append(append([]llm.Message(nil), messages[:start]...), replacement)
		merged = append(merged, messages[plan.End+1:]...)
		return merged, "applied"

	default:
		return nil, status
	}
}
