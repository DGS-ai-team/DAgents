package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// AgentPromptContextRecord 为按 Agent 持久化的侧车 Markdown 与长期记忆正文。
type AgentPromptContextRecord struct {
	AgentID    string
	SoulMD     string
	UserMD     string
	CustomMD   string
	LongTermMD string
	UpdatedAt  time.Time
}

func (s *AgentStore) ensurePromptContextSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS agent_prompt_context (
  agent_id TEXT PRIMARY KEY,
  soul_md TEXT NOT NULL DEFAULT '',
  user_md TEXT NOT NULL DEFAULT '',
  custom_md TEXT NOT NULL DEFAULT '',
  long_term_md TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL
);
`)
	return err
}

// GetAgentPromptContext 读取侧车正文；不存在返回 (nil, nil)。
func (s *AgentStore) GetAgentPromptContext(ctx context.Context, agentID string) (*AgentPromptContextRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, soul_md, user_md, custom_md, long_term_md, updated_at
FROM agent_prompt_context WHERE agent_id = ?`, agentID)
	var id, soul, user, custom, longTerm, updated string
	if err := row.Scan(&id, &soul, &user, &custom, &longTerm, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ut, _ := time.Parse(time.RFC3339Nano, updated)
	return &AgentPromptContextRecord{
		AgentID:    id,
		SoulMD:     soul,
		UserMD:     user,
		CustomMD:   custom,
		LongTermMD: longTerm,
		UpdatedAt:  ut,
	}, nil
}

// SaveAgentPromptContext 写入或覆盖侧车正文。
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
  agent_id, soul_md, user_md, custom_md, long_term_md, updated_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  soul_md=excluded.soul_md,
  user_md=excluded.user_md,
  custom_md=excluded.custom_md,
  long_term_md=excluded.long_term_md,
  updated_at=excluded.updated_at
`, id, rec.SoulMD, rec.UserMD, rec.CustomMD, rec.LongTermMD, now.Format(time.RFC3339Nano))
	return err
}

// SaveAgentPromptContextMetadata updates only the non-memory sidecar fields.
// When Memory v2 is active, long_term_md is a read-only compatibility
// projection and must not be written back to agents.db.
func (s *AgentStore) SaveAgentPromptContextMetadata(ctx context.Context, rec AgentPromptContextRecord) error {
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
  agent_id, soul_md, user_md, custom_md, long_term_md, updated_at
) VALUES (?, ?, ?, ?, '', ?)
ON CONFLICT(agent_id) DO UPDATE SET
  soul_md=excluded.soul_md,
  user_md=excluded.user_md,
  custom_md=excluded.custom_md,
  updated_at=excluded.updated_at
`, id, rec.SoulMD, rec.UserMD, rec.CustomMD, now.Format(time.RFC3339Nano))
	return err
}

// UpdateAgentLongTermCAS 在 updated_at 与 expectedUpdatedAt 一致时更新 long_term_md。
// 返回 updated=true 表示写入成功；版本不匹配时返回 (false, nil)。
func (s *AgentStore) UpdateAgentLongTermCAS(ctx context.Context, agentID, longTerm string, expectedUpdatedAt time.Time) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return false, fmt.Errorf("agent_id is required")
	}
	now := time.Now().UTC()
	expected := expectedUpdatedAt.UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE agent_prompt_context
SET long_term_md = ?, updated_at = ?
WHERE agent_id = ? AND updated_at = ?`,
		longTerm, now.Format(time.RFC3339Nano), agentID, expected)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeleteAgentPromptContext 删除侧车正文行。
func (s *AgentStore) DeleteAgentPromptContext(ctx context.Context, agentID string) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_prompt_context WHERE agent_id = ?`, strings.TrimSpace(agentID))
	return err
}
