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
)

// AgentRecord 为 Agent 实例元数据。
type AgentRecord struct {
	AgentID        string
	DisplayName    string
	TemplateID     string
	ConfigSnapshot json.RawMessage
	HostJSON       json.RawMessage // OS / display 快照
	Archived       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// RuntimeRevision is an independent, monotonic revision for runtime-affecting
	// agent configuration. It must not be derived from UpdatedAt: metadata writes
	// and clock precision are not a reliable runtime identity.
	RuntimeRevision int64
}

// AgentStore 持久化 Agent 实例元数据（agents.db）。
type AgentStore struct {
	db *sql.DB
}

// OpenAgents 打开或创建 agents.db。
func OpenAgents(dbPath string) (*AgentStore, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		return nil, fmt.Errorf("agents db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create agents db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &AgentStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭连接。
func (s *AgentStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *AgentStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS agents (
  agent_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  template_id TEXT NOT NULL,
  config_snapshot_json TEXT NOT NULL DEFAULT '{}',
  host_json TEXT NOT NULL DEFAULT '{}',
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  runtime_revision INTEGER NOT NULL DEFAULT 1
);
`)
	if err != nil {
		return err
	}
	if err := s.ensurePolicySchema(); err != nil {
		return err
	}
	return s.ensurePromptContextSchema()
}

// Save 写入或更新 Agent 元数据。
func (s *AgentStore) Save(ctx context.Context, rec AgentRecord) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	id := strings.TrimSpace(rec.AgentID)
	if id == "" {
		return fmt.Errorf("agent_id is required")
	}
	name := strings.TrimSpace(rec.DisplayName)
	if name == "" {
		name = id
	}
	tpl := strings.TrimSpace(rec.TemplateID)
	// template_id 可选；空表示无模板溯源。
	snap := rec.ConfigSnapshot
	if len(snap) == 0 {
		snap = json.RawMessage(`{}`)
	}
	host := rec.HostJSON
	if len(host) == 0 {
		host = json.RawMessage(`{}`)
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
	archived := 0
	if rec.Archived {
		archived = 1
	}
	// Save is the single write boundary for an Agent runtime definition. Bump
	// the revision here, independently of UpdatedAt, so every successful
	// configuration write causes a deterministic next-runtime decision.
	runtimeRevision := int64(1)
	var currentRevision int64
	err := s.db.QueryRowContext(ctx, `SELECT runtime_revision FROM agents WHERE agent_id = ?`, id).Scan(&currentRevision)
	switch err {
	case nil:
		if currentRevision > 0 {
			runtimeRevision = currentRevision + 1
		}
	case sql.ErrNoRows:
	default:
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO agents (
  agent_id, display_name, template_id, config_snapshot_json, host_json,
  archived, created_at, updated_at, runtime_revision
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  display_name=excluded.display_name,
  template_id=excluded.template_id,
  config_snapshot_json=excluded.config_snapshot_json,
  host_json=excluded.host_json,
  archived=excluded.archived,
  updated_at=excluded.updated_at,
  runtime_revision=excluded.runtime_revision
	`, id, name, tpl, string(snap), string(host), archived,
		created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano), runtimeRevision)
	return err
}

// Get 按 id 读取；不存在返回 (nil, nil)。
func (s *AgentStore) Get(ctx context.Context, agentID string) (*AgentRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, display_name, template_id, config_snapshot_json, host_json,
       archived, created_at, updated_at,
       runtime_revision
FROM agents WHERE agent_id = ?`, agentID)
	return scanAgent(row)
}

// List 列出未归档 Agent（按 updated_at 降序）。
func (s *AgentStore) List(ctx context.Context) ([]AgentRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT agent_id, display_name, template_id, config_snapshot_json, host_json,
       archived, created_at, updated_at,
       runtime_revision
FROM agents WHERE archived = 0
ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentRecord
	for rows.Next() {
		rec, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		if rec != nil {
			out = append(out, *rec)
		}
	}
	return out, rows.Err()
}

// SoftDelete 归档 Agent，并删除其 policy / 侧车正文行。
func (s *AgentStore) SoftDelete(ctx context.Context, agentID string) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
UPDATE agents SET archived = 1, updated_at = ? WHERE agent_id = ? AND archived = 0`,
		time.Now().UTC().Format(time.RFC3339Nano), agentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q not found", agentID)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_policy WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("delete agent policy: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_prompt_context WHERE agent_id = ?`, agentID); err != nil {
		return fmt.Errorf("delete agent prompt context: %w", err)
	}
	return tx.Commit()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAgent(row scannable) (*AgentRecord, error) {
	var (
		id, name, tpl, snap, host, created, updated string
		archived, runtimeRevision                   int64
	)
	if err := row.Scan(&id, &name, &tpl, &snap, &host, &archived, &created, &updated, &runtimeRevision); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ct, _ := time.Parse(time.RFC3339Nano, created)
	ut, _ := time.Parse(time.RFC3339Nano, updated)
	if strings.TrimSpace(host) == "" {
		host = "{}"
	}
	return &AgentRecord{
		AgentID:         id,
		DisplayName:     name,
		TemplateID:      tpl,
		ConfigSnapshot:  json.RawMessage(snap),
		HostJSON:        json.RawMessage(host),
		Archived:        archived != 0,
		CreatedAt:       ct,
		UpdatedAt:       ut,
		RuntimeRevision: runtimeRevision,
	}, nil
}
