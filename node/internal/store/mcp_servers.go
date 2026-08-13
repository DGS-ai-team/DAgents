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

	"github.com/DGS-ai-team/DAgents/node/internal/mcp"
	_ "modernc.org/sqlite"
)

type MCPServerStore struct{ db *sql.DB }

type storedMCPServerConfig struct {
	ID           string            `json:"id"`
	DisplayName  string            `json:"display_name"`
	Transport    string            `json:"transport"`
	Command      string            `json:"command"`
	Args         []string          `json:"args,omitempty"`
	CWD          string            `json:"cwd,omitempty"`
	URL          string            `json:"url,omitempty"`
	EnvRefs      map[string]string `json:"env_refs,omitempty"`
	HeaderRefs   map[string]string `json:"header_refs,omitempty"`
	EnvValues    map[string]string `json:"env_values,omitempty"`
	HeaderValues map[string]string `json:"header_values,omitempty"`
	EnabledTools []string          `json:"enabled_tools,omitempty"`
	Enabled      bool              `json:"enabled"`
}

func encodeMCPServerConfig(cfg mcp.ServerConfig) ([]byte, error) {
	return json.Marshal(storedMCPServerConfig{
		ID: cfg.ID, DisplayName: cfg.DisplayName, Transport: cfg.Transport,
		Command: cfg.Command, Args: cfg.Args, CWD: cfg.CWD, URL: cfg.URL,
		EnvRefs: cfg.EnvRefs, HeaderRefs: cfg.HeaderRefs, EnvValues: cfg.EnvValues,
		HeaderValues: cfg.HeaderValues, EnabledTools: cfg.EnabledTools, Enabled: cfg.Enabled,
	})
}

func decodeMCPServerConfig(raw string) (mcp.ServerConfig, error) {
	var stored storedMCPServerConfig
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return mcp.ServerConfig{}, err
	}
	return mcp.ServerConfig{
		ID: stored.ID, DisplayName: stored.DisplayName, Transport: stored.Transport,
		Command: stored.Command, Args: stored.Args, CWD: stored.CWD, URL: stored.URL,
		EnvRefs: stored.EnvRefs, HeaderRefs: stored.HeaderRefs, EnvValues: stored.EnvValues,
		HeaderValues: stored.HeaderValues, EnabledTools: stored.EnabledTools, Enabled: stored.Enabled,
	}, nil
}

func OpenMCPServers(dbPath string) (*MCPServerStore, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		return nil, fmt.Errorf("mcp servers db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create mcp servers db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &MCPServerStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *MCPServerStore) initSchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS mcp_servers (
  server_id TEXT PRIMARY KEY,
  config_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);`)
	return err
}

func (s *MCPServerStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *MCPServerStore) List(ctx context.Context) ([]mcp.ServerConfig, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("mcp server store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT config_json FROM mcp_servers ORDER BY server_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []mcp.ServerConfig
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		cfg, err := decodeMCPServerConfig(raw)
		if err != nil {
			return nil, fmt.Errorf("decode mcp server: %w", err)
		}
		validated, err := mcp.ValidateServerConfig(cfg)
		if err != nil {
			return nil, err
		}
		out = append(out, validated)
	}
	return out, rows.Err()
}

func (s *MCPServerStore) Get(ctx context.Context, id string) (*mcp.ServerConfig, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("mcp server store unavailable")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT config_json FROM mcp_servers WHERE server_id = ?`, strings.TrimSpace(id)).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg, err := decodeMCPServerConfig(raw)
	if err != nil {
		return nil, err
	}
	validated, err := mcp.ValidateServerConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &validated, nil
}

func (s *MCPServerStore) Save(ctx context.Context, raw mcp.ServerConfig) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("mcp server store unavailable")
	}
	cfg, err := mcp.ValidateServerConfig(raw)
	if err != nil {
		return err
	}
	data, err := encodeMCPServerConfig(cfg)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `INSERT INTO mcp_servers(server_id, config_json, created_at, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(server_id) DO UPDATE SET config_json=excluded.config_json, updated_at=excluded.updated_at`,
		cfg.ID, string(data), now, now)
	return err
}

// Replace atomically replaces the complete set of MCP server configurations.
func (s *MCPServerStore) Replace(ctx context.Context, configs []mcp.ServerConfig) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("mcp server store unavailable")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_servers`); err != nil {
		return err
	}
	for _, raw := range configs {
		cfg, err := mcp.ValidateServerConfig(raw)
		if err != nil {
			return err
		}
		data, err := encodeMCPServerConfig(cfg)
		if err != nil {
			return err
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_servers(server_id, config_json, created_at, updated_at) VALUES (?, ?, ?, ?)`, cfg.ID, string(data), now, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *MCPServerStore) Delete(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("mcp server store unavailable")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM mcp_servers WHERE server_id = ?`, strings.TrimSpace(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("mcp server %q not found", id)
	}
	return nil
}
