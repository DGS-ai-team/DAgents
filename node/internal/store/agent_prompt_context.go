package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AgentPromptContextRecord stores the editable prompt sidecar for an Agent.
// Memory is persisted by memory.Store and is deliberately not duplicated here.
type AgentPromptContextRecord struct {
	AgentID   string
	SoulMD    string
	CustomMD  string
	UpdatedAt time.Time
}

func (s *AgentStore) ensurePromptContextSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS agent_prompt_context (
  agent_id TEXT PRIMARY KEY,
  soul_md TEXT NOT NULL DEFAULT '',
  custom_md TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
`)
	return err
}

// GetAgentPromptContext reads the editable prompt sidecar.
func (s *AgentStore) GetAgentPromptContext(ctx context.Context, agentID string) (*AgentPromptContextRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, soul_md, custom_md, updated_at
FROM agent_prompt_context WHERE agent_id = ?`, agentID)
	var id, soul, custom, updated string
	if err := row.Scan(&id, &soul, &custom, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ut, _ := time.Parse(time.RFC3339Nano, updated)
	return &AgentPromptContextRecord{
		AgentID: id, SoulMD: soul, CustomMD: custom, UpdatedAt: ut,
	}, nil
}

// SaveAgentPromptContext writes the editable prompt sidecar.
func (s *AgentStore) SaveAgentPromptContext(ctx context.Context, rec AgentPromptContextRecord) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	id := strings.TrimSpace(rec.AgentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	now := time.Now().UTC()
	if !rec.UpdatedAt.IsZero() {
		now = rec.UpdatedAt.UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO agent_prompt_context (
  agent_id, soul_md, custom_md, updated_at
) VALUES (?, ?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  soul_md=excluded.soul_md,
  custom_md=excluded.custom_md,
  updated_at=excluded.updated_at
`, id, rec.SoulMD, rec.CustomMD, now.Format(time.RFC3339Nano))
	return err
}

// DeleteAgentPromptContext deletes the sidecar row.
func (s *AgentStore) DeleteAgentPromptContext(ctx context.Context, agentID string) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_prompt_context WHERE agent_id = ?`, strings.TrimSpace(agentID))
	return err
}
