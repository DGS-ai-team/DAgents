// Package store 提供 session 对话历史的 SQLite 持久化（N5）。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

// Record 为 session 元数据与消息快照。
type Record struct {
	SessionID        string
	AgentID          string
	Messages         []llm.Message
	LoadedSkills     []skills.LoadedSkill
	RuntimeState     RuntimeState
	FirstUserMessage string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Summary 为列表项摘要。
type Summary struct {
	SessionID        string
	AgentID          string
	MessageCount     int
	FirstUserMessage string
	UpdatedAt        time.Time
}

// SQLiteStore 将会话消息存为单行 JSON 数组。
type SQLiteStore struct {
	db *sql.DB
}

// Open 打开或创建 SQLite 数据库并初始化 schema。
func Open(dbPath string) (*SQLiteStore, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		return nil, fmt.Errorf("db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLiteStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库连接。
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sessions (
  session_id TEXT PRIMARY KEY,
  agent_id TEXT NOT NULL,
  messages_json TEXT NOT NULL DEFAULT '[]',
  first_user_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`)
	if err != nil {
		return err
	}
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN loaded_skills_json TEXT NOT NULL DEFAULT '[]'`)
	_, _ = s.db.Exec(`ALTER TABLE sessions ADD COLUMN runtime_state_json TEXT NOT NULL DEFAULT '{}'`)
	return nil
}

// Save 写入或更新 session 全量消息快照。
func (s *SQLiteStore) Save(ctx context.Context, rec Record) error {
	if strings.TrimSpace(rec.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	raw, err := json.Marshal(rec.Messages)
	if err != nil {
		return err
	}
	skillsRaw, err := json.Marshal(rec.LoadedSkills)
	if err != nil {
		return err
	}
	runtimeRaw, err := json.Marshal(rec.RuntimeState)
	if err != nil {
		return err
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
	first := rec.FirstUserMessage
	if first == "" {
		first = firstUserMessage(rec.Messages)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO sessions(session_id, agent_id, messages_json, loaded_skills_json, runtime_state_json, first_user_message, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id) DO UPDATE SET
  agent_id=excluded.agent_id,
  messages_json=excluded.messages_json,
  loaded_skills_json=excluded.loaded_skills_json,
  runtime_state_json=excluded.runtime_state_json,
  first_user_message=CASE WHEN excluded.first_user_message != '' THEN excluded.first_user_message ELSE sessions.first_user_message END,
  updated_at=excluded.updated_at
`, rec.SessionID, rec.AgentID, string(raw), string(skillsRaw), string(runtimeRaw), first, created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano))
	return err
}

// Load 读取 session；不存在时返回 nil, nil。
func (s *SQLiteStore) Load(ctx context.Context, sessionID string) (*Record, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT session_id, agent_id, messages_json, COALESCE(loaded_skills_json, '[]'), COALESCE(runtime_state_json, '{}'), first_user_message, created_at, updated_at
FROM sessions WHERE session_id = ?
`, sid)
	var rec Record
	var raw, skillsRaw, runtimeRaw string
	var created, updated string
	if err := row.Scan(&rec.SessionID, &rec.AgentID, &raw, &skillsRaw, &runtimeRaw, &rec.FirstUserMessage, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &rec.Messages); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(skillsRaw), &rec.LoadedSkills)
	if rec.LoadedSkills == nil {
		rec.LoadedSkills = []skills.LoadedSkill{}
	}
	_ = json.Unmarshal([]byte(runtimeRaw), &rec.RuntimeState)
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &rec, nil
}

// List 返回全部持久化 session 摘要（按 updated_at 降序）。
func (s *SQLiteStore) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT session_id, agent_id, messages_json, first_user_message, updated_at
FROM sessions ORDER BY updated_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var sum Summary
		var raw, updated string
		if err := rows.Scan(&sum.SessionID, &sum.AgentID, &raw, &sum.FirstUserMessage, &updated); err != nil {
			return nil, err
		}
		var msgs []llm.Message
		_ = json.Unmarshal([]byte(raw), &msgs)
		sum.MessageCount = len(msgs)
		sum.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, sum)
	}
	return out, rows.Err()
}

// Delete 删除 session 行；返回是否删除成功。
func (s *SQLiteStore) Delete(ctx context.Context, sessionID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE session_id = ?`, strings.TrimSpace(sessionID))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClearMessages 清空对话历史但保留 session 行。
func (s *SQLiteStore) ClearMessages(ctx context.Context, sessionID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE sessions SET messages_json = '[]', runtime_state_json = '{}', loaded_skills_json = '[]', updated_at = ? WHERE session_id = ?
`, now, strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func firstUserMessage(messages []llm.Message) string {
	for _, m := range messages {
		if m.Role == "user" && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return ""
}
