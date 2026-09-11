package memory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrMaintenanceConflict = errors.New("maintenance operation conflict")

type MaintenanceCursor struct {
	AgentID              string    `json:"agent_id"`
	Scope                Scope     `json:"scope"`
	Sequence             int64     `json:"sequence"`
	SourceFingerprint    string    `json:"source_fingerprint"`
	CandidateFingerprint string    `json:"candidate_fingerprint"`
	Revision             int64     `json:"revision"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type MaintenanceOperation struct {
	OperationID          string    `json:"operation_id"`
	AgentID              string    `json:"agent_id"`
	Scope                Scope     `json:"scope"`
	SourceFingerprint    string    `json:"source_fingerprint"`
	CandidateFingerprint string    `json:"candidate_fingerprint"`
	ExpectedCursor       int64     `json:"expected_cursor"`
	NextCursor           int64     `json:"next_cursor"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// ApplyMaintenanceOperation is the only maintenance write boundary.

func (s *Store) GetMaintenanceOperation(ctx context.Context, id string) (MaintenanceOperation, error) {
	op, err := scanMaintenanceOperation(ctx, s.db, strings.TrimSpace(id))
	if errors.Is(err, sql.ErrNoRows) {
		return MaintenanceOperation{}, ErrNotFound
	}
	return op, err
}

// GetMaintenanceOperation reads an operation only through the service's
// private Agent store. It is intentionally independent of the mutable model
// scope, so a global scope selection cannot widen recovery access.
func (s *LocalService) GetMaintenanceOperation(ctx context.Context, id string) (MaintenanceOperation, error) {
	if s == nil || s.agent == nil || strings.TrimSpace(s.agentID) == "" || strings.TrimSpace(id) == "" {
		return MaintenanceOperation{}, ErrNotFound
	}
	op, err := s.agent.GetMaintenanceOperation(ctx, strings.TrimSpace(id))
	if err != nil {
		return MaintenanceOperation{}, err
	}
	if op.Scope != ScopeAgent || op.AgentID != s.agentID {
		return MaintenanceOperation{}, ErrNotFound
	}
	return op, nil
}

func scanMaintenanceOperation(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string) (MaintenanceOperation, error) {
	var op MaintenanceOperation
	var scope, created, updated string
	err := q.QueryRowContext(ctx, `SELECT operation_id,scope,agent_id,source_fingerprint,candidate_fingerprint,expected_cursor,next_cursor,status,created_at,updated_at FROM maintenance_operations WHERE operation_id=?`, id).Scan(&op.OperationID, &scope, &op.AgentID, &op.SourceFingerprint, &op.CandidateFingerprint, &op.ExpectedCursor, &op.NextCursor, &op.Status, &created, &updated)
	if err != nil {
		return op, err
	}
	op.Scope = Scope(scope)
	op.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	op.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return op, nil
}

func (s *Store) GetMaintenanceCursor(ctx context.Context, agentID string) (MaintenanceCursor, error) {
	var c MaintenanceCursor
	var scope, updated string
	err := s.db.QueryRowContext(ctx, `SELECT agent_id,scope,sequence,source_fingerprint,revision,updated_at FROM maintenance_cursors WHERE agent_id=? AND scope=?`, strings.TrimSpace(agentID), string(s.scope)).Scan(&c.AgentID, &scope, &c.Sequence, &c.SourceFingerprint, &c.Revision, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return MaintenanceCursor{AgentID: strings.TrimSpace(agentID), Scope: s.scope}, nil
	}
	c.Scope = Scope(scope)
	c.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return c, err
}

// ApplyMaintenanceOperation atomically commits the operation record, candidate
// effects, and cursor. Queue submission alone never advances the cursor.
func (s *LocalService) ApplyMaintenanceOperation(ctx context.Context, op MaintenanceOperation, candidates []Candidate, next MaintenanceCursor) ([]WriteResult, error) {
	if s == nil {
		return nil, fmt.Errorf("memory service unavailable")
	}
	op.OperationID = strings.TrimSpace(op.OperationID)
	op.AgentID = strings.TrimSpace(op.AgentID)
	op.SourceFingerprint = strings.TrimSpace(op.SourceFingerprint)
	next.AgentID = strings.TrimSpace(next.AgentID)
	st, err := s.storeFor(op.Scope)
	if err != nil {
		return nil, err
	}
	if op.OperationID == "" || op.Scope != ScopeAgent || op.AgentID == "" || (s.agentID != "" && s.agentID != op.AgentID) || next.AgentID != op.AgentID || next.Scope != ScopeAgent || op.SourceFingerprint == "" || next.SourceFingerprint != op.SourceFingerprint || next.Sequence != op.NextCursor || next.Sequence < op.ExpectedCursor {
		return nil, fmt.Errorf("invalid maintenance operation identity")
	}
	for _, candidate := range candidates {
		if candidate.Request.Scope != ScopeAgent || candidate.Request.AgentID != op.AgentID {
			return nil, fmt.Errorf("maintenance candidate identity mismatch")
		}
	}
	raw, err := json.Marshal(candidates)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(raw)
	op.CandidateFingerprint = hex.EncodeToString(digest[:])
	next.CandidateFingerprint = op.CandidateFingerprint
	// The operation record, all candidate effects, and the cursor CAS must be
	// one SQLite transaction. This makes a failed CAS or SQL error leave no
	// pending/active memory rows behind.
	s.consolidateMu.Lock()
	defer s.consolidateMu.Unlock()
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var status, agent, sourceFP, candidateFP string
	var expected, nextSeq int64
	err = tx.QueryRowContext(ctx, `SELECT status,agent_id,source_fingerprint,candidate_fingerprint,expected_cursor,next_cursor FROM maintenance_operations WHERE operation_id=?`, op.OperationID).Scan(&status, &agent, &sourceFP, &candidateFP, &expected, &nextSeq)
	if errors.Is(err, sql.ErrNoRows) {
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx, `INSERT INTO maintenance_operations(operation_id,scope,agent_id,source_fingerprint,candidate_fingerprint,expected_cursor,next_cursor,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, op.OperationID, string(op.Scope), op.AgentID, op.SourceFingerprint, op.CandidateFingerprint, op.ExpectedCursor, op.NextCursor, "prepared", now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if err != nil {
			return nil, err
		}
		status, agent, sourceFP, candidateFP, expected, nextSeq = "prepared", op.AgentID, op.SourceFingerprint, op.CandidateFingerprint, op.ExpectedCursor, op.NextCursor
	} else if err != nil {
		return nil, err
	} else if agent != op.AgentID || sourceFP != op.SourceFingerprint || candidateFP != op.CandidateFingerprint || expected != op.ExpectedCursor || nextSeq != op.NextCursor || status != "prepared" && status != "applied" {
		return nil, fmt.Errorf("%w: maintenance operation", ErrMaintenanceConflict)
	}
	if status == "applied" {
		return nil, tx.Commit()
	}
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT sequence FROM maintenance_cursors WHERE agent_id=? AND scope=?`, op.AgentID, string(ScopeAgent)).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		current = 0
	} else if err != nil {
		return nil, err
	}
	if current != op.ExpectedCursor {
		return nil, fmt.Errorf("%w: maintenance cursor", ErrMaintenanceConflict)
	}
	results := make([]WriteResult, 0, len(candidates))
	for _, candidate := range candidates {
		result, err := st.consolidateCandidateTx(ctx, tx, candidate.Request)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}
	now := time.Now().UTC()
	result, err := tx.ExecContext(ctx, `INSERT INTO maintenance_cursors(agent_id,scope,sequence,source_fingerprint,revision,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET sequence=excluded.sequence,source_fingerprint=excluded.source_fingerprint,revision=maintenance_cursors.revision+1,updated_at=excluded.updated_at WHERE maintenance_cursors.sequence=?`, op.AgentID, string(ScopeAgent), next.Sequence, next.SourceFingerprint, 1, now.Format(time.RFC3339Nano), op.ExpectedCursor)
	if err != nil {
		return results, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return results, fmt.Errorf("%w: maintenance cursor", ErrMaintenanceConflict)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE maintenance_operations SET status='applied',updated_at=? WHERE operation_id=? AND status='prepared'`, now.Format(time.RFC3339Nano), op.OperationID); err != nil {
		return results, err
	}
	if err = tx.Commit(); err != nil {
		return results, err
	}
	return results, nil
}

// GetMaintenanceCursor returns the durable cursor for this service's scope.
func (s *LocalService) GetMaintenanceCursor(ctx context.Context) (MaintenanceCursor, error) {
	if s == nil || s.agent == nil {
		return MaintenanceCursor{}, fmt.Errorf("memory service unavailable")
	}
	return s.agent.GetMaintenanceCursor(ctx, s.agentID)
}

func (s *Store) consolidateCandidateTx(ctx context.Context, tx *sql.Tx, req RememberRequest) (WriteResult, error) {
	req.Scope = ScopeAgent
	req.Tier = TierRecall
	if strings.TrimSpace(req.SourceType) == "" {
		req.SourceType = "model_inference"
	}
	candidate := newEntry(req)
	candidate.Status = StatusPending
	potential, err := s.potentialConflictsWith(ctx, tx, candidate)
	if err != nil {
		return WriteResult{}, err
	}
	for _, existing := range potential {
		if existing.ContentHash == candidate.ContentHash {
			revision, _ := revisionFrom(ctx, tx)
			return WriteResult{Outcome: WriteDuplicate, Entry: &existing, ExistingID: existing.ID, StoreRevision: revision}, nil
		}
	}
	if conflicts := classifyDeterministic(candidate, potential); len(conflicts) > 0 {
		conflict, _, err := s.createConflictTx(ctx, tx, candidate, conflicts, "deterministic semantic conflict from background extraction")
		if err != nil {
			return WriteResult{}, err
		}
		return WriteResult{Outcome: WritePendingConflict, Entry: &candidate, Conflict: &conflict, StoreRevision: conflict.StoreRevision}, nil
	}
	if _, err := s.insertTx(ctx, tx, candidate, "candidate_pending"); err != nil {
		return WriteResult{}, err
	}
	storeRevision, err := s.updateStatusTx(ctx, tx, candidate, StatusActive, "candidate_consolidated")
	if err != nil {
		return WriteResult{}, err
	}
	candidate.Status = StatusActive
	candidate.Revision++
	candidate.UpdatedAt = time.Now().UTC()
	return WriteResult{Outcome: WriteAdded, Entry: &candidate, StoreRevision: storeRevision}, nil
}
