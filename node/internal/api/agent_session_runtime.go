package api

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// agentSessionRuntime contains the immutable runtime inputs that are shared by
// a personal Agent session and isolated sessions opened for that Agent by a
// Workgroup. Callers still choose the session ID and own its queue/history.
type agentSessionRuntime struct {
	Built            agentruntime.Built
	Policy           *policy.Engine
	LLM              llm.Client
	LLMProfileDigest string
}

type agentSessionRuntimeOptions struct {
	DisableMemory       bool
	RequireStoreContext bool
}

// buildAgentSessionRuntime builds one isolated registry and its matching
// policy, prompt, and model inputs. It deliberately does not create or reuse a
// session, so callers retain their own session ID, queue, and history.
func (s *Server) buildAgentSessionRuntime(
	ctx context.Context,
	rec *store.AgentRecord,
	options agentSessionRuntimeOptions,
) (_ agentSessionRuntime, err error) {
	if s == nil || s.cfg == nil || s.sessions == nil || rec == nil {
		return agentSessionRuntime{}, fmt.Errorf("agent runtime unavailable")
	}
	id := rec.AgentID
	if id == "" {
		return agentSessionRuntime{}, fmt.Errorf("agent_id is required")
	}
	snapshot, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return agentSessionRuntime{}, fmt.Errorf("parse agent snapshot: %w", err)
	}

	var policyEngine *policy.Engine
	if s.agents != nil {
		policyEngine, err = s.agents.LoadAgentPolicyEngine(ctx, id)
		if err != nil {
			if options.RequireStoreContext {
				return agentSessionRuntime{}, fmt.Errorf("load agent policy: %w", err)
			}
			s.logger.Warn("agent policy load failed", "agent_id", id, "error", err)
			policyEngine = nil
		}
	}

	built, err := agentruntime.Build(agentruntime.BuildParams{
		NodeCFG:              s.cfg,
		BaseTurn:             s.sessions.DefaultTurnOptions(),
		AgentID:              id,
		Snapshot:             snapshot,
		MCP:                  s.mcpManager,
		WorkspaceCoordinator: s.workspaceCoord,
		DisableMemory:        options.DisableMemory,
	})
	if err != nil {
		return agentSessionRuntime{}, fmt.Errorf("build agent runtime: %w", err)
	}
	transferred := false
	defer func() {
		if !transferred {
			_ = built.Close()
		}
	}()

	s.attachNodeRuntimeDeps(built.Registry, id)
	built.TurnOptions.PreferredName = s.cfg.PreferredName()
	built.TurnOptions.RuntimeRevision = rec.RuntimeRevision

	if s.agents != nil {
		promptContext, contextErr := s.agents.EnsureAgentPromptContext(ctx, id)
		if contextErr != nil {
			if options.RequireStoreContext {
				return agentSessionRuntime{}, fmt.Errorf("load agent prompt context: %w", contextErr)
			}
			s.logger.Warn("agent prompt context load failed", "agent_id", id, "error", contextErr)
		} else {
			built.TurnOptions.PromptContent = promptContentFromRecord(promptContext)
		}
	}
	promptSeed := ""
	if built.TurnOptions.PromptContent != nil {
		if raw, marshalErr := json.Marshal(built.TurnOptions.PromptContent); marshalErr == nil {
			promptSeed = string(raw)
		}
	}
	built.TurnOptions.RuntimeDigest = turn.RuntimeDigestFromInputs(
		snapshot,
		promptSeed,
		built.Registry.Definitions(),
	)

	client, profileDigest, err := s.llmClientForAgent(ctx, rec, id)
	if err != nil {
		return agentSessionRuntime{}, fmt.Errorf("resolve agent llm: %w", err)
	}
	built.TurnOptions.LLMProfileDigest = profileDigest
	transferred = true
	return agentSessionRuntime{
		Built: built, Policy: policyEngine, LLM: client, LLMProfileDigest: profileDigest,
	}, nil
}
