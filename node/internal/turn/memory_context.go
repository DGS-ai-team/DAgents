package turn

import (
	"context"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/memory"
)

const (
	memoryRecallLimit        = memory.DefaultMemorySearchLimit
	memoryRecallTokenBudget  = memory.DefaultMemoryRecallTokenBudget
	memoryCoreTokenBudget    = memory.DefaultMemoryCoreTokenBudget
	memoryContextTokenBudget = memory.DefaultMemoryContextTokenBudget
)

// buildMemoryInjection performs one recall at the model-context boundary.
// The returned snapshot is stored in ModelContextSnapshot; subsequent Steps
// and approval/resume requests reuse the same request-only context rather than
// querying a database that may have changed underneath the Turn.
func (o *Orchestrator) buildMemoryInjection(ctx context.Context, sessionID string, history []llm.Message) (*memory.Snapshot, *ContextInjection) {
	if o == nil || o.memoryService == nil || !o.memoryAutoRecall || o.isChildSession {
		return nil, nil
	}
	root := latestMemoryRecallRoot(history)
	if root == nil {
		return nil, nil
	}
	coreBudget := memoryCoreTokenBudget
	if o.memoryCoreBudgetTokens > 0 {
		coreBudget = o.memoryCoreBudgetTokens
	}
	request := memory.RecallRequest{
		AgentID:            o.agentID,
		RootMessageID:      messageReference(*root),
		QueryText:          llm.MessageTextSummary(*root),
		Limit:              memoryRecallLimit,
		TokenBudget:        memoryRecallTokenBudget,
		CoreBudget:         coreBudget,
		ContextTokenBudget: memoryContextTokenBudget,
		IncludeCore:        true,
		Now:                time.Now().UTC(),
	}
	snapshot, err := o.memoryService.Recall(ctx, request)
	if err != nil {
		// Memory is an enhancement, not a reason to fail an otherwise valid
		// model request. Keep the error observable and send no partial context.
		if o.logger != nil {
			o.logger.Warn("memory recall failed", "agent_id", o.agentID, "session_id", sessionID, "error", err)
		}
		return nil, nil
	}
	if strings.TrimSpace(snapshot.RenderedContent) == "" {
		return &snapshot, nil
	}
	injection := &ContextInjection{
		Name:        llm.UserNameMemoryContext,
		Source:      "memory",
		Content:     snapshot.RenderedContent,
		Position:    "after_current_user",
		MessageKind: llm.MessageSourceMemory,
		MessageForm: llm.MessageFormSnapshot,
		LegacyName:  llm.UserNameMemoryContext,
	}
	return &snapshot, injection
}

// SetMemoryCoreBudgetTokens controls the per-Turn Core memory budget. It is
// configured when a runtime is built; the active ModelContextSnapshot remains
// immutable until the next context boundary.
func (o *Orchestrator) SetMemoryCoreBudgetTokens(tokens int) {
	if o == nil || tokens <= 0 {
		return
	}
	o.memoryCoreBudgetTokens = tokens
}

func messageReference(message llm.Message) string {
	if message.Provenance == nil {
		return ""
	}
	return strings.TrimSpace(message.Provenance.Reference)
}

func latestMemoryRecallRoot(history []llm.Message) *llm.Message {
	for i := len(history) - 1; i >= 0; i-- {
		if !isContextRootUser(history[i]) {
			continue
		}
		message := history[i]
		return &message
	}
	return nil
}
