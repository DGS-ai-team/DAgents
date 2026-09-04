package memory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "modernc.org/sqlite"

	"github.com/DGS-ai-team/DAgents/node/internal/tokens"
)

const (
	defaultRecallLimit  = DefaultMemorySearchLimit
	defaultRecallBudget = DefaultMemoryRecallTokenBudget
	defaultCoreBudget   = 2000
)

var ErrNotFound = errors.New("memory entry not found")

// Store 是单一 scope 的 Workspace SQLite 权威存储。
type Store struct {
	db       *sql.DB
	path     string
	scope    Scope
	ftsMu    sync.RWMutex
	ftsReady bool
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func OpenStore(path string, scope Scope) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("memory database path is required")
	}
	if scope != ScopeAgent && scope != ScopeGlobal {
		return nil, fmt.Errorf("unsupported memory scope %q", scope)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create memory database dir: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	// Several Agent runtimes may share the global store. A short busy timeout
	// turns ordinary concurrent writes into bounded waits instead of spurious
	// SQLITE_BUSY failures; the Store still serializes its own connection.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure memory database: %w", err)
	}
	s := &Store{db: db, path: path, scope: scope}
	if err := s.initSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) initSchema() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memory store unavailable")
	}
	if _, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS memory_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS memory_entries (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  tier TEXT NOT NULL,
  kind TEXT NOT NULL,
  semantic_key TEXT NOT NULL DEFAULT '',
  subject TEXT NOT NULL DEFAULT '',
  predicate TEXT NOT NULL DEFAULT '',
  value_json TEXT NOT NULL DEFAULT 'null',
  qualifiers_json TEXT NOT NULL DEFAULT '{}',
  cardinality TEXT NOT NULL DEFAULT 'unknown',
  content TEXT NOT NULL,
  normalized_content TEXT NOT NULL,
  content_hash TEXT NOT NULL,
  status TEXT NOT NULL,
  importance INTEGER NOT NULL DEFAULT 50,
  confidence INTEGER NOT NULL DEFAULT 100,
  sensitivity TEXT NOT NULL DEFAULT 'normal',
  source_type TEXT NOT NULL DEFAULT 'explicit_user',
  source_session_id TEXT NOT NULL DEFAULT '',
  source_message_id TEXT NOT NULL DEFAULT '',
  source_reference TEXT NOT NULL DEFAULT '',
  supersedes_id TEXT,
  conflict_group_id TEXT,
  valid_from TEXT,
  valid_to TEXT,
  expires_at TEXT,
  revision INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_accessed_at TEXT,
  access_count INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_memory_scope_status
  ON memory_entries(scope, status, tier, updated_at);
CREATE INDEX IF NOT EXISTS idx_memory_semantic_key
  ON memory_entries(scope, semantic_key, status);
CREATE INDEX IF NOT EXISTS idx_memory_subject_predicate
  ON memory_entries(scope, subject, predicate, status);
CREATE INDEX IF NOT EXISTS idx_memory_expiry
  ON memory_entries(scope, expires_at, status);
CREATE TABLE IF NOT EXISTS memory_revisions (
  memory_id TEXT NOT NULL,
  revision INTEGER NOT NULL,
  snapshot_json TEXT NOT NULL,
  changed_by TEXT NOT NULL,
  change_reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(memory_id, revision)
);
CREATE TABLE IF NOT EXISTS memory_conflicts (
  id TEXT PRIMARY KEY,
  candidate_json TEXT NOT NULL,
  existing_ids_json TEXT NOT NULL,
  existing_revisions_json TEXT NOT NULL,
  relation TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  store_revision INTEGER NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  resolved_at TEXT
);
INSERT OR IGNORE INTO memory_meta(key, value) VALUES ('schema_version', '1');
INSERT OR IGNORE INTO memory_meta(key, value) VALUES ('store_revision', '0');
`); err != nil {
		return fmt.Errorf("create memory schema: %w", err)
	}
	_, err := s.db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(
  memory_id UNINDEXED,
  semantic_key,
  subject,
  predicate,
  content,
  tokenize = 'trigram'
)`)
	s.ftsMu.Lock()
	s.ftsReady = err == nil
	s.ftsMu.Unlock()
	// FTS 不可用时仍允许精确字段和 LIKE fallback，能力会由上层指标暴露。
	return nil
}

