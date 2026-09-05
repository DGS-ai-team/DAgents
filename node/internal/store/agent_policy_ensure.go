package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// EnsureAgentPolicy 确保 Agent 有策略行；新 Agent 从内置种子初始化。
func (s *AgentStore) EnsureAgentPolicy(ctx context.Context, agentID string) (*AgentPolicyRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	existing, err := s.GetAgentPolicy(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	maps := policy.LoadSeedMaps()
	tools, shell := policy.MapsToStringMaps(maps)
	rec := AgentPolicyRecord{
		AgentID:   agentID,
		Tools:     tools,
		Shell:     shell,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.SaveAgentPolicy(ctx, rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

// LoadAgentPolicyEngine 确保并加载为 policy.Engine。
func (s *AgentStore) LoadAgentPolicyEngine(ctx context.Context, agentID string) (*policy.Engine, error) {
	rec, err := s.EnsureAgentPolicy(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return policy.NewEngineFromMaps(policy.StringMapsToMaps(rec.Tools, rec.Shell)), nil
}

// EnsureAgentPromptContext ensures the prompt sidecar row exists.
func (s *AgentStore) EnsureAgentPromptContext(ctx context.Context, agentID string) (*AgentPromptContextRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	existing, err := s.GetAgentPromptContext(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}

	rec := AgentPromptContextRecord{
		AgentID:   agentID,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.SaveAgentPromptContext(ctx, rec); err != nil {
		return nil, err
	}
	return &rec, nil
}
