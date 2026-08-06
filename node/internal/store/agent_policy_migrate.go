package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

// ListAgentPolicies 返回全部按 Agent 持久化的策略行。
func (s *AgentStore) ListAgentPolicies(ctx context.Context) ([]AgentPolicyRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT agent_id, policy_json, updated_at FROM agent_policy`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AgentPolicyRecord, 0)
	for rows.Next() {
		var id, raw, updated string
		if err := rows.Scan(&id, &raw, &updated); err != nil {
			return nil, err
		}
		var parsed agentPolicyJSON
		if strings.TrimSpace(raw) != "" {
			if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
				return nil, fmt.Errorf("parse agent policy %q: %w", id, err)
			}
		}
		if parsed.Tools == nil {
			parsed.Tools = map[string]string{}
		}
		if parsed.Shell == nil {
			parsed.Shell = map[string]map[string]string{}
		}
		ut, _ := time.Parse(time.RFC3339Nano, updated)
		out = append(out, AgentPolicyRecord{
			AgentID:   id,
			Tools:     parsed.Tools,
			Shell:     parsed.Shell,
			UpdatedAt: ut,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AgentPolicySeedMergeResult 为存量策略种子缺项合并结果。
type AgentPolicySeedMergeResult struct {
	AgentsTouched int
	ToolsAdded    int
}

// MigrateAgentPoliciesMergeSeed 将 packaging 种子中缺失的工具模式合并进全部存量 agent_policy。
// 不覆盖用户已配置的模式；幂等，可在每次 Node 启动时调用。
func (s *AgentStore) MigrateAgentPoliciesMergeSeed(ctx context.Context) (AgentPolicySeedMergeResult, error) {
	var result AgentPolicySeedMergeResult
	if s == nil {
		return result, fmt.Errorf("agent store unavailable")
	}
	seed := policy.LoadSeedMaps()
	if len(seed.Tools) == 0 {
		return result, nil
	}
	rows, err := s.ListAgentPolicies(ctx)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	for _, rec := range rows {
		tools, added := policy.MergeMissingToolModes(rec.Tools, seed.Tools)
		if added == 0 {
			continue
		}
		rec.Tools = tools
		rec.UpdatedAt = now
		if err := s.SaveAgentPolicy(ctx, rec); err != nil {
			return result, fmt.Errorf("merge seed into agent %q: %w", rec.AgentID, err)
		}
		result.AgentsTouched++
		result.ToolsAdded += added
	}
	return result, nil
}

// mergeSeedToolsIntoRecord 若 rec 缺少种子工具项则就地补齐并返回是否有变更。
func mergeSeedToolsIntoRecord(rec *AgentPolicyRecord) bool {
	if rec == nil {
		return false
	}
	seed := policy.LoadSeedMaps()
	if len(seed.Tools) == 0 {
		return false
	}
	tools, added := policy.MergeMissingToolModes(rec.Tools, seed.Tools)
	if added == 0 {
		return false
	}
	rec.Tools = tools
	rec.UpdatedAt = time.Now().UTC()
	return true
}
