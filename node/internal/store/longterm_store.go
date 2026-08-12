package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	LongTermScopeGlobal = "global"
	LongTermScopeAgent  = "agent"
)

// LongTermEntry 为单条长期记忆条目。
type LongTermEntry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LongTermRecord 为某 scope 下的结构化长期记忆集合。
type LongTermRecord struct {
	Scope     string
	AgentID   string
	Entries   []LongTermEntry
	UpdatedAt time.Time
}

func (s *AgentStore) ensureLongTermStoreSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS longterm_store (
  scope TEXT NOT NULL,
  agent_id TEXT NOT NULL DEFAULT '',
  entries_json TEXT NOT NULL DEFAULT '[]',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (scope, agent_id)
);
`)
	return err
}

// GetLongTermRecord 读取结构化长期记忆；不存在返回 (nil, nil)。
func (s *AgentStore) GetLongTermRecord(ctx context.Context, scope, agentID string) (*LongTermRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	scope = normalizeLongTermScope(scope)
	agentID = longTermAgentKey(scope, agentID)
	row := s.db.QueryRowContext(ctx, `
SELECT scope, agent_id, entries_json, updated_at
FROM longterm_store WHERE scope = ? AND agent_id = ?`, scope, agentID)
	var gotScope, gotAgent, entriesJSON, updated string
	if err := row.Scan(&gotScope, &gotAgent, &entriesJSON, &updated); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	entries, err := decodeLongTermEntries(entriesJSON)
	if err != nil {
		return nil, err
	}
	ut, _ := time.Parse(time.RFC3339Nano, updated)
	return &LongTermRecord{
		Scope:     gotScope,
		AgentID:   gotAgent,
		Entries:   entries,
		UpdatedAt: ut,
	}, nil
}

// EnsureLongTermRecord 确保结构化长期记忆存在；缺失时从 legacy 正文迁移。
func (s *AgentStore) EnsureLongTermRecord(ctx context.Context, scope, agentID, runtimeDir, legacyMD string) (*LongTermRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("agent store unavailable")
	}
	scope = normalizeLongTermScope(scope)
	agentKey := longTermAgentKey(scope, agentID)
	existing, err := s.GetLongTermRecord(ctx, scope, agentKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, nil
	}
	legacy := strings.TrimSpace(legacyMD)
	if legacy == "" && strings.TrimSpace(runtimeDir) != "" && scope == LongTermScopeGlobal {
		legacy = readTrimmedFile(filepath.Join(runtimeDir, "memory", "long_term.md"))
	}
	now := time.Now().UTC()
	rec := &LongTermRecord{
		Scope:     scope,
		AgentID:   agentKey,
		Entries:   EntriesFromLegacyMarkdown(legacy, now),
		UpdatedAt: now,
	}
	if err := s.saveLongTermRecord(ctx, *rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// SaveLongTermRecordCAS 在 updated_at 匹配时写入条目集合。
func (s *AgentStore) SaveLongTermRecordCAS(ctx context.Context, rec LongTermRecord, expectedUpdatedAt time.Time) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("agent store unavailable")
	}
	rec.Scope = normalizeLongTermScope(rec.Scope)
	rec.AgentID = longTermAgentKey(rec.Scope, rec.AgentID)
	expected := expectedUpdatedAt.UTC().Format(time.RFC3339Nano)
	now := time.Now().UTC()
	entriesJSON, err := encodeLongTermEntries(rec.Entries)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `
UPDATE longterm_store
SET entries_json = ?, updated_at = ?
WHERE scope = ? AND agent_id = ? AND updated_at = ?`,
		entriesJSON, now.Format(time.RFC3339Nano), rec.Scope, rec.AgentID, expected)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// SaveLongTermRecordOverwrite 覆盖写入条目（设置页保存，无 CAS）。
func (s *AgentStore) SaveLongTermRecordOverwrite(ctx context.Context, rec LongTermRecord) error {
	return s.saveLongTermRecord(ctx, rec)
}

func (s *AgentStore) saveLongTermRecord(ctx context.Context, rec LongTermRecord) error {
	rec.Scope = normalizeLongTermScope(rec.Scope)
	rec.AgentID = longTermAgentKey(rec.Scope, rec.AgentID)
	now := time.Now().UTC()
	if rec.UpdatedAt.IsZero() {
		rec.UpdatedAt = now
	}
	entriesJSON, err := encodeLongTermEntries(rec.Entries)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO longterm_store (scope, agent_id, entries_json, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(scope, agent_id) DO UPDATE SET
  entries_json = excluded.entries_json,
  updated_at = excluded.updated_at`,
		rec.Scope, rec.AgentID, entriesJSON, rec.UpdatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func normalizeLongTermScope(scope string) string {
	if strings.TrimSpace(scope) == LongTermScopeGlobal {
		return LongTermScopeGlobal
	}
	return LongTermScopeAgent
}

func longTermAgentKey(scope, agentID string) string {
	if scope == LongTermScopeGlobal {
		return ""
	}
	return strings.TrimSpace(agentID)
}

func encodeLongTermEntries(entries []LongTermEntry) (string, error) {
	if entries == nil {
		entries = []LongTermEntry{}
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeLongTermEntries(raw string) ([]LongTermEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return []LongTermEntry{}, nil
	}
	var entries []LongTermEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// EntriesFromLegacyMarkdown 将旧版扁平正文拆分为条目。
func EntriesFromLegacyMarkdown(md string, now time.Time) []LongTermEntry {
	md = strings.TrimSpace(md)
	if md == "" {
		return []LongTermEntry{}
	}
	parts := strings.Split(md, "\n\n")
	out := make([]LongTermEntry, 0, len(parts))
	for _, part := range parts {
		content := strings.TrimSpace(stripLongTermEntryPrefix(part))
		if content == "" {
			continue
		}
		out = append(out, NewLongTermEntry(content, now))
	}
	if len(out) == 0 {
		return []LongTermEntry{}
	}
	return out
}

func stripLongTermEntryPrefix(line string) string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "- [") {
		return line
	}
	close := strings.Index(line, "] ")
	if close < 0 {
		return line
	}
	return strings.TrimSpace(line[close+2:])
}

// NewLongTermEntry 创建带 ID 的新条目。
func NewLongTermEntry(content string, now time.Time) LongTermEntry {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return LongTermEntry{
		ID:        newLongTermEntryID(),
		Content:   strings.TrimSpace(content),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func newLongTermEntryID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return "lt-" + hex.EncodeToString(b[:])
}
