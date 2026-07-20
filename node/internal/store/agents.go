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
	SandboxEnabled bool
	SandboxBackend string
	ConfigSnapshot json.RawMessage
	Archived       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
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
  sandbox_enabled INTEGER NOT NULL DEFAULT 0,
  sandbox_backend TEXT NOT NULL DEFAULT 'process',
  config_snapshot_json TEXT NOT NULL DEFAULT '{}',
  archived INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
`)
	return err
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
	if tpl == "" {
		return fmt.Errorf("template_id is required")
	}
	backend := strings.TrimSpace(rec.SandboxBackend)
	if backend == "" {
		backend = "process"
	}
	snap := rec.ConfigSnapshot
	if len(snap) == 0 {
		snap = json.RawMessage(`{}`)
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
	sandbox := 0
	if rec.SandboxEnabled {
		sandbox = 1
	}
	archived := 0
	if rec.Archived {
		archived = 1
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO agents (
  agent_id, display_name, template_id, sandbox_enabled, sandbox_backend,
  config_snapshot_json, archived, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_id) DO UPDATE SET
  display_name=excluded.display_name,
  template_id=excluded.template_id,
  sandbox_enabled=excluded.sandbox_enabled,
  sandbox_backend=excluded.sandbox_backend,
  config_snapshot_json=excluded.config_snapshot_json,
  archived=excluded.archived,
  updated_at=excluded.updated_at
`, id, name, tpl, sandbox, backend, string(snap), archived,
		created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano))
	return err
}

// Get 按 id 读取；不存在返回 (nil, nil)。
func (s *AgentStore) Get(ctx context.Context, agentID string) (*AgentRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, display_name, template_id, sandbox_enabled, sandbox_backend,
       config_snapshot_json, archived, created_at, updated_at
FROM agents WHERE agent_id = ?`, agentID)
	return scanAgent(row)
}

// List 列出未归档 Agent（按 updated_at 降序）。
func (s *AgentStore) List(ctx context.Context) ([]AgentRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT agent_id, display_name, template_id, sandbox_enabled, sandbox_backend,
       config_snapshot_json, archived, created_at, updated_at
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

// SoftDelete 归档 Agent。
func (s *AgentStore) SoftDelete(ctx context.Context, agentID string) error {
	if s == nil {
		return fmt.Errorf("agent store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	res, err := s.db.ExecContext(ctx, `
UPDATE agents SET archived = 1, updated_at = ? WHERE agent_id = ? AND archived = 0`,
		time.Now().UTC().Format(time.RFC3339Nano), agentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("agent %q not found", agentID)
	}
	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAgent(row scannable) (*AgentRecord, error) {
	var (
		id, name, tpl, backend, snap, created, updated string
		sandbox, archived                              int
	)
	if err := row.Scan(&id, &name, &tpl, &sandbox, &backend, &snap, &archived, &created, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	ct, _ := time.Parse(time.RFC3339Nano, created)
	ut, _ := time.Parse(time.RFC3339Nano, updated)
	return &AgentRecord{
		AgentID:        id,
		DisplayName:    name,
		TemplateID:     tpl,
		SandboxEnabled: sandbox != 0,
		SandboxBackend: backend,
		ConfigSnapshot: json.RawMessage(snap),
		Archived:       archived != 0,
		CreatedAt:      ct,
		UpdatedAt:      ut,
	}, nil
}
