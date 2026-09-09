package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrHandbookConflict = errors.New("handbook revision conflict")
	ErrHandbookNotFound = errors.New("handbook version not found")
	ErrHandbookInvalid  = errors.New("invalid handbook candidate")
)

type HandbookEvidence struct {
	Source    string `json:"source"`
	Reference string `json:"reference,omitempty"`
}
type HandbookValidation struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}
type HandbookCandidate struct {
	Content    string               `json:"content"`
	Evidence   []HandbookEvidence   `json:"evidence"`
	Validation []HandbookValidation `json:"validation"`
	Reason     string               `json:"reason"`
}
type HandbookVersion struct {
	AgentID    string               `json:"agent_id"`
	Scope      Scope                `json:"scope"`
	Revision   int64                `json:"revision"`
	Content    string               `json:"content"`
	Evidence   []HandbookEvidence   `json:"evidence"`
	Validation []HandbookValidation `json:"validation"`
	Reason     string               `json:"reason"`
	CreatedAt  time.Time            `json:"created_at"`
}

// LocalHandbookService binds all handbook operations to one agent-owned store
// and identity. Callers cannot select a different agent or the global scope.
type LocalHandbookService struct {
	store   *Store
	agentID string
}

func NewLocalHandbookService(store *Store, agentID string) (*LocalHandbookService, error) {
	if store == nil || store.scope != ScopeAgent {
		return nil, ErrHandbookInvalid
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrHandbookInvalid
	}
	return &LocalHandbookService{store: store, agentID: agentID}, nil
}

func (l *LocalHandbookService) Get(ctx context.Context) (*HandbookVersion, error) {
	if l == nil || l.store == nil {
		return nil, ErrHandbookInvalid
	}
	return l.store.GetHandbook(ctx, l.agentID)
}
func (l *LocalHandbookService) List(ctx context.Context) ([]HandbookVersion, error) {
	if l == nil || l.store == nil {
		return nil, ErrHandbookInvalid
	}
	return l.store.ListHandbookVersions(ctx, l.agentID)
}
func (l *LocalHandbookService) Submit(ctx context.Context, candidate HandbookCandidate, expectedRevision int64) (*HandbookVersion, error) {
	if l == nil || l.store == nil {
		return nil, ErrHandbookInvalid
	}
	return l.store.SubmitHandbook(ctx, l.agentID, candidate, expectedRevision)
}
func (l *LocalHandbookService) Rollback(ctx context.Context, revision, expectedRevision int64) (*HandbookVersion, error) {
	if l == nil || l.store == nil {
		return nil, ErrHandbookInvalid
	}
	return l.store.RollbackHandbook(ctx, l.agentID, revision, expectedRevision)
}

