package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/tools"
	_ "modernc.org/sqlite"
)

// LinuxChannelRecord stores connection metadata only. Authentication material
// is represented by CredentialID and is resolved at execution time.
type LinuxChannelRecord struct {
	ChannelID        string
	DisplayName      string
	Host             string
	Port             int
	Username         string
	CredentialID     string
	HostKeyPolicy    string
	HostKeyRef       string
	RemoteShell      string
	DefaultCWD       string
	ConnectTimeoutMS int
	CommandTimeoutMS int
	KeepaliveSeconds int
	MaxSessions      int
	Enabled          bool
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// LinuxCredentialRecord stores an opaque secret reference, never a password
// or private-key body. Secret resolution is intentionally outside this store.
type LinuxCredentialRecord struct {
	CredentialID string
	DisplayName  string
	AuthType     string
	SecretRef    string
	UsernameHint string
	Enabled      bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type LinuxChannelBindingRecord struct {
	AgentID         string
	ChannelID       string
	Enabled         bool
	IsDefault       bool
	RemoteCWD       string
	Shell           string
	MaxConcurrency  int
	ApprovalMode    string
	AllowedCommands []string
	DeniedCommands  []string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type LinuxChannelStore struct{ db *sql.DB }

func OpenLinuxChannels(dbPath string) (*LinuxChannelStore, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		return nil, fmt.Errorf("linux channels db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create linux channels db dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &LinuxChannelStore{db: db}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *LinuxChannelStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS linux_channels (
  channel_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  host TEXT NOT NULL,
  port INTEGER NOT NULL DEFAULT 22,
  username TEXT NOT NULL,
  credential_id TEXT NOT NULL,
  host_key_policy TEXT NOT NULL DEFAULT 'known_hosts',
  host_key_ref TEXT NOT NULL DEFAULT '',
  remote_shell TEXT NOT NULL DEFAULT 'bash',
  default_cwd TEXT NOT NULL DEFAULT '',
  connect_timeout_ms INTEGER NOT NULL DEFAULT 10000,
  command_timeout_ms INTEGER NOT NULL DEFAULT 120000,
  keepalive_seconds INTEGER NOT NULL DEFAULT 30,
  max_sessions INTEGER NOT NULL DEFAULT 4,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS linux_credentials (
  credential_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  auth_type TEXT NOT NULL,
  secret_ref TEXT NOT NULL,
  username_hint TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_linux_channels_enabled ON linux_channels(enabled, channel_id);
CREATE INDEX IF NOT EXISTS idx_linux_credentials_enabled ON linux_credentials(enabled, credential_id);
CREATE TABLE IF NOT EXISTS agent_linux_channels (
  agent_id TEXT NOT NULL,
  channel_id TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  is_default INTEGER NOT NULL DEFAULT 0,
  remote_cwd TEXT NOT NULL DEFAULT '',
  shell TEXT NOT NULL DEFAULT '',
  max_concurrency INTEGER NOT NULL DEFAULT 1,
  approval_mode TEXT NOT NULL DEFAULT 'require_approval',
  allowed_commands_json TEXT NOT NULL DEFAULT '[]',
  denied_commands_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(agent_id, channel_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_linux_channels_agent ON agent_linux_channels(agent_id, enabled, channel_id);
`)
	return err
}

func (s *LinuxChannelStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *LinuxChannelStore) ListChannels(ctx context.Context) ([]LinuxChannelRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("linux channel store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT channel_id, display_name, host, port, username, credential_id,
       host_key_policy, host_key_ref, remote_shell, default_cwd,
       connect_timeout_ms, command_timeout_ms, keepalive_seconds, max_sessions,
       enabled, created_at, updated_at
FROM linux_channels ORDER BY channel_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinuxChannelRecord
	for rows.Next() {
		rec, err := scanLinuxChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *LinuxChannelStore) GetChannel(ctx context.Context, id string) (*LinuxChannelRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("linux channel store unavailable")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT channel_id, display_name, host, port, username, credential_id,
       host_key_policy, host_key_ref, remote_shell, default_cwd,
       connect_timeout_ms, command_timeout_ms, keepalive_seconds, max_sessions,
       enabled, created_at, updated_at
FROM linux_channels WHERE channel_id = ?`, strings.TrimSpace(id))
	rec, err := scanLinuxChannel(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *LinuxChannelStore) SaveChannel(ctx context.Context, rec LinuxChannelRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("linux channel store unavailable")
	}
	normalizeLinuxChannel(&rec)
	if rec.ChannelID == "" || rec.Host == "" || rec.Username == "" || rec.CredentialID == "" {
		return fmt.Errorf("channel_id, host, username and credential_id are required")
	}
	if rec.Port < 1 || rec.Port > 65535 {
		return fmt.Errorf("channel port must be between 1 and 65535")
	}
	if rec.HostKeyPolicy != "known_hosts" && rec.HostKeyPolicy != "pinned" {
		return fmt.Errorf("host_key_policy must be known_hosts or pinned")
	}
	if rec.HostKeyPolicy == "pinned" && rec.HostKeyRef == "" {
		return fmt.Errorf("host_key_ref is required for pinned host key policy")
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
	_, err := s.db.ExecContext(ctx, `
INSERT INTO linux_channels(
 channel_id, display_name, host, port, username, credential_id,
 host_key_policy, host_key_ref, remote_shell, default_cwd,
 connect_timeout_ms, command_timeout_ms, keepalive_seconds, max_sessions,
 enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(channel_id) DO UPDATE SET
 display_name=excluded.display_name, host=excluded.host, port=excluded.port,
 username=excluded.username, credential_id=excluded.credential_id,
 host_key_policy=excluded.host_key_policy, host_key_ref=excluded.host_key_ref,
 remote_shell=excluded.remote_shell, default_cwd=excluded.default_cwd,
 connect_timeout_ms=excluded.connect_timeout_ms,
 command_timeout_ms=excluded.command_timeout_ms,
 keepalive_seconds=excluded.keepalive_seconds, max_sessions=excluded.max_sessions,
 enabled=excluded.enabled, updated_at=excluded.updated_at`,
		rec.ChannelID, rec.DisplayName, rec.Host, rec.Port, rec.Username, rec.CredentialID,
		rec.HostKeyPolicy, rec.HostKeyRef, rec.RemoteShell, rec.DefaultCWD,
		rec.ConnectTimeoutMS, rec.CommandTimeoutMS, rec.KeepaliveSeconds, rec.MaxSessions,
		boolInt(rec.Enabled), created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano))
	return err
}

// GenerateChannelID returns a server-generated ID that is unique in the
// channel store. Callers must not use user-provided IDs for new channels.
func (s *LinuxChannelStore) GenerateChannelID(ctx context.Context) (string, error) {
	return s.generateUniqueID(ctx, "channel_", "linux_channels", "channel_id")
}

func (s *LinuxChannelStore) DeleteChannel(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("linux channel store unavailable")
	}
	id = strings.TrimSpace(id)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_linux_channels WHERE channel_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM linux_channels WHERE channel_id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("linux channel %q not found", id)
	}
	return tx.Commit()
}

func (s *LinuxChannelStore) ListCredentials(ctx context.Context) ([]LinuxCredentialRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("linux channel store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT credential_id, display_name, auth_type, secret_ref, username_hint,
       enabled, created_at, updated_at
FROM linux_credentials ORDER BY credential_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinuxCredentialRecord
	for rows.Next() {
		rec, err := scanLinuxCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *LinuxChannelStore) GetCredential(ctx context.Context, id string) (*LinuxCredentialRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("linux channel store unavailable")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT credential_id, display_name, auth_type, secret_ref, username_hint,
       enabled, created_at, updated_at
FROM linux_credentials WHERE credential_id = ?`, strings.TrimSpace(id))
	rec, err := scanLinuxCredential(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *LinuxChannelStore) SaveCredential(ctx context.Context, rec LinuxCredentialRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("linux channel store unavailable")
	}
	rec.CredentialID = strings.TrimSpace(rec.CredentialID)
	rec.AuthType = strings.ToLower(strings.TrimSpace(rec.AuthType))
	rec.SecretRef = strings.TrimSpace(rec.SecretRef)
	if rec.CredentialID == "" || rec.AuthType == "" || rec.SecretRef == "" {
		return fmt.Errorf("credential_id, auth_type and secret_ref are required")
	}
	switch rec.AuthType {
	case "password", "private_key", "ssh_agent":
	default:
		return fmt.Errorf("unsupported credential auth_type %q", rec.AuthType)
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
	_, err := s.db.ExecContext(ctx, `
INSERT INTO linux_credentials(
 credential_id, display_name, auth_type, secret_ref, username_hint,
 enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(credential_id) DO UPDATE SET
 display_name=excluded.display_name, auth_type=excluded.auth_type,
 secret_ref=excluded.secret_ref, username_hint=excluded.username_hint,
 enabled=excluded.enabled, updated_at=excluded.updated_at`,
		rec.CredentialID, rec.DisplayName, rec.AuthType, rec.SecretRef, rec.UsernameHint,
		boolInt(rec.Enabled), created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano))
	return err
}

// GenerateCredentialID returns a server-generated ID that is unique in the
// credential store. Callers must not use user-provided IDs for new credentials.
func (s *LinuxChannelStore) GenerateCredentialID(ctx context.Context) (string, error) {
	return s.generateUniqueID(ctx, "cred_", "linux_credentials", "credential_id")
}

func (s *LinuxChannelStore) generateUniqueID(ctx context.Context, prefix, table, column string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("linux channel store unavailable")
	}
	for attempt := 0; attempt < 16; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return "", fmt.Errorf("generate server id: %w", err)
		}
		id := prefix + hex.EncodeToString(raw[:])
		query := fmt.Sprintf("SELECT 1 FROM %s WHERE %s = ? LIMIT 1", table, column)
		var marker int
		err := s.db.QueryRowContext(ctx, query, id).Scan(&marker)
		if err == sql.ErrNoRows {
			return id, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("could not generate a unique server id")
}

func (s *LinuxChannelStore) DeleteCredential(ctx context.Context, id string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("linux channel store unavailable")
	}
	id = strings.TrimSpace(id)
	var inUse int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM linux_channels WHERE credential_id = ?`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse > 0 {
		return fmt.Errorf("linux credential %q is still used by %d channel(s)", id, inUse)
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM linux_credentials WHERE credential_id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("linux credential %q not found", id)
	}
	return nil
}

func (s *LinuxChannelStore) ListBindings(ctx context.Context, agentID string) ([]LinuxChannelBindingRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("linux channel store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT agent_id, channel_id, enabled, is_default, remote_cwd, shell,
       max_concurrency, approval_mode, allowed_commands_json, denied_commands_json,
       created_at, updated_at
FROM agent_linux_channels WHERE agent_id = ? ORDER BY channel_id`, strings.TrimSpace(agentID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LinuxChannelBindingRecord
	for rows.Next() {
		rec, err := scanLinuxBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// ListTerminalConfigs implements tools.TerminalConfigResolver. It joins the
// Agent's enabled bindings with channel metadata and deliberately omits all
// credential and host-key fields from the returned summaries.
func (s *LinuxChannelStore) ListTerminalConfigs(ctx context.Context, agentID string) ([]tools.TerminalConfigInfo, error) {
	bindings, err := s.ListBindings(ctx, agentID)
	if err != nil {
		return nil, err
	}
	out := make([]tools.TerminalConfigInfo, 0, len(bindings))
	for _, binding := range bindings {
		if !binding.Enabled {
			continue
		}
		channel, err := s.GetChannel(ctx, binding.ChannelID)
		if err != nil {
			return nil, err
		}
		if channel == nil || !channel.Enabled {
			continue
		}
		out = append(out, terminalConfigInfo(*channel))
	}
	return out, nil
}

// ResolveTerminalConfig validates both sides of the binding before a terminal
// can be opened. The prefixed ID keeps the model-facing identifier distinct
// from a provider's raw channel ID.
func (s *LinuxChannelStore) ResolveTerminalConfig(ctx context.Context, agentID, configID string) (tools.TerminalConfigInfo, error) {
	configID = strings.TrimSpace(configID)
	if !strings.HasPrefix(configID, tools.TerminalConfigLinuxPrefix) {
		return tools.TerminalConfigInfo{}, fmt.Errorf("terminal config %q is not a Linux channel config", configID)
	}
	channelID := strings.TrimSpace(strings.TrimPrefix(configID, tools.TerminalConfigLinuxPrefix))
	if channelID == "" {
		return tools.TerminalConfigInfo{}, fmt.Errorf("terminal config id is incomplete")
	}
	binding, err := s.GetBinding(ctx, agentID, channelID)
	if err != nil {
		return tools.TerminalConfigInfo{}, err
	}
	if binding == nil || !binding.Enabled {
		return tools.TerminalConfigInfo{}, fmt.Errorf("terminal config %q is not enabled for agent %q", configID, strings.TrimSpace(agentID))
	}
	channel, err := s.GetChannel(ctx, channelID)
	if err != nil {
		return tools.TerminalConfigInfo{}, err
	}
	if channel == nil || !channel.Enabled {
		return tools.TerminalConfigInfo{}, fmt.Errorf("terminal config %q is disabled or missing", configID)
	}
	return terminalConfigInfo(*channel), nil
}

func terminalConfigInfo(rec LinuxChannelRecord) tools.TerminalConfigInfo {
	return tools.TerminalConfigInfo{
		ConfigID:    tools.TerminalConfigLinuxPrefix + rec.ChannelID,
		DisplayName: rec.DisplayName,
		Host:        rec.Host,
		Port:        rec.Port,
		Username:    rec.Username,
		Remark:      rec.DisplayName,
		TargetKind:  "linux_channel",
		TargetID:    rec.ChannelID,
	}
}

func (s *LinuxChannelStore) GetBinding(ctx context.Context, agentID, channelID string) (*LinuxChannelBindingRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("linux channel store unavailable")
	}
	row := s.db.QueryRowContext(ctx, `
SELECT agent_id, channel_id, enabled, is_default, remote_cwd, shell,
       max_concurrency, approval_mode, allowed_commands_json, denied_commands_json,
       created_at, updated_at
FROM agent_linux_channels WHERE agent_id = ? AND channel_id = ?`, strings.TrimSpace(agentID), strings.TrimSpace(channelID))
	rec, err := scanLinuxBinding(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (s *LinuxChannelStore) SaveBinding(ctx context.Context, rec LinuxChannelBindingRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("linux channel store unavailable")
	}
	return saveLinuxBinding(ctx, s.db, rec)
}

// ReplaceBindings atomically replaces one Agent's complete channel binding
// set. Validation happens before the delete, so a malformed update cannot
// leave the Agent with a partially configured list.
func (s *LinuxChannelStore) ReplaceBindings(ctx context.Context, agentID string, records []LinuxChannelBindingRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("linux channel store unavailable")
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	for i := range records {
		records[i].AgentID = agentID
		if err := validateLinuxBinding(records[i]); err != nil {
			return err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_linux_channels WHERE agent_id = ?`, agentID); err != nil {
		return err
	}
	for _, rec := range records {
		if err := saveLinuxBinding(ctx, tx, rec); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type linuxBindingExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func validateLinuxBinding(rec LinuxChannelBindingRecord) error {
	rec.AgentID = strings.TrimSpace(rec.AgentID)
	rec.ChannelID = strings.TrimSpace(rec.ChannelID)
	if rec.AgentID == "" || rec.ChannelID == "" {
		return fmt.Errorf("agent_id and channel_id are required")
	}
	return nil
}

func saveLinuxBinding(ctx context.Context, execer linuxBindingExecer, rec LinuxChannelBindingRecord) error {
	if err := validateLinuxBinding(rec); err != nil {
		return err
	}
	rec.AgentID = strings.TrimSpace(rec.AgentID)
	rec.ChannelID = strings.TrimSpace(rec.ChannelID)
	rec.ApprovalMode = strings.TrimSpace(rec.ApprovalMode)
	if rec.MaxConcurrency <= 0 {
		rec.MaxConcurrency = 1
	}
	if rec.ApprovalMode == "" {
		rec.ApprovalMode = "require_approval"
	}
	allowed, err := json.Marshal(rec.AllowedCommands)
	if err != nil {
		return err
	}
	denied, err := json.Marshal(rec.DeniedCommands)
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
	_, err = execer.ExecContext(ctx, `
INSERT INTO agent_linux_channels(
 agent_id, channel_id, enabled, is_default, remote_cwd, shell,
 max_concurrency, approval_mode, allowed_commands_json, denied_commands_json,
 created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(agent_id, channel_id) DO UPDATE SET
 enabled=excluded.enabled, is_default=excluded.is_default,
 remote_cwd=excluded.remote_cwd, shell=excluded.shell,
 max_concurrency=excluded.max_concurrency, approval_mode=excluded.approval_mode,
 allowed_commands_json=excluded.allowed_commands_json,
 denied_commands_json=excluded.denied_commands_json, updated_at=excluded.updated_at`,
		rec.AgentID, rec.ChannelID, boolInt(rec.Enabled), boolInt(rec.IsDefault), rec.RemoteCWD, rec.Shell,
		rec.MaxConcurrency, rec.ApprovalMode, string(allowed), string(denied),
		created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano))
	return err
}

func (s *LinuxChannelStore) DeleteBinding(ctx context.Context, agentID, channelID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("linux channel store unavailable")
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM agent_linux_channels WHERE agent_id = ? AND channel_id = ?`, strings.TrimSpace(agentID), strings.TrimSpace(channelID))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("linux channel binding not found")
	}
	return nil
}

// ResolveLinuxChannel implements tools.LinuxChannelResolver without exposing
// credential secret material to the execution package.
func (s *LinuxChannelStore) ResolveLinuxChannel(ctx context.Context, id string) (tools.LinuxChannelConfig, error) {
	rec, err := s.GetChannel(ctx, id)
	if err != nil {
		return tools.LinuxChannelConfig{}, err
	}
	if rec == nil {
		return tools.LinuxChannelConfig{}, fmt.Errorf("linux channel %q not found", id)
	}
	return tools.LinuxChannelConfig{
		ID: rec.ChannelID, DisplayName: rec.DisplayName, Host: rec.Host, Port: rec.Port,
		Username: rec.Username, CredentialID: rec.CredentialID,
		HostKeyPolicy: rec.HostKeyPolicy, HostKeyRef: rec.HostKeyRef,
		RemoteShell: rec.RemoteShell, DefaultCWD: rec.DefaultCWD,
		ConnectTimeout: time.Duration(rec.ConnectTimeoutMS) * time.Millisecond,
		CommandTimeout: time.Duration(rec.CommandTimeoutMS) * time.Millisecond,
		Enabled:        rec.Enabled, MaxSessions: rec.MaxSessions,
	}, nil
}

func (s *LinuxChannelStore) ResolveLinuxCredential(ctx context.Context, id string) (tools.LinuxCredential, error) {
	rec, err := s.GetCredential(ctx, id)
	if err != nil {
		return tools.LinuxCredential{}, err
	}
	if rec == nil {
		return tools.LinuxCredential{}, fmt.Errorf("linux credential %q not found", id)
	}
	return tools.LinuxCredential{
		ID: rec.CredentialID, DisplayName: rec.DisplayName, AuthType: rec.AuthType,
		SecretRef: rec.SecretRef, UsernameHint: rec.UsernameHint, Enabled: rec.Enabled,
	}, nil
}

func (s *LinuxChannelStore) ResolveLinuxBinding(ctx context.Context, agentID, channelID string) (tools.LinuxChannelBinding, error) {
	rec, err := s.GetBinding(ctx, agentID, channelID)
	if err != nil {
		return tools.LinuxChannelBinding{}, err
	}
	if rec == nil {
		return tools.LinuxChannelBinding{}, fmt.Errorf("linux channel %q is not bound to agent %q", channelID, agentID)
	}
	return tools.LinuxChannelBinding{
		AgentID: rec.AgentID, ChannelID: rec.ChannelID, Enabled: rec.Enabled,
		IsDefault: rec.IsDefault, RemoteCWD: rec.RemoteCWD, Shell: rec.Shell,
		MaxConcurrency: rec.MaxConcurrency, ApprovalMode: rec.ApprovalMode,
		AllowedCommands: append([]string(nil), rec.AllowedCommands...),
		DeniedCommands:  append([]string(nil), rec.DeniedCommands...),
	}, nil
}

type linuxScanner interface{ Scan(dest ...any) error }

func scanLinuxChannel(row linuxScanner) (LinuxChannelRecord, error) {
	var rec LinuxChannelRecord
	var enabled int
	var created, updated string
	err := row.Scan(&rec.ChannelID, &rec.DisplayName, &rec.Host, &rec.Port, &rec.Username,
		&rec.CredentialID, &rec.HostKeyPolicy, &rec.HostKeyRef, &rec.RemoteShell, &rec.DefaultCWD,
		&rec.ConnectTimeoutMS, &rec.CommandTimeoutMS, &rec.KeepaliveSeconds, &rec.MaxSessions,
		&enabled, &created, &updated)
	if err != nil {
		return LinuxChannelRecord{}, err
	}
	rec.Enabled = enabled != 0
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return rec, nil
}

func scanLinuxCredential(row linuxScanner) (LinuxCredentialRecord, error) {
	var rec LinuxCredentialRecord
	var enabled int
	var created, updated string
	err := row.Scan(&rec.CredentialID, &rec.DisplayName, &rec.AuthType, &rec.SecretRef,
		&rec.UsernameHint, &enabled, &created, &updated)
	if err != nil {
		return LinuxCredentialRecord{}, err
	}
	rec.Enabled = enabled != 0
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return rec, nil
}

func scanLinuxBinding(row linuxScanner) (LinuxChannelBindingRecord, error) {
	var rec LinuxChannelBindingRecord
	var enabled, isDefault int
	var allowedRaw, deniedRaw, created, updated string
	err := row.Scan(&rec.AgentID, &rec.ChannelID, &enabled, &isDefault, &rec.RemoteCWD, &rec.Shell,
		&rec.MaxConcurrency, &rec.ApprovalMode, &allowedRaw, &deniedRaw, &created, &updated)
	if err != nil {
		return LinuxChannelBindingRecord{}, err
	}
	rec.Enabled = enabled != 0
	rec.IsDefault = isDefault != 0
	_ = json.Unmarshal([]byte(allowedRaw), &rec.AllowedCommands)
	_ = json.Unmarshal([]byte(deniedRaw), &rec.DeniedCommands)
	if rec.AllowedCommands == nil {
		rec.AllowedCommands = []string{}
	}
	if rec.DeniedCommands == nil {
		rec.DeniedCommands = []string{}
	}
	rec.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	rec.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return rec, nil
}

func normalizeLinuxChannel(rec *LinuxChannelRecord) {
	rec.ChannelID = strings.TrimSpace(rec.ChannelID)
	rec.DisplayName = strings.TrimSpace(rec.DisplayName)
	rec.Host = strings.TrimSpace(rec.Host)
	rec.Username = strings.TrimSpace(rec.Username)
	rec.CredentialID = strings.TrimSpace(rec.CredentialID)
	rec.HostKeyPolicy = strings.ToLower(strings.TrimSpace(rec.HostKeyPolicy))
	if rec.HostKeyPolicy == "" {
		rec.HostKeyPolicy = "known_hosts"
	}
	rec.HostKeyRef = strings.TrimSpace(rec.HostKeyRef)
	rec.RemoteShell = strings.TrimSpace(rec.RemoteShell)
	if rec.RemoteShell == "" {
		rec.RemoteShell = "bash"
	}
	if rec.Port == 0 {
		rec.Port = 22
	}
	if rec.ConnectTimeoutMS <= 0 {
		rec.ConnectTimeoutMS = 10000
	}
	if rec.CommandTimeoutMS <= 0 {
		rec.CommandTimeoutMS = 120000
	}
	if rec.KeepaliveSeconds <= 0 {
		rec.KeepaliveSeconds = 30
	}
	if rec.MaxSessions <= 0 {
		rec.MaxSessions = 4
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