func (s *Store) ftsAvailable() bool {
	s.ftsMu.RLock()
	defer s.ftsMu.RUnlock()
	return s.ftsReady
}

func (s *Store) revision(ctx context.Context) (int64, error) {
	return revisionFrom(ctx, s.db)
}

func revisionFrom(ctx context.Context, q queryer) (int64, error) {
	var raw string
	if err := q.QueryRowContext(ctx, `SELECT value FROM memory_meta WHERE key='store_revision'`).Scan(&raw); err != nil {
		return 0, err
	}
	var n int64
	if _, err := fmt.Sscan(raw, &n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) nextRevision(ctx context.Context, tx *sql.Tx) (int64, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT value FROM memory_meta WHERE key='store_revision'`).Scan(&raw); err != nil {
		return 0, err
	}
	var n int64
	if _, err := fmt.Sscan(raw, &n); err != nil {
		return 0, err
	}
	n++
	if _, err := tx.ExecContext(ctx, `UPDATE memory_meta SET value=? WHERE key='store_revision'`, fmt.Sprint(n)); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) list(ctx context.Context, where string, args ...any) ([]Entry, error) {
	return s.listWithQueryer(ctx, s.db, where, args...)
}

func (s *Store) listWithQueryer(ctx context.Context, q queryer, where string, args ...any) ([]Entry, error) {
	query := `SELECT id, scope, tier, kind, semantic_key, subject, predicate,
value_json, qualifiers_json, cardinality, content, normalized_content,
content_hash, status, importance, confidence, sensitivity, source_type,
source_session_id, source_message_id, source_reference, supersedes_id,
conflict_group_id, valid_from, valid_to, expires_at, revision, created_at,
updated_at, last_accessed_at, access_count FROM memory_entries WHERE ` + where
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type rowScanner interface{ Scan(dest ...any) error }

func scanEntry(row rowScanner) (Entry, error) {
	var e Entry
	var scope, tier, kind, status string
	var valueRaw, qualifiersRaw string
	var validFrom, validTo, expiresAt, createdAt, updatedAt string
	var lastAccessedNull sql.NullString
	var supersedes, conflictGroup sql.NullString
	if err := row.Scan(
		&e.ID, &scope, &tier, &kind, &e.SemanticKey, &e.Subject, &e.Predicate,
		&valueRaw, &qualifiersRaw, &e.Cardinality, &e.Content, &e.NormalizedText,
		&e.ContentHash, &status, &e.Importance, &e.Confidence, &e.Sensitivity,
		&e.SourceType, &e.SourceSession, &e.SourceMessage, &e.SourceRef,
		&supersedes, &conflictGroup, &validFrom, &validTo, &expiresAt, &e.Revision,
		&createdAt, &updatedAt, &lastAccessedNull, &e.AccessCount,
	); err != nil {
		return Entry{}, err
	}
	e.Scope, e.Tier, e.Kind, e.Status = Scope(scope), Tier(tier), Kind(kind), Status(status)
	e.SupersedesID, e.ConflictGroup = supersedes.String, conflictGroup.String
	_ = json.Unmarshal([]byte(valueRaw), &e.Value)
	_ = json.Unmarshal([]byte(qualifiersRaw), &e.Qualifiers)
	if t, err := parseTime(valueFromString(validFrom)); err == nil && !t.IsZero() {
		e.ValidFrom = &t
	}
	if t, err := parseTime(valueFromString(validTo)); err == nil && !t.IsZero() {
		e.ValidTo = &t
	}
	if t, err := parseTime(valueFromString(expiresAt)); err == nil && !t.IsZero() {
		e.ExpiresAt = &t
	}
	if t, err := parseTime(lastAccessedNull.String); err == nil && !t.IsZero() {
		e.LastAccessedAt = &t
	}
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	e.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return e, nil
}

func valueFromString(v string) string { return v }

func parseTime(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, raw)
}

func eligibleWhere(now time.Time) (string, []any) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return `(status IN (?, ?) AND (expires_at IS NULL OR expires_at = '' OR expires_at > ?))`, []any{string(StatusActive), string(StatusConflicted), now.UTC().Format(time.RFC3339Nano)}
}

// recall reads Core, Recall candidates and store_revision from one SQLite
// snapshot. Without this transaction a concurrent remember could change the
// revision between the two queries, making the diagnostic revision describe a
// different database state from the rendered MemoryContext.
func (s *Store) recall(ctx context.Context, req RecallRequest) ([]Entry, []SearchResult, int64, error) {
	if req.Now.IsZero() {
		req.Now = time.Now().UTC()
	}
	if req.Limit <= 0 || req.Limit > 50 {
		req.Limit = defaultRecallLimit
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	defer tx.Rollback()

	var core []Entry
	if req.IncludeCore {
		where, args := eligibleWhere(req.Now)
		core, err = s.listWithQueryer(ctx, tx, where+` AND tier = ? ORDER BY importance DESC, semantic_key ASC, id ASC`, append(args, string(TierCore))...)
		if err != nil {
			return nil, nil, 0, err
		}
	}
	var recalled []SearchResult
	if strings.TrimSpace(req.QueryText) != "" {
		recalled, err = s.searchWithQueryer(ctx, tx, SearchRequest{
			Scope: s.scope, Query: req.QueryText, Limit: req.Limit, Now: req.Now,
		})
		if err != nil {
			return nil, nil, 0, err
		}
	}
	revision, err := revisionFrom(ctx, tx)
	if err != nil {
		return nil, nil, 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, 0, err
	}
	return core, recalled, revision, nil
}

func (s *Store) search(ctx context.Context, req SearchRequest) ([]SearchResult, error) {
	return s.searchWithQueryer(ctx, s.db, req)
}

func (s *Store) searchWithQueryer(ctx context.Context, q queryer, req SearchRequest) ([]SearchResult, error) {
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, nil
	}
	terms := memorySearchTerms(query)
	// Search is a model-facing boundary. Bound the query before it reaches
	// FTS/LIKE or dynamic term construction so a pasted transcript cannot turn
	// one lookup into an unbounded SQL request.
	query = strings.TrimSpace(tokens.TakePrefixForTokenBudget(query, float64(DefaultMemoryQueryTokenBudget)))
	if query == "" {
		return nil, nil
	}
	limit := req.Limit
	if limit <= 0 || limit > 50 {
		limit = defaultRecallLimit
	}
	now := req.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	where, args := eligibleWhere(now)
	if req.IncludeInactive {
		where, args = `scope = ?`, []any{string(s.scope)}
	} else {
		where = `scope = ? AND ` + where
		args = append([]any{string(s.scope)}, args...)
	}
	if s.ftsAvailable() {
		match := `"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		sqlText := `SELECT e.id, e.scope, e.tier, e.kind, e.semantic_key, e.subject, e.predicate,
e.value_json, e.qualifiers_json, e.cardinality, e.content, e.normalized_content,
e.content_hash, e.status, e.importance, e.confidence, e.sensitivity, e.source_type,
e.source_session_id, e.source_message_id, e.source_reference, e.supersedes_id,
e.conflict_group_id, e.valid_from, e.valid_to, e.expires_at, e.revision, e.created_at,
e.updated_at, e.last_accessed_at, e.access_count
FROM memory_fts f JOIN memory_entries e ON e.id=f.memory_id
WHERE f MATCH ? AND ` + where + ` ORDER BY bm25(f), e.importance DESC, e.updated_at DESC LIMIT ?`
		rows, err := q.QueryContext(ctx, sqlText, append([]any{match}, append(args, limit)...)...)
		if err == nil {
			var out []SearchResult
			for rows.Next() {
				e, scanErr := scanEntry(rows)
				if scanErr != nil {
					return nil, scanErr
				}
				out = append(out, SearchResult{Entry: e, Score: memorySearchScore(e, terms)})
			}
			if rowsErr := rows.Err(); rowsErr != nil {
				_ = rows.Close()
				return nil, rowsErr
			}
			_ = rows.Close()
			if len(out) > 0 {
				return out, nil
			}
		}
	}
	// LIKE 是 FTS 不可用、中文短查询或 FTS 无结果时的确定性降级。
	like := `%` + query + `%`
	sqlText := `SELECT id, scope, tier, kind, semantic_key, subject, predicate,
value_json, qualifiers_json, cardinality, content, normalized_content,
content_hash, status, importance, confidence, sensitivity, source_type,
source_session_id, source_message_id, source_reference, supersedes_id,
conflict_group_id, valid_from, valid_to, expires_at, revision, created_at,
updated_at, last_accessed_at, access_count FROM memory_entries WHERE ` + where +
		` AND (content LIKE ? OR semantic_key LIKE ? OR subject LIKE ? OR predicate LIKE ?)
ORDER BY importance DESC, updated_at DESC, id ASC LIMIT ?`
	rows, err := q.QueryContext(ctx, sqlText, append(args, like, like, like, like, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchResult
	for rows.Next() {
		e, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, SearchResult{Entry: e, Score: memorySearchScore(e, terms)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		sortMemorySearchResults(out)
		return out, nil
	}
	return s.searchByTermsWithQueryer(ctx, q, where, args, query, limit)
}

// searchByTermsWithQueryer is the deterministic fallback for natural-language
// recall. Automatic Turn recall passes the complete root user message, which
// is intentionally too specific for an exact FTS phrase query. Extracting
// literal identifiers and meaningful CJK bigrams keeps recall explainable and
// avoids handing an unsanitized natural-language sentence to the FTS parser.
func (s *Store) searchByTermsWithQueryer(ctx context.Context, q queryer, where string, args []any, query string, limit int) ([]SearchResult, error) {
	terms := memorySearchTerms(query)
	if len(terms) == 0 {
		return nil, nil
	}
	clauses := make([]string, 0, len(terms))
	searchArgs := append([]any(nil), args...)
	for _, term := range terms {
		clauses = append(clauses, `(instr(lower(content), lower(?)) > 0 OR instr(lower(semantic_key), lower(?)) > 0 OR instr(lower(subject), lower(?)) > 0 OR instr(lower(predicate), lower(?)) > 0)`)
		searchArgs = append(searchArgs, term, term, term, term)
	}
	candidateLimit := limit * 10
	if candidateLimit < 50 {
		candidateLimit = 50
	}
	if candidateLimit > 500 {
		candidateLimit = 500
	}
	sqlText := `SELECT id, scope, tier, kind, semantic_key, subject, predicate,
value_json, qualifiers_json, cardinality, content, normalized_content,
content_hash, status, importance, confidence, sensitivity, source_type,
source_session_id, source_message_id, source_reference, supersedes_id,
conflict_group_id, valid_from, valid_to, expires_at, revision, created_at,
updated_at, last_accessed_at, access_count FROM memory_entries WHERE ` + where +
		` AND (` + strings.Join(clauses, " OR ") + `) LIMIT ?`
	searchArgs = append(searchArgs, candidateLimit)
	rows, err := q.QueryContext(ctx, sqlText, searchArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]SearchResult, 0, candidateLimit)
	for rows.Next() {
		entry, scanErr := scanEntry(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		results = append(results, SearchResult{Entry: entry, Score: memorySearchScore(entry, terms)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortMemorySearchResults(results)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func sortMemorySearchResults(results []SearchResult) {
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		if results[i].Entry.Importance != results[j].Entry.Importance {
			return results[i].Entry.Importance > results[j].Entry.Importance
		}
		if !results[i].Entry.UpdatedAt.Equal(results[j].Entry.UpdatedAt) {
			return results[i].Entry.UpdatedAt.After(results[j].Entry.UpdatedAt)
		}
		return results[i].Entry.ID < results[j].Entry.ID
	})
}

func memorySearchTerms(query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil
	}
	seen := make(map[string]struct{})
	terms := make([]string, 0, 16)
	add := func(raw string) {
		if len(terms) >= DefaultMemoryMaxSearchTerms {
			return
		}
		term := strings.TrimSpace(raw)
		if !memorySearchTermUseful(term) {
			return
		}
		if _, exists := seen[term]; exists {
			return
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
	}
	flushWord := func(word *[]rune) {
		if len(*word) == 0 {
			return
		}
		add(string(*word))
		*word = (*word)[:0]
	}
	flushHan := func(han *[]rune) {
		if len(*han) == 0 {
			return
		}
		if len(*han) <= 24 {
			add(string(*han))
		}
		for i := 0; i+1 < len(*han); i++ {
			add(string((*han)[i : i+2]))
		}
		*han = (*han)[:0]
	}

	var word, han []rune
	for _, r := range query {
		switch {
		case unicode.Is(unicode.Han, r):
			flushWord(&word)
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '.' || r == ':':
			flushHan(&han)
			word = append(word, r)
		default:
			flushWord(&word)
			flushHan(&han)
		}
	}
	flushWord(&word)
	flushHan(&han)
	return terms
}

func memorySearchTermUseful(term string) bool {
	runes := []rune(term)
	if len(runes) == 0 {
		return false
	}
	for _, r := range runes {
		if unicode.Is(unicode.Han, r) {
			return len(runes) >= 2
		}
	}
	if len(runes) >= 3 {
		return true
	}
	return strings.ContainsAny(term, "0123456789_-.:")
}

func memorySearchScore(entry Entry, terms []string) float64 {
	fields := []struct {
		value  string
		weight float64
	}{
		{entry.SemanticKey, 120},
		{entry.Subject, 90},
		{entry.Predicate, 90},
		{entry.Content, 10},
	}
	score := 0.0
	for _, term := range terms {
		for _, field := range fields {
			value := strings.ToLower(field.value)
			if value == "" || !strings.Contains(value, term) {
				continue
			}
			// A memory is a document, not a bag-of-words count. Counting every
			// repeated occurrence lets a pathological or verbose entry outrank a
			// short, precise entry merely by repeating a common term thousands of
			// times. One match per term/field keeps ranking bounded and stable.
			score += field.weight
		}
	}
	return score
}

func normalizeText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(text))), " ")
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(normalizeText(text)))
	return hex.EncodeToString(sum[:])
}

func newID(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + fmt.Sprintf("-%d", time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}

func clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func marshalOrEmpty(v any, empty string) (string, error) {
	if v == nil {
		return empty, nil
	}
	raw, err := json.Marshal(v)
	return string(raw), err
}

func entryReference(e Entry) Reference {
	return Reference{ID: e.ID, Revision: e.Revision, Tier: e.Tier, Kind: e.Kind, Content: e.Content, UpdatedAt: e.UpdatedAt}
}

func trimToBudget(refs []Reference, budget int) ([]Reference, int) {
	if budget <= 0 {
		budget = defaultRecallBudget
	}
	var out []Reference
	used := 0
	for _, ref := range refs {
		cost := tokens.EstimateInt(ref.Content) + tokens.EstimateInt(ref.ID) + 12
		if len(out) > 0 && used+cost > budget {
			break
		}
		if cost > budget && len(out) == 0 {
			ref.Content = tokens.TakePrefixForTokenBudget(ref.Content, float64(budget-12))
			cost = tokens.EstimateInt(ref.Content) + tokens.EstimateInt(ref.ID) + 12
		}
		out = append(out, ref)
		used += cost
	}
	return out, used
}
