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

func estimateMessageTokens(messages []llm.Message) int {
	return llm.EstimateMessageTokens(messages)
}

func pendingToolCallsCount(pending *turn.PendingHITL) int {
	if pending == nil {
		return 0
	}
	return len(pending.AllToolCalls())
}
