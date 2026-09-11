package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type storedConflict struct {
	ID                string
	Candidate         Entry
	ExistingIDs       []string
	ExistingRevisions []int64
	Relation          string
	Description       string
	StoreRevision     int64
	Status            string
	CreatedAt         time.Time
}

func (s *LocalService) ResolveConflict(ctx context.Context, scope Scope, conflictID string, decision ConflictDecision) (WriteResult, error) {
	resolved, err := s.modelScope(scope)
	if err != nil {
		return WriteResult{}, err
	}
	st, err := s.storeFor(resolved)
	if err != nil {
		return WriteResult{}, err
	}
	return st.resolveConflict(ctx, strings.TrimSpace(conflictID), decision)
}

func (s *Store) loadConflict(ctx context.Context, id string) (storedConflict, error) {
	var conflict storedConflict
	var candidateRaw, idsRaw, revisionsRaw, created string
	if err := s.db.QueryRowContext(ctx, `SELECT id, candidate_json, existing_ids_json,
existing_revisions_json, relation, description, store_revision, status, created_at
FROM memory_conflicts WHERE id=?`, id).Scan(&conflict.ID, &candidateRaw, &idsRaw, &revisionsRaw,
		&conflict.Relation, &conflict.Description, &conflict.StoreRevision, &conflict.Status, &created); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedConflict{}, ErrNotFound
		}
		return storedConflict{}, err
	}
	if err := json.Unmarshal([]byte(candidateRaw), &conflict.Candidate); err != nil {
		return storedConflict{}, err
	}
	if err := json.Unmarshal([]byte(idsRaw), &conflict.ExistingIDs); err != nil {
		return storedConflict{}, err
	}
	if err := json.Unmarshal([]byte(revisionsRaw), &conflict.ExistingRevisions); err != nil {
		return storedConflict{}, err
	}
	conflict.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return conflict, nil
}

