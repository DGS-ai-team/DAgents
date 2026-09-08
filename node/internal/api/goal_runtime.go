package api

import (
	"context"
	"fmt"
	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/goals"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// ensureGoalRuntime builds an isolated runtime from the selected Agent snapshot.
// It never calls Manager.Create, which would bind the Node default Agent.
func (s *Server) ensureGoalRuntime(ctx context.Context, rec store.AgentRecord, sessionID string) error {
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return fmt.Errorf("parse agent snapshot: %w", err)
	}
	if _, err = agentruntime.EnsureWorkspace(s.cfg.RuntimeDir(), rec.AgentID, snap.Workspace); err != nil {
		return err
	}
	var pe *policy.Engine
	if s.agents != nil {
		var e error
		pe, e = s.agents.LoadAgentPolicyEngine(ctx, rec.AgentID)
		if e != nil {
			return fmt.Errorf("load agent policy: %w", e)
		}
	}
	var client llm.Client
	var digest string
	if s.llmInjected && s.defaultLLM != nil {
		client, digest = s.defaultLLM, "injected-test"
	} else {
		client, digest, err = s.llmClientForAgent(ctx, &rec, rec.AgentID)
		if err != nil {
			return fmt.Errorf("resolve goal agent LLM: %w", err)
		}
	}
	built, err := agentruntime.Build(agentruntime.BuildParams{NodeCFG: s.cfg, BaseTurn: s.sessions.DefaultTurnOptions(), AgentID: rec.AgentID, Snapshot: snap, MCP: s.mcpManager, WorkspaceCoordinator: s.workspaceCoord})
	if err != nil {
		return err
	}
	// A dedicated Goal runtime may checkpoint its own run, but cannot mutate
	// the parent Auto Agent's long-lived task configuration.
	built.Registry.SetAutonomyRuntime(false, nil, nil)
	s.attachNodeRuntimeDeps(built.Registry, rec.AgentID)
	built.Registry.EnableManagedGoalCheckpoint()
	if goal, ok := s.lookupGoalSession(sessionID); ok {
		built.TurnOptions.OnLifecycle = s.observeGoalLifecycle
		// Hydrate/context endpoints may restore paused, completed, or
		// post-restart sessions. Leave their conservative agent limits intact;
		// goal wake/message paths validate runnable state before starting a Turn.
		if goal.Status != goals.StatusPaused && goal.Status != goals.StatusCompleted && goal.Status != goals.StatusStopped {
			if err := applyGoalBudget(&built.TurnOptions, goal); err != nil {
				_ = built.Close()
				return err
			}
		}
		built.TurnOptions.BudgetResolver = func() (turn.TurnBudget, error) {
			current, found, err := s.goalForSession(sessionID)
			if err != nil {
				return turn.TurnBudget{}, err
			}
			if !found {
				return turn.TurnBudget{}, fmt.Errorf("managed goal not found for session")
			}
			refreshed := built.TurnOptions
			if err := applyGoalBudget(&refreshed, current); err != nil {
				return turn.TurnBudget{}, err
			}
			return refreshed.Budget, nil
		}
	}
	built.TurnOptions.RuntimeRevision = rec.RuntimeRevision
	built.TurnOptions.LLMProfileDigest = digest
	if _, _, err = s.sessions.ReplaceWithOptionsAndLLM(sessionID, built.TurnOptions, built.Registry, pe, client, rec.AgentID); err != nil {
		_ = built.Close()
		return err
	}
	return nil
}
