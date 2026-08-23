package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

// llmClientForAgent builds a client from the Agent's bound profile without
// changing cfg.LLM.Active or the process-wide focused Agent. This is the
// critical concurrency boundary for two Workgroup members using different
// models at the same time.
func (s *Server) llmClientForAgent(ctx context.Context, rec *store.AgentRecord, runtimeAgentID string) (llm.Client, error) {
	if s == nil || s.cfg == nil || rec == nil {
		return nil, fmt.Errorf("llm config unavailable")
	}
	cfg := *s.cfg
	snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
	if err != nil {
		return nil, err
	}
	profileID := agentruntime.LLMActiveFromDefaults(snap)
	if profileID != "" {
		if err := cfg.SetActiveLLMProfile(profileID); err != nil {
			return nil, fmt.Errorf("agent llm profile %q: %w", profileID, err)
		}
	}
	settings := llm.NewRuntimeSettings(&cfg)
	settings.AgentID = strings.TrimSpace(runtimeAgentID)
	if s.llmConfigs != nil {
		active := cfg.LLM.ActiveProfileID()
		if active != "" {
			if key, keyErr := s.llmConfigs.ResolveAPIKey(ctx, active); keyErr == nil {
				settings.SetAPIKey(key)
			}
		}
	}
	return llm.NewFromConfig(&cfg, settings), nil
}