func (s *Store) resolveConflict(ctx context.Context, id string, decision ConflictDecision) (WriteResult, error) {
	if id == "" {
		return WriteResult{}, fmt.Errorf("conflict id is required")
	}
	switch decision {
	case ConflictKeepOld, ConflictUseNew, ConflictKeepBoth, ConflictCancel:
	default:
		return WriteResult{}, fmt.Errorf("unsupported memory conflict decision %q", decision)
	}
	conflict, err := s.loadConflict(ctx, id)
	if err != nil {
		return WriteResult{}, err
	}
	if conflict.Status != "pending" {
		return WriteResult{}, fmt.Errorf("memory conflict %q is %s", id, conflict.Status)
	}
	if len(conflict.ExistingIDs) != len(conflict.ExistingRevisions) {
		return WriteResult{}, fmt.Errorf("memory conflict %q has invalid revision metadata", id)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WriteResult{}, err
	}
	defer tx.Rollback()
	currentCandidate, err := getEntryTx(ctx, tx, s.scope, conflict.Candidate.ID, true)
	if err != nil {
		return WriteResult{}, err
	}
	if currentCandidate.Status != StatusPending || currentCandidate.Revision != conflict.Candidate.Revision {
		_ = markConflictStaleTx(ctx, tx, id)
		_ = tx.Commit()
		return WriteResult{}, fmt.Errorf("memory conflict %q is stale", id)
	}
	existing := make([]Entry, 0, len(conflict.ExistingIDs))
	for i, existingID := range conflict.ExistingIDs {
		entry, getErr := getEntryTx(ctx, tx, s.scope, existingID, true)
		if getErr != nil {
			return WriteResult{}, getErr
		}
		if entry.Revision != conflict.ExistingRevisions[i] || (entry.Status != StatusActive && entry.Status != StatusConflicted) {
			_ = markConflictStaleTx(ctx, tx, id)
			_ = tx.Commit()
			return WriteResult{}, fmt.Errorf("memory conflict %q is stale", id)
		}
		existing = append(existing, entry)
	}
	now := time.Now().UTC()
	var superseded []string
	var outcome WriteOutcome = WriteAdded
	switch decision {
	case ConflictKeepOld, ConflictCancel:
		currentCandidate, err = mutateEntryTx(ctx, tx, s, currentCandidate, StatusDeleted, "", "", "memory_conflict_rejected")
		if err != nil {
			return WriteResult{}, err
		}
	case ConflictUseNew:
		for _, entry := range existing {
			if _, err := mutateEntryTx(ctx, tx, s, entry, StatusSuperseded, "", "", "memory_conflict_use_new"); err != nil {
				return WriteResult{}, err
			}
			superseded = append(superseded, entry.ID)
		}
		currentCandidate.SupersedesID = ""
		if len(superseded) > 0 {
			currentCandidate.SupersedesID = superseded[0]
		}
		currentCandidate.ConflictGroup = ""
		currentCandidate, err = mutateEntryTx(ctx, tx, s, currentCandidate, StatusActive, currentCandidate.SupersedesID, "", "memory_conflict_use_new")
		if err != nil {
			return WriteResult{}, err
		}
		outcome = WriteSuperseded
	case ConflictKeepBoth:
		for _, entry := range existing {
			if _, err := mutateEntryTx(ctx, tx, s, entry, StatusConflicted, "", conflict.ID, "memory_conflict_keep_both"); err != nil {
				return WriteResult{}, err
			}
		}
		currentCandidate, err = mutateEntryTx(ctx, tx, s, currentCandidate, StatusConflicted, "", conflict.ID, "memory_conflict_keep_both")
		if err != nil {
			return WriteResult{}, err
		}
	}
	storeRevision, err := s.nextRevision(ctx, tx)
	if err != nil {
		return WriteResult{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE memory_conflicts SET status='resolved', resolved_at=? WHERE id=? AND status='pending'`, now.Format(time.RFC3339Nano), id); err != nil {
		return WriteResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return WriteResult{}, err
	}
	currentCandidate.UpdatedAt = now
	return WriteResult{Outcome: outcome, Entry: &currentCandidate, Superseded: superseded, StoreRevision: storeRevision}, nil
}

func getEntryTx(ctx context.Context, tx *sql.Tx, scope Scope, id string, includeInactive bool) (Entry, error) {
	where := `scope=? AND id=?`
	args := []any{string(scope), id}
	if !includeInactive {
		active, activeArgs := eligibleWhere(time.Now().UTC())
		where += ` AND ` + active
		args = append(args, activeArgs...)
	}
	row := tx.QueryRowContext(ctx, `SELECT id, scope, tier, kind, semantic_key, subject, predicate,
value_json, qualifiers_json, cardinality, content, normalized_content,
content_hash, status, importance, confidence, sensitivity, source_type,
source_session_id, source_message_id, source_reference, supersedes_id,
conflict_group_id, valid_from, valid_to, expires_at, revision, created_at,
updated_at, last_accessed_at, access_count FROM memory_entries WHERE `+where, args...)
	entry, err := scanEntry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Entry{}, ErrNotFound
	}
	return entry, err
}

func mutateEntryTx(ctx context.Context, tx *sql.Tx, s *Store, entry Entry, status Status, supersedesID, conflictGroup, reason string) (Entry, error) {
	now := time.Now().UTC()
	next := entry.Revision + 1
	result, err := tx.ExecContext(ctx, `UPDATE memory_entries SET status=?, supersedes_id=?, conflict_group_id=?, revision=?, updated_at=? WHERE id=? AND scope=? AND revision=?`,
		string(status), supersedesID, conflictGroup, next, now.Format(time.RFC3339Nano), entry.ID, string(s.scope), entry.Revision)
	if err != nil {
		return Entry{}, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return Entry{}, fmt.Errorf("memory entry revision conflict")
	}
	if err := deleteFTSTx(ctx, tx, entry.ID, s.ftsAvailable()); err != nil {
		return Entry{}, err
	}
	updated := entry
	updated.Status, updated.SupersedesID, updated.ConflictGroup, updated.Revision, updated.UpdatedAt = status, supersedesID, conflictGroup, next, now
	if err := insertFTSTx(ctx, tx, updated, s.ftsAvailable()); err != nil {
		return Entry{}, err
	}
	if err := insertRevisionTx(ctx, tx, updated, reason); err != nil {
		return Entry{}, err
	}
	return updated, nil
}

func markConflictStaleTx(ctx context.Context, tx *sql.Tx, id string) error {
	if tx == nil {
		return nil
	}
	_, err := tx.ExecContext(ctx, `UPDATE memory_conflicts SET status='stale' WHERE id=? AND status='pending'`, id)
	return err
}
