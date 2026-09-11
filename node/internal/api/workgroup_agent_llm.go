package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/agentruntime"
	"github.com/DGS-ai-team/DAgents/node/internal/browser"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

// resolveLLMForAgent resolves the effective Agent-bound profile without
// changing cfg.LLM.Active or the process-wide focused Agent. This is the
// critical concurrency boundary for two runtimes using different models.
func (s *Server) resolveLLMForAgent(ctx context.Context, rec *store.AgentRecord, runtimeAgentID string) (*config.Config, *llm.RuntimeSettings, error) {
	if s == nil || s.cfg == nil {
		return nil, nil, fmt.Errorf("llm config unavailable")
	}
	cfg := *s.cfg
	if rec != nil {
		snap, err := agentruntime.ParseSnapshot(rec.ConfigSnapshot)
		if err != nil {
			return nil, nil, err
		}
		profileID := agentruntime.LLMActiveFromDefaults(snap)
		if profileID != "" {
			if _, ok := cfg.LLM.GetProfile(profileID); ok {
				if err := cfg.SetActiveLLMProfile(profileID); err != nil {
					return nil, nil, fmt.Errorf("agent llm profile %q: %w", profileID, err)
				}
			}
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
	return &cfg, settings, nil
}

// llmClientForAgent builds a client and returns the effective profile digest.
func (s *Server) llmClientForAgent(ctx context.Context, rec *store.AgentRecord, runtimeAgentID string) (llm.Client, string, error) {
	if rec == nil {
		return nil, "", fmt.Errorf("agent record required")
	}
	cfg, settings, err := s.resolveLLMForAgent(ctx, rec, runtimeAgentID)
	if err != nil {
		return nil, "", err
	}
	return llm.NewFromConfig(cfg, settings), settings.Fingerprint(), nil
}

// browserLLMForAgent resolves the same Agent-bound profile used by the main
// turn and returns only the transient settings needed by browser-use. The
// sidecar must not read bootstrap YAML for model selection: SQLite/Agent
// snapshots are the single runtime source of truth.
func (s *Server) browserLLMForAgent(ctx context.Context, agentID string) (*browser.LLMSettings, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("llm config unavailable")
	}
	var rec *store.AgentRecord
	if s.agents != nil && strings.TrimSpace(agentID) != "" {
		loaded, err := s.agents.Get(ctx, strings.TrimSpace(agentID))
		if err != nil {
			return nil, fmt.Errorf("load agent llm profile: %w", err)
		}
		rec = loaded
	}
	_, settings, err := s.resolveLLMForAgent(ctx, rec, agentID)
	if err != nil {
		return nil, err
	}
	provider, baseURL, keyEnv, mock := settings.Connection()
	view := settings.Snapshot()
	return &browser.LLMSettings{
		Provider:          provider,
		BaseURL:           baseURL,
		Model:             settings.ModelName(),
		APIKeyEnv:         keyEnv,
		APIKey:            settings.APIKeyValue(),
		Mock:              mock,
		MultimodalEnabled: view.MultimodalEnabled,
		Thinking:          view.Thinking,
		ReasoningEffort:   view.ReasoningEffort,
	}, nil
}
