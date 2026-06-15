package session

import (
	"github.com/DGS-ai-team/DAgents/node/internal/compression"
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
	SystemPrompt          string
	SystemPromptEstimatedTokens  int
	SkillsCatalogEstimatedTokens      int
	SkillsCatalogMaxBodyEstimatedTokens int
	SkillsCatalogBloatThreshold       int
	Messages              []llm.Message
	LastCompression       *compression.LastCompressionSnapshot
}

func enrichContextPromptStats(view *ContextView, catalog *skills.Catalog) {
	if view == nil {
		return
	}
	view.SystemPromptEstimatedTokens = llm.EstimateTextTokens(view.SystemPrompt)
	view.SkillsCatalogBloatThreshold = skills.CatalogBloatTokenThreshold
	if catalog != nil {
		stats := catalog.EstimateCatalogStats()
		view.SkillsCatalogEstimatedTokens = stats.MetadataTokens
		view.SkillsCatalogMaxBodyEstimatedTokens = stats.MaxBodyTokens
	}
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
