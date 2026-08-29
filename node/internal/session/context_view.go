package session

import (
	"github.com/DGS-ai-team/DAgents/node/internal/compression"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// ContextView 为 GET /v1/agents/{id}/context 的聚合视图。
type ContextView struct {
	SessionID                           string
	MessagesCount                       int
	MessagesTotalTokens                 int
	PendingToolCallsCount               int
	ToolLoopCount                       int
	LoadedSkills                        []skills.LoadedSkill
	QueuePending                        int
	HasActiveTurn                       bool
	TurnID                              string
	StepID                              string
	StepIndex                           int
	ContextEpoch                        int
	TurnStatus                          turn.TurnStatus
	TurnEndReason                       string
	StepStatus                          turn.StepStatus
	StepEndReason                       string
	TurnGeneration                      uint64
	RuntimeRevision                     int64
	RuntimeDigest                       string
	PromptDigest                        string
	ToolDigest                          string
	ContextInjectionDigest              string
	ContextInjectionCount               int
	RecoveryRequired                    bool
	TurnState                           turn.State
	SystemPrompt                        string
	SystemPromptEstimatedTokens         int
	SkillsCatalogEstimatedTokens        int
	SkillsCatalogMaxBodyEstimatedTokens int
	SkillsCatalogBloatThreshold         int
	SkillsCatalogTiming                 skills.CatalogTiming
	Messages                            []llm.Message
	LastCompression                     *compression.LastCompressionSnapshot
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
		view.SkillsCatalogTiming = catalog.TimingSnapshot()
	}
}

// contextView assembles the diagnostic projection without becoming another
// runtime state source. Lifecycle data comes from the coordinator snapshot;
// history and skills are copied under the runtime lock.
func (r *runtime) contextView() *ContextView {
	r.lifecycleMu.Lock()
	lifecycle := turn.CoordinatorSnapshot{}
	if r.turnCoordinator != nil {
		lifecycle = r.turnCoordinator.Snapshot()
	}
	r.mu.Lock()
	msgs := append([]llm.Message(nil), r.messages...)
	loaded := append([]skills.LoadedSkill(nil), r.loadedSkills...)
	promptCatalog := r.skillsTurnCatalog
	if promptCatalog == nil {
		promptCatalog = r.skillsCatalog
	}
	queuePending := r.queue.Len()
	if r.inputBox != nil {
		queuePending += r.inputBox.Len()
	}
	state := r.turnState()
	view := &ContextView{
		SessionID:           r.session.ID,
		MessagesCount:       len(r.messages),
		MessagesTotalTokens: estimateMessageTokens(r.messages),
		ToolLoopCount:       lifecycle.Usage.Steps,
		LoadedSkills:        loaded,
		QueuePending:        queuePending,
		HasActiveTurn:       lifecycle.HasActiveTurn,
		TurnID:              lifecycle.TurnID,
		StepID:              lifecycle.StepID,
		StepIndex:           lifecycle.StepIndex,
		ContextEpoch:        lifecycle.ContextEpoch,
		TurnStatus:          lifecycle.TurnStatus,
		TurnEndReason:       lifecycle.TurnEndReason,
		StepStatus:          lifecycle.StepStatus,
		StepEndReason:       lifecycle.StepEndReason,
		TurnGeneration:      lifecycle.Generation,
		RuntimeRevision:     lifecycle.RuntimeRevision,
		RuntimeDigest:       lifecycle.RuntimeDigest,
		PromptDigest:        lifecycle.PromptDigest,
		ToolDigest:          lifecycle.ToolDigest,
		RecoveryRequired:    lifecycle.RecoveryRequired,
		TurnState:           state,
		Messages:            msgs,
	}
	if view.LoadedSkills == nil {
		view.LoadedSkills = []skills.LoadedSkill{}
	}
	r.mu.Unlock()
	r.lifecycleMu.Unlock()
	pending := r.pendingSnapshot()
	view.PendingToolCallsCount = pendingToolCallsCount(pending)
	view.SystemPrompt = r.orch.SystemPromptForSession(r.session.ID)
	if snapshot := r.orch.ModelContextSnapshot(r.session.ID); snapshot != nil {
		view.ContextInjectionDigest = snapshot.ContextInjectionDigest
		view.ContextInjectionCount = len(snapshot.ContextInjections)
	} else {
		injections := r.orch.ContextInjectionsForSession(r.session.ID)
		view.ContextInjectionCount = len(injections)
		if len(injections) > 0 {
			view.ContextInjectionDigest = turn.Digest(injections)
		}
	}
	// Diagnostics must describe the same frozen catalog view as the active
	// model prompt. The live catalog is reserved for discovery and may already
	// contain edits that will not apply until the next context boundary.
	enrichContextPromptStats(view, promptCatalog)
	if r.compression != nil {
		if snap, ok := r.compression.LastCompression(r.session.ID); ok {
			s := snap
			view.LastCompression = &s
		}
	}
	return view
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
