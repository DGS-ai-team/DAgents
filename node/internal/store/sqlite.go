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
	AgentID                string
	NodeID                 string
	Messages               []llm.Message
	LoadedSkills           []skills.LoadedSkill
	RuntimeState           RuntimeState
	FirstUserMessage       string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	AcceptedInputReceipt   *InputReceipt
	InputQueueRows         []InputQueueRow
	CompletedInputSeq      uint64
	CompletedInputRevision uint64
	ResubmitGuard          *ConversationResubmitGuard
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
	if err := s.initTurnEventSchema(); err != nil {
		return err
	}
	if err := s.initChildRunSchema(); err != nil {
		return err
	}
	if err := s.initConversationInputSchema(); err != nil {
		return err
	}
	return s.migrateConversationStorage(context.Background())
}

// Save writes runtime metadata only. Message payloads are intentionally ignored;
// transcript changes must use PersistRuntimeDelta with explicit item IDs.
func (s *SQLiteStore) Save(ctx context.Context, rec Record) error {
	_, err := s.PersistRuntimeDelta(ctx, rec, nil)
	return err
}

// SaveConversationSnapshot is a one-shot legacy/import helper. Runtime code
// must use Save for metadata or PersistRuntimeDelta for explicit content deltas.
func (s *SQLiteStore) SaveConversationSnapshot(ctx context.Context, rec Record) error {
	if strings.TrimSpace(rec.AgentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	// messages_json is frozen migration input. From schema v1 onward, history
	// records and the model checkpoint are committed together below.
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `
INSERT INTO agent_runtimes(agent_id, node_id, messages_json, loaded_skills_json, runtime_state_json, first_user_message, created_at, updated_at)
VALUES (?, ?, '[]', ?, ?, ?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  node_id=excluded.node_id,
  loaded_skills_json=excluded.loaded_skills_json,
  runtime_state_json=excluded.runtime_state_json,
  first_user_message=CASE WHEN excluded.first_user_message != '' THEN excluded.first_user_message ELSE agent_runtimes.first_user_message END,
  updated_at=excluded.updated_at
`, rec.AgentID, rec.NodeID, string(skillsRaw), string(runtimeRaw), first, created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	if err := saveConversationSnapshotTx(ctx, tx, rec); err != nil {
		return err
	}
	return tx.Commit()
}

// Load 读取 Agent 运行时；不存在时返回 nil, nil。
func (s *SQLiteStore) Load(ctx context.Context, agentID string) (*Record, error) {
	id := strings.TrimSpace(agentID)
	if id == "" {
		return nil, fmt.Errorf("agent_id is required")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, node_id, COALESCE(loaded_skills_json, '[]'), COALESCE(runtime_state_json, '{}'), first_user_message, created_at, updated_at
FROM agent_runtimes WHERE agent_id = ?
`, id)
	var rec Record
	var skillsRaw, runtimeRaw string
	var created, updated string
	if err := row.Scan(&rec.AgentID, &rec.NodeID, &skillsRaw, &runtimeRaw, &rec.FirstUserMessage, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	// The checkpoint, not the frozen messages_json migration source, is the
	// authoritative model working set after conversation schema v1.
	_ = json.Unmarshal([]byte(skillsRaw), &rec.LoadedSkills)
	if rec.LoadedSkills == nil {
		rec.LoadedSkills = []skills.LoadedSkill{}
	}
	_ = json.Unmarshal([]byte(runtimeRaw), &rec.RuntimeState)
	checkpoint, err := loadConversationCheckpointTx(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if checkpoint != nil {
		rec.Messages = checkpoint.Messages
		rec.RuntimeState.ContextCheckpointRevision = checkpoint.Revision
		rec.RuntimeState.ActiveContextStart = 0
	}
	_ = s.db.QueryRowContext(ctx, `SELECT revision FROM conversations WHERE conversation_id=?`, id).Scan(&rec.RuntimeState.ConversationRevision)
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return &rec, nil
}

// List 返回全部持久化 Agent 摘要（按 updated_at 降序）。
func (s *SQLiteStore) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT r.agent_id, r.node_id, (SELECT COUNT(*) FROM conversation_records c WHERE c.conversation_id=r.agent_id), r.first_user_message, r.updated_at
FROM agent_runtimes r ORDER BY r.updated_at DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var sum Summary
		var updated string
		if err := rows.Scan(&sum.AgentID, &sum.NodeID, &sum.MessageCount, &sum.FirstUserMessage, &updated); err != nil {
			return nil, err
		}
		sum.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, sum)
	}
	return out, rows.Err()
}

// Delete 删除 Agent 运行时行；返回是否删除成功。
func (s *SQLiteStore) Delete(ctx context.Context, agentID string) (bool, error) {
	id := strings.TrimSpace(agentID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, table := range []string{"conversation_input_queue", "conversation_inputs", "conversation_records", "context_checkpoints", "conversation_rounds", "conversations"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE conversation_id=?`, id); err != nil {
			return false, err
		}
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM agent_runtimes WHERE agent_id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// ClearMessages 清空对话历史但保留 Agent 行。
func (s *SQLiteStore) ClearMessages(ctx context.Context, agentID string) error {
	return s.ClearMessagesWithRuntimeState(ctx, agentID, RuntimeState{})
}

// ClearMessagesWithRuntimeState atomically clears a transcript and writes the
// non-lifecycle state needed to keep its presentation boundary across restart.
func (s *SQLiteStore) ClearMessagesWithRuntimeState(ctx context.Context, agentID string, state RuntimeState) error {
	if strings.TrimSpace(agentID) == "" {
		return fmt.Errorf("agent_id is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	id := strings.TrimSpace(agentID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_records WHERE conversation_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_inputs WHERE conversation_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_input_queue WHERE conversation_id=?`, id); err != nil {
		return err
	}
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM context_checkpoints WHERE conversation_id=?`, id).Scan(&revision); err != nil {
		return err
	}
	var nodeID string
	if err := tx.QueryRowContext(ctx, `SELECT node_id FROM agent_runtimes WHERE agent_id=?`, id).Scan(&nodeID); err != nil {
		return err
	}
	state.ContextCheckpointRevision = revision
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM conversations WHERE conversation_id=?),0)+1`, id).Scan(&state.ConversationRevision); err != nil {
		return err
	}
	runtimeRaw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE agent_runtimes SET runtime_state_json = ?, loaded_skills_json = '[]', first_user_message = '', updated_at = ? WHERE agent_id = ?`, string(runtimeRaw), now, id)
	if err != nil {
		return err
	}
	if count, _ := res.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES(?,?,'[]','clear',0,?)`, id, revision, stamp); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM context_checkpoints WHERE conversation_id=? AND revision<>?`, id, revision); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversations(conversation_id,node_id,revision,current_checkpoint_revision,lifecycle_covered_seq,created_at,updated_at) VALUES(?,?,?,?,0,?,?) ON CONFLICT(conversation_id) DO UPDATE SET revision=excluded.revision,current_checkpoint_revision=excluded.current_checkpoint_revision,lifecycle_covered_seq=0,updated_at=excluded.updated_at`, id, nodeID, state.ConversationRevision, revision, stamp, stamp); err != nil {
		return err
	}
	return tx.Commit()
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
