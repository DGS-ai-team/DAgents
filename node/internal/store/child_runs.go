package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ChildRunRecord 是临时子 Agent 的轻量、可恢复状态快照。
// 子 Agent 的完整 transcript 仍属于内部运行时，不复制到父会话；这里只
// 保存恢复 UI、审批路由和终态结果所需的控制面事实。
type ChildRunRecord struct {
	ChildAgentID  string
	ParentAgentID string
	NodeID        string
	ToolCallID    string
	Purpose       string
	Status        string
	Phase         string
	AllowedTools  []string
	LoadedSkills  []string
	Progress      json.RawMessage
	TurnCount     int
	MaxTurns      int
	Summary       string
	Error         string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	UpdatedAt     time.Time
	FinishedAt    time.Time
	Revision      uint64
}

func (s *SQLiteStore) initChildRunSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS child_runs (
  child_agent_id TEXT PRIMARY KEY,
  parent_agent_id TEXT NOT NULL,
  node_id TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  purpose TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  phase TEXT NOT NULL DEFAULT '',
  allowed_tools_json TEXT NOT NULL DEFAULT '[]',
  loaded_skills_json TEXT NOT NULL DEFAULT '[]',
  progress_json TEXT NOT NULL DEFAULT '{}',
  turn_count INTEGER NOT NULL DEFAULT 0,
  max_turns INTEGER NOT NULL DEFAULT 0,
  summary TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  finished_at TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_child_runs_parent_updated
  ON child_runs(parent_agent_id, updated_at DESC);
`)
	return err
}

// SaveChildRun 以 child_agent_id 幂等保存一份最新快照。
func (s *SQLiteStore) SaveChildRun(ctx context.Context, rec ChildRunRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is nil")
	}
	if strings.TrimSpace(rec.ChildAgentID) == "" {
		return fmt.Errorf("child_agent_id is required")
	}
	if strings.TrimSpace(rec.ParentAgentID) == "" {
		return fmt.Errorf("parent_agent_id is required")
	}
	allowed, err := json.Marshal(rec.AllowedTools)
	if err != nil {
		return err
	}
	loaded, err := json.Marshal(rec.LoadedSkills)
	if err != nil {
		return err
	}
	progress := rec.Progress
	if len(progress) == 0 {
		progress = json.RawMessage(`{}`)
	}
	if !json.Valid(progress) {
		return fmt.Errorf("child progress is invalid JSON")
	}
	now := time.Now().UTC()
	created := rec.CreatedAt
	if created.IsZero() {
		created = now
	}
	updated := rec.UpdatedAt
	if updated.IsZero() {
		updated = now
	}
	finished := ""
	if !rec.FinishedAt.IsZero() {
		finished = rec.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO child_runs(
  child_agent_id, parent_agent_id, node_id, tool_call_id, purpose, status,
  phase, allowed_tools_json, loaded_skills_json, progress_json, turn_count,
  max_turns, summary, error, created_at, expires_at, updated_at, finished_at,
  revision
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(child_agent_id) DO UPDATE SET
  parent_agent_id=excluded.parent_agent_id,
  node_id=excluded.node_id,
  tool_call_id=excluded.tool_call_id,
  purpose=excluded.purpose,
  status=excluded.status,
  phase=excluded.phase,
  allowed_tools_json=excluded.allowed_tools_json,
  loaded_skills_json=excluded.loaded_skills_json,
  progress_json=excluded.progress_json,
  turn_count=excluded.turn_count,
  max_turns=excluded.max_turns,
  summary=excluded.summary,
  error=excluded.error,
  expires_at=excluded.expires_at,
  updated_at=excluded.updated_at,
  finished_at=excluded.finished_at,
  revision=excluded.revision`,
		rec.ChildAgentID, rec.ParentAgentID, rec.NodeID, rec.ToolCallID, rec.Purpose,
		rec.Status, rec.Phase, string(allowed), string(loaded), string(progress),
		rec.TurnCount, rec.MaxTurns, rec.Summary, rec.Error,
		created.UTC().Format(time.RFC3339Nano), rec.ExpiresAt.UTC().Format(time.RFC3339Nano),
		updated.UTC().Format(time.RFC3339Nano), finished, rec.Revision)
	return err
}

// LoadChildRun 读取一条子 Agent 快照。
func (s *SQLiteStore) LoadChildRun(ctx context.Context, childAgentID string) (*ChildRunRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is nil")
	}
	var rec ChildRunRecord
	var allowed, loaded, progress, created, expires, updated, finished string
	err := s.db.QueryRowContext(ctx, `
SELECT child_agent_id, parent_agent_id, node_id, tool_call_id, purpose, status,
       phase, allowed_tools_json, loaded_skills_json, progress_json, turn_count,
       max_turns, summary, error, created_at, expires_at, updated_at, finished_at,
       revision
FROM child_runs WHERE child_agent_id = ?`, strings.TrimSpace(childAgentID)).Scan(
		&rec.ChildAgentID, &rec.ParentAgentID, &rec.NodeID, &rec.ToolCallID,
		&rec.Purpose, &rec.Status, &rec.Phase, &allowed, &loaded, &progress,
		&rec.TurnCount, &rec.MaxTurns, &rec.Summary, &rec.Error, &created,
		&expires, &updated, &finished, &rec.Revision)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(allowed), &rec.AllowedTools)
	_ = json.Unmarshal([]byte(loaded), &rec.LoadedSkills)
	rec.Progress = json.RawMessage(progress)
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	rec.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if strings.TrimSpace(finished) != "" {
		rec.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
	}
	return &rec, nil
}

// ListChildRuns 返回父 Agent 最近的子 Agent 快照；parentAgentID 为空时返回全表。
func (s *SQLiteStore) ListChildRuns(ctx context.Context, parentAgentID string, limit int) ([]ChildRunRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is nil")
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	query := `
SELECT child_agent_id, parent_agent_id, node_id, tool_call_id, purpose, status,
       phase, allowed_tools_json, loaded_skills_json, progress_json, turn_count,
       max_turns, summary, error, created_at, expires_at, updated_at, finished_at,
       revision
FROM child_runs`
	args := []any{}
	if strings.TrimSpace(parentAgentID) != "" {
		query += ` WHERE parent_agent_id = ?`
		args = append(args, strings.TrimSpace(parentAgentID))
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChildRunRecord
	for rows.Next() {
		var rec ChildRunRecord
		var allowed, loaded, progress, created, expires, updated, finished string
		if err := rows.Scan(
			&rec.ChildAgentID, &rec.ParentAgentID, &rec.NodeID, &rec.ToolCallID,
			&rec.Purpose, &rec.Status, &rec.Phase, &allowed, &loaded, &progress,
			&rec.TurnCount, &rec.MaxTurns, &rec.Summary, &rec.Error, &created,
			&expires, &updated, &finished, &rec.Revision,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(allowed), &rec.AllowedTools)
		_ = json.Unmarshal([]byte(loaded), &rec.LoadedSkills)
		rec.Progress = json.RawMessage(progress)
		rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		rec.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expires)
		rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		if strings.TrimSpace(finished) != "" {
			rec.FinishedAt, _ = time.Parse(time.RFC3339Nano, finished)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}
