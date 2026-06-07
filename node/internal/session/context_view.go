package session

import (
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// ContextView 为 GET /v1/sessions/{id}/context 的聚合视图。
type ContextView struct {
	SessionID             string
	MessagesCount         int
	MessagesTotalTokens   int
	PendingToolCallsCount int
	ToolLoopCount         int
	LoadedSkills          []skills.LoadedSkill
	QueuePending          int
	HasActiveTurn         bool
	TurnState             turn.State
	Messages              []llm.Message
}

// estimateMessageTokens 粗算 messages token（与 compression 估算一致：len/4 + 固定开销）。
func estimateMessageTokens(messages []llm.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content)/4 + 16
		if len(m.ToolCalls) > 0 {
			total += len(m.ToolCalls) * 32
		}
		if m.ReasoningContent != "" {
			total += len(m.ReasoningContent) / 4
		}
	}
	return total
}

func pendingToolCallsCount(pending *turn.PendingHITL) int {
	if pending == nil {
		return 0
	}
	return len(pending.AllToolCalls())
}
