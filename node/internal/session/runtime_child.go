package session

import (
	"log/slog"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func newChildRuntime(
	id, parentID, agentID string,
	hub *stream.Hub,
	llmClient llm.Client,
	baseRegistry *tools.Registry,
	policyEngine *policy.Engine,
	logger *slog.Logger,
	turnOpts TurnOptions,
	allowedTools []string,
	purpose string,
	childMgr *childagent.Manager,
) *runtime {
	restricted := childagent.NewRestrictedRegistry(baseRegistry, allowedTools)
	relay := &childagent.RelayHub{
		Inner:           hub,
		ParentSessionID: parentID,
		AgentID:         agentID,
		ChildSessionID:  id,
		ChildPurpose:    purpose,
	}
	rt := newRuntimeWithPublisher(
		id, agentID, relay, hub, llmClient, restricted, policyEngine, nil, logger,
		nil, nil, nil, 0, turnOpts, nil,
	)
	rt.childMeta = &childRuntimeMeta{
		parentSessionID: parentID,
		childMgr:        childMgr,
	}
	rt.orch.SetChildAgentTools(childMgr, true)
	return rt
}

type childRuntimeMeta struct {
	parentSessionID string
	childMgr        *childagent.Manager
	completing      bool
}

func (r *runtime) isChildSession() bool {
	return r.childMeta != nil
}

func (r *runtime) toolLoopCountSnapshot() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.toolLoopCount
}

func (r *runtime) tryCompleteChildIfIdle() {
	if r.childMeta == nil || r.childMeta.childMgr == nil {
		return
	}
	r.mu.Lock()
	idle := r.pending == nil && r.queue.Len() == 0
	meta := r.childMeta
	msgs := append([]llm.Message(nil), r.messages...)
	loops := r.toolLoopCount
	r.mu.Unlock()
	if !idle || meta.completing {
		return
	}
	if len(msgs) == 0 {
		return
	}
	last := msgs[len(msgs)-1]
	if last.Role != "assistant" {
		return
	}
	meta.completing = true
	summary := lastAssistantSummary(msgs)
	meta.childMgr.OnChildSettled(r.session.ID, summary, loops)
}
