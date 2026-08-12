package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// LLMConfigRecord 为一条 LLM 连接配置（API Key 以密文存库）。
type LLMConfigRecord struct {
	ID                 string
	SortOrder          int
	Provider           string
	BaseURL            string
	Model              string
	APIKeyCiphertext   string
	Mock               bool
	Thinking           string
	ReasoningEffort    string
	MultimodalEnabled  bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// LLMConfigStore 持久化 LLM 配置列表（llm_configs.db）。
type LLMConfigStore struct {
	db  *sql.DB
	box *SecretBox
}

// OpenLLMConfigs 打开或创建 llm_configs.db，并初始化加密密钥。
func OpenLLMConfigs(dbPath, keyDir string) (*LLMConfigStore, error) {
	path := strings.TrimSpace(dbPath)
	if path == "" {
		return nil, fmt.Errorf("llm configs db path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create llm configs db dir: %w", err)
	}
	box, err := OpenSecretBox(keyDir)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &LLMConfigStore{db: db, box: box}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭连接。
func (s *LLMConfigStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *LLMConfigStore) initSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS llm_configs (
  id TEXT PRIMARY KEY,
  sort_order INTEGER NOT NULL,
  provider TEXT NOT NULL,
  base_url TEXT NOT NULL DEFAULT '',
  model TEXT NOT NULL DEFAULT '',
  api_key_ciphertext TEXT NOT NULL DEFAULT '',
  mock INTEGER NOT NULL DEFAULT 0,
  thinking TEXT NOT NULL DEFAULT '',
  reasoning_effort TEXT NOT NULL DEFAULT '',
  multimodal_enabled INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_llm_configs_sort ON llm_configs(sort_order, id);
`)
	return err
}

// List 按 sort_order 返回全部配置（不含明文 key）。
func (s *LLMConfigStore) List(ctx context.Context) ([]LLMConfigRecord, error) {
	if s == nil {
		return nil, fmt.Errorf("llm config store unavailable")
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, sort_order, provider, base_url, model, api_key_ciphertext, mock,
       thinking, reasoning_effort, multimodal_enabled, created_at, updated_at
FROM llm_configs
ORDER BY sort_order ASC, id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LLMConfigRecord
	for rows.Next() {
		rec, err := scanLLMConfig(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

// Get 按 id 读取一条。
func (s *LLMConfigStore) Get(ctx context.Context, id string) (LLMConfigRecord, error) {
	if s == nil {
		return LLMConfigRecord{}, fmt.Errorf("llm config store unavailable")
	}
	id = strings.TrimSpace(id)
	row := s.db.QueryRowContext(ctx, `
SELECT id, sort_order, provider, base_url, model, api_key_ciphertext, mock,
       thinking, reasoning_effort, multimodal_enabled, created_at, updated_at
FROM llm_configs WHERE id = ?`, id)
	return scanLLMConfig(row)
}

// HasAPIKey 是否已存密文 key。
func (r LLMConfigRecord) HasAPIKey() bool {
	return strings.TrimSpace(r.APIKeyCiphertext) != ""
}

// DecryptAPIKey 解密 API Key；无密文时返回空串。
func (s *LLMConfigStore) DecryptAPIKey(rec LLMConfigRecord) (string, error) {
	if s == nil || s.box == nil {
		return "", fmt.Errorf("llm config store unavailable")
	}
	if !rec.HasAPIKey() {
		return "", nil
	}
	return s.box.Decrypt(rec.APIKeyCiphertext)
}

// ResolveAPIKey 按 id 解密 API Key。
func (s *LLMConfigStore) ResolveAPIKey(ctx context.Context, id string) (string, error) {
	rec, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return s.DecryptAPIKey(rec)
}

// ReplaceAll 用完整列表覆盖存储；apiKeyByID 中缺省或空串表示保留原密文（若旧记录存在）。
// 传入的明文 key（非空）会加密写入；显式清空用 clearAPIKeyIDs。
func (s *LLMConfigStore) ReplaceAll(ctx context.Context, records []LLMConfigRecord, apiKeys map[string]string, clearAPIKeyIDs map[string]bool) error {
	if s == nil {
		return fmt.Errorf("llm config store unavailable")
	}
	if len(records) == 0 {
		return fmt.Errorf("at least one llm config is required")
	}

	existing := map[string]LLMConfigRecord{}
	prev, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, r := range prev {
		existing[r.ID] = r
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM llm_configs`); err != nil {
		return err
	}

	now := time.Now().UTC()
	for i, rec := range records {
		id := strings.TrimSpace(rec.ID)
		if id == "" {
			return fmt.Errorf("llm config id is required")
		}
		provider := strings.ToLower(strings.TrimSpace(rec.Provider))
		if provider == "" {
			return fmt.Errorf("llm config %q: provider is required", id)
		}
		mock := rec.Mock || provider == "mock"
		cipherText := ""
		if clearAPIKeyIDs[id] {
			cipherText = ""
		} else if key, ok := apiKeys[id]; ok && strings.TrimSpace(key) != "" {
			enc, err := s.box.Encrypt(strings.TrimSpace(key))
			if err != nil {
				return fmt.Errorf("encrypt api key for %q: %w", id, err)
			}
			cipherText = enc
		} else if old, ok := existing[id]; ok {
			cipherText = old.APIKeyCiphertext
		}
		created := rec.CreatedAt
		if created.IsZero() {
			if old, ok := existing[id]; ok && !old.CreatedAt.IsZero() {
				created = old.CreatedAt
			} else {
				created = now
			}
		}
		mockInt := 0
		if mock {
			mockInt = 1
		}
		mm := 0
		if rec.MultimodalEnabled {
			mm = 1
		}
		_, err := tx.ExecContext(ctx, `
INSERT INTO llm_configs (
  id, sort_order, provider, base_url, model, api_key_ciphertext, mock,
  thinking, reasoning_effort, multimodal_enabled, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, i, provider, strings.TrimSpace(rec.BaseURL), strings.TrimSpace(rec.Model),
			cipherText, mockInt, strings.TrimSpace(rec.Thinking), strings.TrimSpace(rec.ReasoningEffort),
			mm, created.UTC().Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Count 返回配置条数。
func (s *LLMConfigStore) Count(ctx context.Context) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("llm config store unavailable")
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM llm_configs`).Scan(&n)
	return n, err
}

type llmConfigScanner interface {
	Scan(dest ...any) error
}

func scanLLMConfig(row llmConfigScanner) (LLMConfigRecord, error) {
	var (
		rec            LLMConfigRecord
		mock           int
		mm             int
		created, updated string
	)
	err := row.Scan(
		&rec.ID, &rec.SortOrder, &rec.Provider, &rec.BaseURL, &rec.Model, &rec.APIKeyCiphertext,
		&mock, &rec.Thinking, &rec.ReasoningEffort, &mm, &created, &updated,
	)
	if err != nil {
		return LLMConfigRecord{}, err
	}
	rec.Mock = mock != 0
	rec.MultimodalEnabled = mm != 0
	if t, err := time.Parse(time.RFC3339Nano, created); err == nil {
		rec.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339Nano, updated); err == nil {
		rec.UpdatedAt = t
	}
	return rec, nil
}
