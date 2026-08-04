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

	"github.com/DGS-ai-team/DAgents/shared/config"
	_ "modernc.org/sqlite"
)

const nodeSettingsSchemaVersion = 1

// NodeSettingsStore 持久化 Node 进程级设置（除 listen/local 外；LLM 档案仍在 llm_configs.db）。
type NodeSettingsStore struct {
	db *sql.DB
}

// OpenNodeSettings 打开或创建 node_settings.db。
func OpenNodeSettings(dbPath string) (*NodeSettingsStore, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		return nil, fmt.Errorf("node settings db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create node settings db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &NodeSettingsStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭连接。
func (s *NodeSettingsStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *NodeSettingsStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS node_settings (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  settings_json TEXT NOT NULL DEFAULT '{}',
  schema_version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL
);
`)
	return err
}

// Empty 表示库中尚无设置行。
func (s *NodeSettingsStore) Empty(ctx context.Context) (bool, error) {
	if s == nil {
		return true, fmt.Errorf("node settings store unavailable")
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM node_settings WHERE id = 1`).Scan(&n)
	if err != nil {
		return true, err
	}
	return n == 0, nil
}

// Load 读取设置快照；空库返回 nil, nil。
func (s *NodeSettingsStore) Load(ctx context.Context) (*config.Config, error) {
	if s == nil {
		return nil, fmt.Errorf("node settings store unavailable")
	}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT settings_json FROM node_settings WHERE id = 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return nil, nil
	}
	var snap config.Config
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, fmt.Errorf("parse node settings: %w", err)
	}
	return &snap, nil
}

// Save 覆盖写入单例设置。
func (s *NodeSettingsStore) Save(ctx context.Context, cfg *config.Config) error {
	if s == nil {
		return fmt.Errorf("node settings store unavailable")
	}
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	snap := SnapshotNodeSettings(cfg)
	data, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.ExecContext(ctx, `
INSERT INTO node_settings (id, settings_json, schema_version, updated_at)
VALUES (1, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  settings_json = excluded.settings_json,
  schema_version = excluded.schema_version,
  updated_at = excluded.updated_at
`, string(data), nodeSettingsSchemaVersion, now)
	return err
}

// SnapshotNodeSettings 抽取应进 SQLite 的字段（去掉 listen/local）。
func SnapshotNodeSettings(cfg *config.Config) config.Config {
	if cfg == nil {
		return config.Config{}
	}
	out := *cfg
	out.Listen = config.ListenConfig{}
	out.Local = config.LocalConfig{}
	out.FSRoot = ""
	// LLM 档案权威在 llm_configs.db；快照不保留 profiles/key。
	out.LLM = config.LLMConfig{
		Active:          cfg.LLM.Active,
		Provider:        cfg.LLM.Provider,
		BaseURL:         cfg.LLM.BaseURL,
		Model:           cfg.LLM.Model,
		Mock:            cfg.LLM.Mock,
		Thinking:        cfg.LLM.Thinking,
		ReasoningEffort: cfg.LLM.ReasoningEffort,
	}
	return out
}

// OverlayNodeSettings 将快照覆盖到 dst（保留 dst 的 listen/local/fs_root）。
func OverlayNodeSettings(dst *config.Config, snap *config.Config) {
	if dst == nil || snap == nil {
		return
	}
	listen := dst.Listen
	local := dst.Local
	fsRoot := dst.FSRoot
	*dst = *snap
	dst.Listen = listen
	dst.Local = local
	dst.FSRoot = fsRoot
	if strings.TrimSpace(dst.FSRoot) == "" {
		dst.FSRoot = config.DefaultFSRoot
	}
}

// ProductNodeSettingsSeed 空库且无 YAML 种子时的开箱默认。
func ProductNodeSettingsSeed() *config.Config {
	ui := true
	bashCompress := true
	dup := true
	toolResult := true
	cfg := &config.Config{
		Agent: config.AgentConfig{
			Name:        "local-assistant",
			Description: "本机智能助手",
			Role:        "ops",
		},
		FSRoot: config.DefaultFSRoot,
		LLM: config.LLMConfig{
			Mock:     true,
			Provider: "mock",
			Model:    "mock",
		},
		Skills: config.SkillsConfig{Enabled: true, MaxInPrompt: 5},
		Compression: config.CompressionConfig{
			SilentTriggerTokens:   80000,
			BlockingTriggerTokens: 100000,
		},
		Triggers:    config.TriggersConfig{Enabled: true, PollSeconds: 5},
		ChildAgents: config.ChildAgentsConfig{Enabled: true},
		Log:         config.LogConfig{Level: "info"},
		UI:          config.UIConfig{Enabled: &ui},
		Tools: config.ToolsConfig{
			BashOutputEncoding: "utf-8",
			BashCompress: config.BashCompressConfig{
				Enabled:              &bashCompress,
				MaxOutputChars:       12000,
				MaxOutputCharsStderr: 16000,
			},
		},
		Hooks: config.HooksConfig{
			DuplicateToolCall: config.DuplicateToolCallHookConfig{
				Enabled:       &dup,
				WindowSeconds: 60,
			},
			ToolResult: config.ToolResultHookConfig{
				Enabled:              &toolResult,
				SpillThresholdTokens: 12000,
				Tools: []string{
					"bash_run", "read_file", "grep_file", "grep_files",
					"search_replace", "glob_files",
				},
			},
		},
	}
	cfg.ApplyDefaults()
	return cfg
}
