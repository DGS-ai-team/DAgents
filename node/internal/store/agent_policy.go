package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// AgentPolicyRecord 为按 Agent 持久化的工具/shell 策略（JSON）。
type AgentPolicyRecord struct {
	AgentID   string
	Tools     map[string]string            // tool_name → always|never|rule|deny
	Shell     map[string]map[string]string // shell_type → command → mode
	UpdatedAt time.Time
}

type agentPolicyJSON struct {
	Tools map[string]string            `json:"tools"`
	Shell map[string]map[string]string `json:"shell"`
}

func (s *AgentStore) ensurePolicySchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS agent_policy (
  agent_id TEXT PRIMARY KEY,
  policy_json TEXT NOT NULL DEFAULT '{}',
  updated_at TEXT NOT NULL
);
`)
	return err
}

// GetAgentPolicy 读取 Agent 策略；不存在返回 (nil, nil)。
func (s *AgentStore) GetAgentPolicy(ctx context.Context, agentID string) (*AgentPolicyRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, policy_json, updated_at FROM agent_policy WHERE agent_id = ?`, agentID)
	var id, raw, updated string
	if err := row.Scan(&id, &raw, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	var parsed agentPolicyJSON
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, fmt.Errorf("parse agent policy: %w", err)
		}
	}
	if parsed.Tools == nil {
		parsed.Tools = map[string]string{}
	}
	if parsed.Shell == nil {
		parsed.Shell = map[string]map[string]string{}
	}
	ut, _ := time.Parse(time.RFC3339Nano, updated)
	return &AgentPolicyRecord{
		AgentID:   id,
		Tools:     parsed.Tools,
		Shell:     parsed.Shell,
		UpdatedAt: ut,
	}, nil
}

// SaveAgentPolicy 写入或覆盖 Agent 策略。
func (s *AgentStore) SaveAgentPolicy(ctx context.Context, rec AgentPolicyRecord) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	id := strings.TrimSpace(rec.AgentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	tools := rec.Tools
	if tools == nil {
		tools = map[string]string{}
	}
	shell := rec.Shell
	if shell == nil {
		shell = map[string]map[string]string{}
	}
	payload, err := json.Marshal(agentPolicyJSON{Tools: tools, Shell: shell})
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if !rec.UpdatedAt.IsZero() {
		now = rec.UpdatedAt.UTC()
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO agent_policy (agent_id, policy_json, updated_at) VALUES (?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  policy_json=excluded.policy_json,
  updated_at=excluded.updated_at
`, id, string(payload), now.Format(time.RFC3339Nano))
	return err
}

// DeleteAgentPolicy 删除 Agent 策略行。
func (s *AgentStore) DeleteAgentPolicy(ctx context.Context, agentID string) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_policy WHERE agent_id = ?`, strings.TrimSpace(agentID))
	return err
}
