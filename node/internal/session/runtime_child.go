package session

import (
	"log/slog"

	"github.com/DGS-ai-team/DAgents/node/internal/childagent"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
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
	initialLoaded []skills.LoadedSkill,
	childMgr *childagent.Manager,
) *runtime {
	// 创建受限工具注册表
	restricted := childagent.NewRestrictedRegistry(baseRegistry, allowedTools)
	// 创建子 Agent 消息中继
	relay := &childagent.RelayHub{
		Inner:         hub,
		ParentAgentID: parentID,
		AgentID:       agentID,
		ChildAgentID:  id,
		ChildPurpose:  purpose,
		Observe: func(eventType string, data map[string]any) {
			if childMgr != nil {
				childMgr.ObserveChildEvent(id, eventType, data)
			}
		},
	}
	// 创建子 runtime
	rt := newRuntimeWithPublisher(
		id, agentID, relay, hub, llmClient, restricted, policyEngine, nil, logger,
		nil, initialLoaded, nil, 0, nil, false, 0, 0, turnOpts, nil,
	)
	// 设置子 runtime 元数据
	rt.childMeta = &childRuntimeMeta{
		parentSessionID: parentID,
		childMgr:        childMgr,
	}
	rt.orch.SetChildSession(true)
	rt.orch.SetSystemPromptBuilder(turn.ChildSystemPromptBuilder(purpose))
	rt.orch.SetContextInjectionBuilder(turn.ChildContextInjectionBuilder(purpose))
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

func (r *runtime) stepIndexSnapshot() int {
	if r == nil || r.turnCoordinator == nil {
		return 0
	}
	return r.turnCoordinator.Snapshot().Usage.Steps
}

func (r *runtime) tryCompleteChildIfIdle() {
	if r.childMeta == nil || r.childMeta.childMgr == nil {
		return
	}
	r.mu.Lock()
	queueEmpty := r.queue.Len() == 0
	meta := r.childMeta
	msgs := append([]llm.Message(nil), r.messages...)
	r.mu.Unlock()
	loops := r.stepIndexSnapshot()
	idle := r.pendingSnapshot() == nil && queueEmpty && !r.lifecycleExecutionBusy()
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
