package compression

import (
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

// compressDecision 为阈值判定结果。
//
// P8 语义：达 silent/blocking 阈值但 buildCompressionPlan 失败时，Should=false 且
// TriggerLevel 仍保留 silent/blocking（非 none），便于区分「未达阈值」与「达阈值但不可压」。
type compressDecision struct {
	Should       bool
	TriggerLevel string // none / silent / blocking
	TotalTokens  int
}

// compressionEvaluation 为阈值判定与 buildCompressionPlan 的单次扫描结果（P1）。
// Decision.Should 与 Plan 是否有效同真同假。
type compressionEvaluation struct {
	Decision compressDecision
	Plan     compressionPlan
}

// compressApplyMode 描述压缩结果写回 session 的方式。
type compressApplyMode string

const (
	compressApplyKeepFollowingAssistant compressApplyMode = "keep_following_assistant"
	compressApplyMergeNextUser          compressApplyMode = "merge_next_user"
)

// sidecarAppendMode 描述侧车 LLM 请求在 snapshot 末尾追加的 ephemeral 消息。
type sidecarAppendMode string

const (
	sidecarAppendAssistantAndUser sidecarAppendMode = "assistant_and_user"
	sidecarAppendUserOnly         sidecarAppendMode = "user_only"
)

// compressionPlan 为一次压缩的区间与侧车/写回策略。
// 可压区间为 messages[leadingSystemSkip:end+1]（生产路径 skip 恒为 0，见 leadingSystemSkip）。
type compressionPlan struct {
	End           int
	ApplyMode     compressApplyMode
	SidecarAppend sidecarAppendMode
}

// prefixClosure 为 messages 各前缀 [0:i+1] 的 OpenAI 轮次合法性/闭合态（一次正序 O(n) 预计算）。
type prefixClosure struct {
	closed []bool
}

func evaluateCompression(messages []llm.Message, silentThreshold, blockingThreshold int) compressionEvaluation {
	silentThreshold = max(0, silentThreshold)
	blockingThreshold = max(0, blockingThreshold)
	total := llm.EstimateMessageTokens(messages)

	var level string
	switch {
	case blockingThreshold > 0 && total >= blockingThreshold:
		level = "blocking"
	case silentThreshold > 0 && total >= silentThreshold:
		level = "silent"
	default:
		return compressionEvaluation{
			Decision: compressDecision{Should: false, TriggerLevel: "none", TotalTokens: total},
		}
	}

	plan, ok := buildCompressionPlan(messages)
	if !ok {
		return compressionEvaluation{
			Decision: compressDecision{Should: false, TriggerLevel: level, TotalTokens: total},
		}
	}
	return compressionEvaluation{
		Decision: compressDecision{Should: true, TriggerLevel: level, TotalTokens: total},
		Plan:     plan,
	}
}

// leadingSystemSkip 返回 messages 首部连续 role=system 条数（P9）。
//
// 生产路径：session/SQLite messages 仅含 user/assistant/tool；system 由
// llm.MessagesWithSystem 在出站 StreamChat 时注入，不落库（见 node/internal/llm/messages.go）。
// 防御：若 journal InsertMessage 等异常写入 leading system，压缩与侧车跳过该前缀，
// 避免与 SidecarPrefix.SystemPrompt 重复且被 apply 误替换。
func leadingSystemSkip(messages []llm.Message) int {
	n := 0
	for n < len(messages) && messages[n].Role == "system" {
		n++
	}
	return n
}

func compressionSlice(messages []llm.Message, plan compressionPlan) []llm.Message {
	start := leadingSystemSkip(messages)
	if plan.End < start || plan.End >= len(messages) {
		return nil
	}
	return append([]llm.Message(nil), messages[start:plan.End+1]...)
}

// buildCompressionPlan 选取压缩区间与写回/侧车策略。
//
// 合法压缩边界 end（isSelectableCompressEnd）：
//   - tool 结果（多 tool_call 时仅该批最后一条 tool）；
//   - 无 tool_calls 的 assistant；
//   - user 且下一条为 assistant（情况一：在 user 处切，保留 tail assistant）。
//
// 前缀闭合由 computePrefixClosure 一次 O(n) 判定；非法 messages 序列不压缩、不修复。
func buildCompressionPlan(messages []llm.Message) (compressionPlan, bool) {
	lastA := lastAssistantIndex(messages)
	if lastA <= 0 {
		return compressionPlan{}, false
	}

	closure := computePrefixClosure(messages)

	last := messages[lastA]
	if len(last.ToolCalls) == 0 && lastA+1 < len(messages) && messages[lastA+1].Role == "user" {
		if !closure.isSelectableCompressEnd(messages, lastA, "user") {
			return compressionPlan{}, false
		}
		return compressionPlan{
			End:           lastA,
			ApplyMode:     compressApplyMergeNextUser,
			SidecarAppend: sidecarAppendUserOnly,
		}, true
	}

	for end := lastA - 1; end >= leadingSystemSkip(messages); end-- {
		if !closure.isSelectableCompressEnd(messages, end, "assistant") {
			continue
		}
		return compressionPlan{
			End:           end,
			ApplyMode:     compressApplyKeepFollowingAssistant,
			SidecarAppend: sidecarAppendAssistantAndUser,
		}, true
	}
	return compressionPlan{}, false
}

func lastAssistantIndex(messages []llm.Message) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return i
		}
	}
	return -1
}

