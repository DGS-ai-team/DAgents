// Package store 提供 Agent 对话历史的 SQLite 持久化（N5）。
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

// Record 为 Agent 运行时元数据与消息快照。
// AgentID 为对话/Agent 实例 id（主键）；NodeID 为所属 Node 身份（历史列曾误名 agent_id）。
type Record struct {
	AgentID          string
	NodeID           string
	Messages         []llm.Message
	LoadedSkills     []skills.LoadedSkill
	RuntimeState     RuntimeState
	FirstUserMessage string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// Summary 为列表项摘要。
type Summary struct {
	AgentID          string
	NodeID           string
	MessageCount     int
	FirstUserMessage string
	UpdatedAt        time.Time
}

// SQLiteStore 将 Agent 消息存为单行 JSON 数组。
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
CREATE TABLE IF NOT EXISTS agent_runtimes (
  agent_id TEXT PRIMARY KEY,
  node_id TEXT NOT NULL DEFAULT '',
  messages_json TEXT NOT NULL DEFAULT '[]',
  first_user_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  loaded_skills_json TEXT NOT NULL DEFAULT '[]',
  runtime_state_json TEXT NOT NULL DEFAULT '{}'
);
`)
	if err != nil {
		return err
	}
	if err := s.initExecutionEventSchema(); err != nil {
		return err
	}
	if err := s.initTurnEventSchema(); err != nil {
		return err
	}
	if err := s.initChildRunSchema(); err != nil {
		return err
	}
	return nil
}

// Save 写入或更新 Agent 全量消息快照。
func (s *SQLiteStore) Save(ctx context.Context, rec Record) error {
	if strings.TrimSpace(rec.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
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
INSERT INTO agent_runtimes(agent_id, node_id, messages_json, loaded_skills_json, runtime_state_json, first_user_message, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  node_id=excluded.node_id,
  messages_json=excluded.messages_json,
  loaded_skills_json=excluded.loaded_skills_json,
  runtime_state_json=excluded.runtime_state_json,
  first_user_message=CASE WHEN excluded.first_user_message != '' THEN excluded.first_user_message ELSE agent_runtimes.first_user_message END,
  updated_at=excluded.updated_at
`, rec.AgentID, rec.NodeID, string(raw), string(skillsRaw), string(runtimeRaw), first, created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano))
	return err
}

// Load 读取 Agent 运行时；不存在时返回 nil, nil。
func (s *SQLiteStore) Load(ctx context.Context, agentID string) (*Record, error) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, node_id, messages_json, COALESCE(loaded_skills_json, '[]'), COALESCE(runtime_state_json, '{}'), first_user_message, created_at, updated_at
FROM agent_runtimes WHERE agent_id = ?
`, id)
	var rec Record
	var raw, skillsRaw, runtimeRaw string
	var created, updated string
	if err := row.Scan(&rec.AgentID, &rec.NodeID, &raw, &skillsRaw, &runtimeRaw, &rec.FirstUserMessage, &created, &updated); err != nil {
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

// List 返回全部持久化 Agent 摘要（按 updated_at 降序）。
func (s *SQLiteStore) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT agent_id, node_id, messages_json, first_user_message, updated_at
FROM agent_runtimes ORDER BY updated_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var sum Summary
		var raw, updated string
		if err := rows.Scan(&sum.AgentID, &sum.NodeID, &raw, &sum.FirstUserMessage, &updated); err != nil {
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

// Delete 删除 Agent 运行时行；返回是否删除成功。
func (s *SQLiteStore) Delete(ctx context.Context, agentID string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM agent_runtimes WHERE agent_id = ?`, strings.TrimSpace(agentID))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ClearMessages 清空对话历史但保留 Agent 行。
func (s *SQLiteStore) ClearMessages(ctx context.Context, agentID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
UPDATE agent_runtimes SET messages_json = '[]', runtime_state_json = '{}', loaded_skills_json = '[]', updated_at = ? WHERE agent_id = ?
`, now, strings.TrimSpace(agentID))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// BackdateUpdatedAt 显式设置 Agent updated_at（测试与 idle 自动压缩扫描）。
func (s *SQLiteStore) BackdateUpdatedAt(ctx context.Context, agentID string, at time.Time) error {
	now := at.UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `UPDATE agent_runtimes SET updated_at = ? WHERE agent_id = ?`, now, strings.TrimSpace(agentID))
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
		if m.Role != "user" {
			continue
		}
		if summary := strings.TrimSpace(llm.MessageTextSummary(m)); summary != "" {
			return summary
		}
		if llm.MessageHasImages(m) {
			return "[image]"
		}
	}
	return ""
}
