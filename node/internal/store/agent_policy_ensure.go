package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// EnsureAgentPolicy 确保 Agent 有策略行：已有则返回；否则从旧文件迁移或种子写入 SQLite。
func (s *AgentStore) EnsureAgentPolicy(ctx context.Context, agentID, runtimeDir string) (*AgentPolicyRecord, error) {
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
	runtimeDir = strings.TrimSpace(runtimeDir)
	if runtimeDir != "" {
		if legacy, err := policy.LoadMapsFromDir(filepath.Join(runtimeDir, "policy")); err == nil {
			maps = legacy
		}
	}
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
func (s *AgentStore) LoadAgentPolicyEngine(ctx context.Context, agentID, runtimeDir string) (*policy.Engine, error) {
	rec, err := s.EnsureAgentPolicy(ctx, agentID, runtimeDir)
	if err != nil {
		return nil, err
	}
	return policy.NewEngineFromMaps(policy.StringMapsToMaps(rec.Tools, rec.Shell)), nil
}

// EnsureAgentPromptContext 确保侧车正文行存在；缺失时从旧文件迁移或写空行。
func (s *AgentStore) EnsureAgentPromptContext(ctx context.Context, agentID, runtimeDir string) (*AgentPromptContextRecord, error) {
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
	runtimeDir = strings.TrimSpace(runtimeDir)
	if runtimeDir != "" {
		rec.SoulMD = readTrimmedFile(filepath.Join(runtimeDir, "prompt_context", "soul.md"))
		rec.UserMD = readTrimmedFile(filepath.Join(runtimeDir, "prompt_context", "user.md"))
		rec.CustomMD = readTrimmedFile(filepath.Join(runtimeDir, "prompt_context", "custom.md"))
		rec.LongTermMD = readTrimmedFile(filepath.Join(runtimeDir, "memory", "long_term.md"))
	}
	if err := s.SaveAgentPromptContext(ctx, rec); err != nil {
		return nil, err
	}
	return &rec, nil
}

func readTrimmedFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