func (s *Store) initHandbookSchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS agent_handbook_versions (agent_id TEXT NOT NULL, scope TEXT NOT NULL, revision INTEGER NOT NULL, candidate_json TEXT NOT NULL, reason TEXT NOT NULL, created_at TEXT NOT NULL, is_current INTEGER NOT NULL DEFAULT 0, PRIMARY KEY(agent_id, scope, revision)); CREATE INDEX IF NOT EXISTS idx_agent_handbook_current ON agent_handbook_versions(agent_id, scope, is_current); CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_handbook_one_current ON agent_handbook_versions(agent_id, scope) WHERE is_current=1`)
	return err
}

func validateHandbook(c HandbookCandidate) error {
	if strings.TrimSpace(c.Content) == "" || len(c.Content) > 32*1024 || len(c.Evidence) == 0 || len(c.Evidence) > 64 || len(c.Validation) == 0 || len(c.Validation) > 64 || len(c.Reason) > 2000 {
		return ErrHandbookInvalid
	}
	for _, e := range c.Evidence {
		if strings.TrimSpace(e.Source) == "" || len(e.Source) > 512 || len(e.Reference) > 2048 {
			return ErrHandbookInvalid
		}
	}
	for _, v := range c.Validation {
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 256 || len(v.Detail) > 2048 || !v.Passed {
			return ErrHandbookInvalid
		}
	}
	return nil
}

func (s *Store) GetHandbook(ctx context.Context, agentID string) (*HandbookVersion, error) {
	if s.scope != ScopeAgent {
		return nil, ErrHandbookInvalid
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrHandbookInvalid
	}
	row := s.db.QueryRowContext(ctx, `SELECT revision,candidate_json,reason,created_at FROM agent_handbook_versions WHERE agent_id=? AND scope=? AND is_current=1`, agentID, string(s.scope))
	return scanHandbook(row, agentID, s.scope)
}
func (s *Store) ListHandbookVersions(ctx context.Context, agentID string) ([]HandbookVersion, error) {
	if s.scope != ScopeAgent {
		return nil, ErrHandbookInvalid
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrHandbookInvalid
	}
	rows, err := s.db.QueryContext(ctx, `SELECT revision,candidate_json,reason,created_at FROM agent_handbook_versions WHERE agent_id=? AND scope=? ORDER BY revision`, agentID, string(s.scope))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HandbookVersion{}
	for rows.Next() {
		v, err := scanHandbookRow(rows, agentID, s.scope)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}
func (s *Store) SubmitHandbook(ctx context.Context, agentID string, candidate HandbookCandidate, expectedRevision int64) (*HandbookVersion, error) {
	if s.scope != ScopeAgent || expectedRevision < 0 || expectedRevision == math.MaxInt64 {
		return nil, ErrHandbookInvalid
	}
	if err := validateHandbook(candidate); err != nil {
		return nil, err
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrHandbookInvalid
	}
	raw, _ := json.Marshal(candidate)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM agent_handbook_versions WHERE agent_id=? AND scope=? AND is_current=1`, agentID, string(s.scope)).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	}
	if current != expectedRevision {
		return nil, ErrHandbookConflict
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_handbook_versions SET is_current=0 WHERE agent_id=? AND scope=? AND is_current=1`, agentID, string(s.scope)); err != nil {
		return nil, err
	}
	next := current + 1
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_handbook_versions(agent_id,scope,revision,candidate_json,reason,created_at,is_current) VALUES(?,?,?,?,?,?,1)`, agentID, string(s.scope), next, raw, candidate.Reason, now.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &HandbookVersion{AgentID: agentID, Scope: s.scope, Revision: next, Content: candidate.Content, Evidence: append([]HandbookEvidence(nil), candidate.Evidence...), Validation: append([]HandbookValidation(nil), candidate.Validation...), Reason: candidate.Reason, CreatedAt: now}, nil
}
func (s *Store) RollbackHandbook(ctx context.Context, agentID string, revision, expectedRevision int64) (*HandbookVersion, error) {
	if s.scope != ScopeAgent || revision <= 0 || expectedRevision < 0 || expectedRevision == math.MaxInt64 {
		return nil, ErrHandbookInvalid
	}
	old, err := s.handbookVersion(ctx, agentID, revision)
	if err != nil {
		return nil, err
	}
	oldCandidate := HandbookCandidate{Content: old.Content, Evidence: old.Evidence, Validation: old.Validation, Reason: fmt.Sprintf("rollback to revision %d", revision)}
	return s.SubmitHandbook(ctx, agentID, oldCandidate, expectedRevision)
}
func (s *Store) handbookVersion(ctx context.Context, agentID string, revision int64) (*HandbookVersion, error) {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrHandbookInvalid
	}
	row := s.db.QueryRowContext(ctx, `SELECT revision,candidate_json,reason,created_at FROM agent_handbook_versions WHERE agent_id=? AND scope=? AND revision=?`, agentID, string(s.scope), revision)
	return scanHandbook(row, agentID, s.scope)
}

type handbookScanner interface{ Scan(...any) error }

func scanHandbook(row handbookScanner, agentID string, scope Scope) (*HandbookVersion, error) {
	return scanHandbookRow(row, agentID, scope)
}
func scanHandbookRow(row handbookScanner, agentID string, scope Scope) (*HandbookVersion, error) {
	var v HandbookVersion
	var raw, created string
	if err := row.Scan(&v.Revision, &raw, &v.Reason, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrHandbookNotFound
		}
		return nil, err
	}
	var c HandbookCandidate
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return nil, err
	}
	v.AgentID, v.Scope, v.Content = agentID, scope, c.Content
	v.Evidence = append([]HandbookEvidence(nil), c.Evidence...)
	v.Validation = append([]HandbookValidation(nil), c.Validation...)
	v.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &v, nil
}
