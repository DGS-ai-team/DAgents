package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func (s *Store) get(ctx context.Context, id string, includeInactive bool) (Entry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Entry{}, ErrNotFound
	}
	where := `scope = ? AND id = ?`
	args := []any{string(s.scope), id}
	if !includeInactive {
		active, activeArgs := eligibleWhere(time.Now().UTC())
		where += ` AND ` + active
		args = append(args, activeArgs...)
	}
	entries, err := s.list(ctx, where, args...)
	if err != nil {
		return Entry{}, err
	}
	if len(entries) == 0 {
		return Entry{}, ErrNotFound
	}
	return entries[0], nil
}

// List is used by the settings API. Model recall
// must use Recall/Search so it never accidentally injects inactive entries.
func (s *Store) List(ctx context.Context, includeInactive bool) ([]Entry, error) {
	where := `scope = ?`
	args := []any{string(s.scope)}
	if !includeInactive {
		active, activeArgs := eligibleWhere(time.Now().UTC())
		where += ` AND ` + active
		args = append(args, activeArgs...)
	}
	entries, err := s.list(ctx, where+` ORDER BY importance DESC, updated_at DESC, id ASC`, args...)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) replaceAll(ctx context.Context, entries []Entry) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_revisions WHERE memory_id IN (SELECT id FROM memory_entries WHERE scope=?)`, string(s.scope)); err != nil {
		return err
	}
	if err := deleteAllFTSTx(ctx, tx, s.scope, s.ftsAvailable()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_entries WHERE scope=?`, string(s.scope)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM memory_conflicts`); err != nil {
		return err
	}
	for _, entry := range entries {
		entry.Scope = s.scope
		if strings.TrimSpace(entry.Content) == "" {
			continue
		}
		entry = normalizeEntry(entry)
		if err := insertEntryTx(ctx, tx, entry, s.ftsAvailable()); err != nil {
			return err
		}
		if err := insertRevisionTx(ctx, tx, entry, "settings_replace_all"); err != nil {
			return err
		}
	}
	if _, err := s.nextRevision(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func deleteAllFTSTx(ctx context.Context, tx *sql.Tx, scope Scope, ftsReady bool) error {
	if !ftsReady {
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM memory_fts WHERE memory_id IN (SELECT id FROM memory_entries WHERE scope=?)`, string(scope))
	return err
}