// isSelectableCompressEnd 合并边界候选与 prefixClosed（P2）。
// requireFollowingRole 非空时要求 messages[end+1].Role 与之匹配。
func (c prefixClosure) isSelectableCompressEnd(messages []llm.Message, end int, requireFollowingRole string) bool {
	if end < 0 || end >= len(messages) || !c.closed[end] {
		return false
	}
	m := messages[end]
	selectable := false
	switch {
	case m.Role == "tool":
		selectable = true
	case m.Role == "assistant" && len(m.ToolCalls) == 0:
		selectable = true
	case m.Role == "user" && end+1 < len(messages) && messages[end+1].Role == "assistant":
		selectable = true
	}
	if !selectable {
		return false
	}
	if requireFollowingRole == "" {
		return true
	}
	return end+1 < len(messages) && messages[end+1].Role == requireFollowingRole
}

func computePrefixClosure(messages []llm.Message) prefixClosure {
	out := prefixClosure{closed: make([]bool, len(messages))}
	if len(messages) == 0 {
		return out
	}

	pending := make(map[string]struct{})
	seqOK := true
	for i, m := range messages {
		if seqOK {
			if ok := applyMessageToPending(pending, m); !ok {
				seqOK = false
			}
		}
		out.closed[i] = seqOK && len(pending) == 0
	}
	return out
}

func (c prefixClosure) prefixClosed(end int) bool {
	if end < 0 || end >= len(c.closed) {
		return false
	}
	return c.closed[end]
}

func applyMessageToPending(pending map[string]struct{}, m llm.Message) bool {
	switch m.Role {
	case "assistant":
		if len(m.ToolCalls) > 0 {
			if len(pending) > 0 {
				return false
			}
			for _, tc := range m.ToolCalls {
				if tc.ID == "" {
					return false
				}
				pending[tc.ID] = struct{}{}
			}
			return true
		}
		return len(pending) == 0
	case "tool":
		if m.ToolCallID == "" {
			return false
		}
		if _, exists := pending[m.ToolCallID]; !exists {
			return false
		}
		delete(pending, m.ToolCallID)
		return true
	default:
		return len(pending) == 0
	}
}

func mergeSummaryWithUser(summary, userText string) string {
	summary = strings.TrimSpace(summary)
	userText = strings.TrimSpace(userText)
	switch {
	case summary == "":
		return userText
	case userText == "":
		return summary
	default:
		return summary + "\n\n" + userText
	}
}