func normalizeEntry(entry Entry) Entry {
	if entry.ID == "" {
		entry.ID = newID("mem")
	}
	if entry.Tier != TierCore && entry.Tier != TierRecall {
		entry.Tier = TierRecall
	}
	if entry.Kind == "" {
		entry.Kind = KindFact
	}
	if entry.Status == "" {
		entry.Status = StatusActive
	}
	if entry.ContentHash == "" {
		entry.ContentHash = hashText(entry.Content)
	}
	if entry.NormalizedText == "" {
		entry.NormalizedText = normalizeText(entry.Content)
	}
	if entry.Cardinality == "" {
		entry.Cardinality = "unknown"
	}
	if entry.Importance == 0 {
		entry.Importance = 50
	}
	if entry.Confidence == 0 {
		entry.Confidence = 100
	}
	if entry.SourceType == "" {
		entry.SourceType = "explicit_user"
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.UpdatedAt.IsZero() {
		entry.UpdatedAt = entry.CreatedAt
	}
	if entry.Revision <= 0 {
		entry.Revision = 1
	}
	entry.Importance = clamp(entry.Importance, 0, 100)
	entry.Confidence = clamp(entry.Confidence, 0, 100)
	return entry
}

func (s *Store) potentialConflicts(ctx context.Context, candidate Entry) ([]Entry, error) {
	where := `scope = ? AND status IN (?, ?)`
	args := []any{string(s.scope), string(StatusActive), string(StatusConflicted)}
	if candidate.SemanticKey != "" {
		where += ` AND semantic_key = ?`
		args = append(args, candidate.SemanticKey)
	} else if candidate.Subject != "" && candidate.Predicate != "" {
		where += ` AND subject = ? AND predicate = ?`
		args = append(args, candidate.Subject, candidate.Predicate)
	} else {
		where += ` AND content_hash = ?`
		args = append(args, candidate.ContentHash)
	}
	entries, err := s.list(ctx, where, args...)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) insert(ctx context.Context, entry Entry, reason string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := insertEntryTx(ctx, tx, entry, s.ftsAvailable()); err != nil {
		return 0, err
	}
	if err := insertRevisionTx(ctx, tx, entry, reason); err != nil {
		return 0, err
	}
	revision, err := s.nextRevision(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) createConflict(ctx context.Context, candidate Entry, existing []Entry, description string) (Conflict, error) {
	conflictID := newID("conflict")
	candidate.Status = StatusPending
	candidate.ConflictGroup = conflictID
	ids := make([]string, 0, len(existing))
	revisions := make([]int64, 0, len(existing))
	for _, entry := range existing {
		ids = append(ids, entry.ID)
		revisions = append(revisions, entry.Revision)
	}
	candidateRaw, err := json.Marshal(candidate)
	if err != nil {
		return Conflict{}, err
	}
	idsRaw, err := json.Marshal(ids)
	if err != nil {
		return Conflict{}, err
	}
	revisionsRaw, err := json.Marshal(revisions)
	if err != nil {
		return Conflict{}, err
	}
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Conflict{}, err
	}
	defer tx.Rollback()
	if err := insertEntryTx(ctx, tx, candidate, s.ftsAvailable()); err != nil {
		return Conflict{}, err
	}
	if err := insertRevisionTx(ctx, tx, candidate, "remember_conflict_pending"); err != nil {
		return Conflict{}, err
	}
	revision, err := s.nextRevision(ctx, tx)
	if err != nil {
		return Conflict{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memory_conflicts(
id, candidate_json, existing_ids_json, existing_revisions_json, relation,
description, store_revision, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, conflictID, string(candidateRaw), string(idsRaw), string(revisionsRaw),
		"contradicts", strings.TrimSpace(description), revision, "pending", now.Format(time.RFC3339Nano)); err != nil {
		return Conflict{}, err
	}
	if err := tx.Commit(); err != nil {
		return Conflict{}, err
	}
	return Conflict{ID: conflictID, Candidate: candidate, Existing: existing, ExistingRevisions: revisions,
		Relation: "contradicts", Description: strings.TrimSpace(description), StoreRevision: revision, CreatedAt: now}, nil
}

func (s *Store) updateStatus(ctx context.Context, entry Entry, status Status, reason string) (int64, error) {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	updatedRevision := entry.Revision + 1
	result, err := tx.ExecContext(ctx, `UPDATE memory_entries SET status=?, revision=?, updated_at=? WHERE id=? AND scope=? AND revision=?`,
		string(status), updatedRevision, now.Format(time.RFC3339Nano), entry.ID, string(s.scope), entry.Revision)
	if err != nil {
		return 0, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return 0, fmt.Errorf("memory entry revision conflict")
	}
	if err := deleteFTSTx(ctx, tx, entry.ID, s.ftsAvailable()); err != nil {
		return 0, err
	}
	updated := entry
	updated.Status, updated.Revision, updated.UpdatedAt = status, updatedRevision, now
	if status == StatusActive || status == StatusConflicted {
		if err := insertFTSTx(ctx, tx, updated, s.ftsAvailable()); err != nil {
			return 0, err
		}
	}
	if err := insertRevisionTx(ctx, tx, updated, reason); err != nil {
		return 0, err
	}
	revision, err := s.nextRevision(ctx, tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) updateContent(ctx context.Context, id, content string) (Entry, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return Entry{}, fmt.Errorf("memory content is required")
	}
	entry, err := s.get(ctx, id, false)
	if err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()
	next := entry.Revision + 1
	normalized := normalizeText(content)
	hash := hashText(content)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Entry{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE memory_entries SET content=?, normalized_content=?, content_hash=?, revision=?, updated_at=? WHERE id=? AND scope=? AND revision=?`,
		content, normalized, hash, next, now.Format(time.RFC3339Nano), id, string(s.scope), entry.Revision)
	if err != nil {
		return Entry{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Entry{}, fmt.Errorf("memory entry revision conflict")
	}
	if err := deleteFTSTx(ctx, tx, id, s.ftsAvailable()); err != nil {
		return Entry{}, err
	}
	entry.Content, entry.NormalizedText, entry.ContentHash = content, normalized, hash
	entry.Revision, entry.UpdatedAt = next, now
	if err := insertFTSTx(ctx, tx, entry, s.ftsAvailable()); err != nil {
		return Entry{}, err
	}
	if err := insertRevisionTx(ctx, tx, entry, "settings_update_content"); err != nil {
		return Entry{}, err
	}
	if _, err := s.nextRevision(ctx, tx); err != nil {
		return Entry{}, err
	}
	if err := tx.Commit(); err != nil {
		return Entry{}, err
	}
	return entry, nil
}

func insertEntryTx(ctx context.Context, tx *sql.Tx, entry Entry, ftsReady bool) error {
	valueRaw, err := marshalOrEmpty(entry.Value, "null")
	if err != nil {
		return err
	}
	qualifiersRaw, err := marshalOrEmpty(entry.Qualifiers, "{}")
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_entries(
id, scope, tier, kind, semantic_key, subject, predicate, value_json,
qualifiers_json, cardinality, content, normalized_content, content_hash,
status, importance, confidence, sensitivity, source_type, source_session_id,
source_message_id, source_reference, supersedes_id, conflict_group_id,
valid_from, valid_to, expires_at, revision, created_at, updated_at,
last_accessed_at, access_count)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, string(entry.Scope), string(entry.Tier), string(entry.Kind), entry.SemanticKey,
		entry.Subject, entry.Predicate, valueRaw, qualifiersRaw, entry.Cardinality, entry.Content,
		entry.NormalizedText, entry.ContentHash, string(entry.Status), entry.Importance, entry.Confidence,
		entry.Sensitivity, entry.SourceType, entry.SourceSession, entry.SourceMessage, entry.SourceRef,
		firstString(entry.SupersedesID, ""), firstString(entry.ConflictGroup, ""), formatTime(entry.ValidFrom),
		formatTime(entry.ValidTo), formatTime(entry.ExpiresAt), entry.Revision, formatTimeValue(entry.CreatedAt),
		formatTimeValue(entry.UpdatedAt), formatTime(entry.LastAccessedAt), entry.AccessCount)
	if err != nil {
		return err
	}
	return insertFTSTx(ctx, tx, entry, ftsReady)
}

func insertFTSTx(ctx context.Context, tx *sql.Tx, entry Entry, ftsReady bool) error {
	if !ftsReady || (entry.Status != StatusActive && entry.Status != StatusConflicted) {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO memory_fts(memory_id, semantic_key, subject, predicate, content) VALUES (?, ?, ?, ?, ?)`,
		entry.ID, entry.SemanticKey, entry.Subject, entry.Predicate, entry.Content)
	return err
}

func deleteFTSTx(ctx context.Context, tx *sql.Tx, id string, ftsReady bool) error {
	if !ftsReady {
		return nil
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM memory_fts WHERE memory_id = ?`, id)
	return err
}

func insertRevisionTx(ctx context.Context, tx *sql.Tx, entry Entry, reason string) error {
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memory_revisions(memory_id, revision, snapshot_json, changed_by, change_reason, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Revision, string(raw), firstString(entry.SourceType, "memory"), strings.TrimSpace(reason), formatTimeValue(time.Now().UTC()))
	return err
}

func formatTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func formatTimeValue(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
