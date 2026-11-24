package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// InputReceipt is the durable idempotency identity for an accepted human
// message. It stores only the request digest and current disposition; the
// accepted queue payload is owned by conversation_input_queue.
type InputReceipt struct {
	ClientRequestID   string `json:"client_request_id"`
	PayloadDigest     string `json:"payload_digest"`
	ConversationEpoch uint64 `json:"conversation_epoch"`
	InputSeq          uint64 `json:"input_seq"`
	InputRevision     uint64 `json:"input_revision"`
	Status            string `json:"status"`
}

// InputQueueRow stores one accepted external input independently from the
// runtime metadata snapshot. PayloadJSON contains the immutable InputRecord
// envelope; status is the durable queued/claimed ownership state.
type InputQueueRow struct {
	ConversationEpoch uint64
	InputSeq          uint64
	InputRevision     uint64
	Kind              string
	Status            string
	PayloadJSON       json.RawMessage
}

type inputQueueSnapshotHeader struct {
	Sequence             uint64 `json:"seq"`
	ConversationEpoch    uint64 `json:"conversation_epoch"`
	QueueRevision        uint64 `json:"queue_revision"`
	QueuePaused          bool   `json:"queue_paused,omitempty"`
	QueueControlRevision uint64 `json:"queue_control_revision,omitempty"`
	QueueStorageVersion  int    `json:"queue_storage_version"`
}

func (s *SQLiteStore) initConversationInputSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS conversation_inputs (
  conversation_id TEXT NOT NULL,
  client_request_id TEXT NOT NULL,
  payload_digest TEXT NOT NULL,
  conversation_epoch INTEGER NOT NULL,
  input_seq INTEGER NOT NULL,
  input_revision INTEGER NOT NULL,
  status TEXT NOT NULL DEFAULT 'queued',
  created_at TEXT NOT NULL,
  PRIMARY KEY(conversation_id, client_request_id),
  UNIQUE(conversation_id, conversation_epoch, input_seq)
);
CREATE INDEX IF NOT EXISTS idx_conversation_inputs_epoch_seq
  ON conversation_inputs(conversation_id, conversation_epoch, input_seq);
CREATE TABLE IF NOT EXISTS conversation_input_queue (
  conversation_id TEXT NOT NULL,
  conversation_epoch INTEGER NOT NULL,
  input_seq INTEGER NOT NULL,
  input_revision INTEGER NOT NULL,
  kind TEXT NOT NULL,
  status TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY(conversation_id, conversation_epoch, input_seq)
);
CREATE INDEX IF NOT EXISTS idx_conversation_input_queue_status
  ON conversation_input_queue(conversation_id, status, input_seq);
CREATE INDEX IF NOT EXISTS idx_conversation_input_queue_kind
  ON conversation_input_queue(conversation_id, kind, input_seq);
`)
	return err
}

func parseInputQueueSnapshotHeader(raw []byte) (inputQueueSnapshotHeader, error) {
	var header inputQueueSnapshotHeader
	if len(raw) == 0 {
		return header, nil
	}
	if err := json.Unmarshal(raw, &header); err != nil {
		return header, fmt.Errorf("decode input queue snapshot header: %w", err)
	}
	if header.QueueStorageVersion == 1 && header.QueueControlRevision == 0 {
		// Existing row-backed snapshots predate the durable stop/resume control.
		header.QueueControlRevision = 1
	}
	return header, nil
}

func inputQueueRowsProjected(raw []byte, rows []InputQueueRow) bool {
	if rows != nil {
		return true
	}
	header, err := parseInputQueueSnapshotHeader(raw)
	return err == nil && header.QueueStorageVersion == 1
}

func validateInputQueueRows(rows []InputQueueRow) (map[[2]uint64]InputQueueRow, error) {
	byKey := make(map[[2]uint64]InputQueueRow, len(rows))
	claimed := 0
	for _, row := range rows {
		if row.ConversationEpoch == 0 || row.InputSeq == 0 || row.InputRevision == 0 || strings.TrimSpace(row.Kind) == "" {
			return nil, fmt.Errorf("input queue row identity is incomplete")
		}
		if row.Status != "queued" && row.Status != "claimed" {
			return nil, fmt.Errorf("invalid input queue status %q", row.Status)
		}
		if row.Status == "claimed" {
			claimed++
			if claimed > 1 {
				return nil, fmt.Errorf("input queue has more than one claimed row")
			}
		}
		if len(row.PayloadJSON) == 0 || !json.Valid(row.PayloadJSON) {
			return nil, fmt.Errorf("input queue row payload is invalid JSON")
		}
		var payloadIdentity struct {
			Seq      uint64 `json:"seq"`
			Revision uint64 `json:"revision"`
			Kind     string `json:"kind"`
		}
		if err := json.Unmarshal(row.PayloadJSON, &payloadIdentity); err != nil {
			return nil, fmt.Errorf("decode input queue row identity: %w", err)
		}
		if payloadIdentity.Seq != row.InputSeq || payloadIdentity.Revision != row.InputRevision || payloadIdentity.Kind != row.Kind {
			return nil, fmt.Errorf("input queue row payload identity mismatch for seq %d", row.InputSeq)
		}
		key := [2]uint64{row.ConversationEpoch, row.InputSeq}
		if _, exists := byKey[key]; exists {
			return nil, fmt.Errorf("duplicate input queue row epoch=%d seq=%d", row.ConversationEpoch, row.InputSeq)
		}
		byKey[key] = row
	}
	return byKey, nil
}

// syncInputQueueRowsTx reconciles the indexed pending-input table. It is only
// called inside the same SQLite transaction that updates runtime metadata.
func syncInputQueueRowsTx(ctx context.Context, tx *sql.Tx, conversationID string, rows []InputQueueRow) error {
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return fmt.Errorf("conversation_id is required")
	}
	wanted, err := validateInputQueueRows(rows)
	if err != nil {
		return err
	}
	type storedRow struct {
		revision uint64
		kind     string
		status   string
		payload  []byte
	}
	stored := make(map[[2]uint64]storedRow)
	result, err := tx.QueryContext(ctx, `SELECT conversation_epoch,input_seq,input_revision,kind,status,payload_json FROM conversation_input_queue WHERE conversation_id=?`, id)
	if err != nil {
		return err
	}
	for result.Next() {
		var key [2]uint64
		var row storedRow
		if err := result.Scan(&key[0], &key[1], &row.revision, &row.kind, &row.status, &row.payload); err != nil {
			result.Close()
			return err
		}
		stored[key] = row
	}
	if err := result.Err(); err != nil {
		result.Close()
		return err
	}
	if err := result.Close(); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for key, previous := range stored {
		if _, ok := wanted[key]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_inputs SET status='cancelled' WHERE conversation_id=? AND conversation_epoch=? AND input_seq=? AND status IN ('queued','claimed')`, id, key[0], key[1]); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_input_queue WHERE conversation_id=? AND conversation_epoch=? AND input_seq=?`, id, key[0], key[1]); err != nil {
			return err
		}
		_ = previous
	}
	for key, row := range wanted {
		if previous, ok := stored[key]; ok {
			if row.InputRevision < previous.revision {
				return fmt.Errorf("stale input queue revision for seq %d: stored %d, got %d", row.InputSeq, previous.revision, row.InputRevision)
			}
			if row.InputRevision == previous.revision && (row.Kind != previous.kind || !bytes.Equal(row.PayloadJSON, previous.payload)) {
				return fmt.Errorf("input queue row changed without revision advance for seq %d", row.InputSeq)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_input_queue(conversation_id,conversation_epoch,input_seq,input_revision,kind,status,payload_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(conversation_id,conversation_epoch,input_seq) DO UPDATE SET input_revision=excluded.input_revision,kind=excluded.kind,status=excluded.status,payload_json=excluded.payload_json,updated_at=excluded.updated_at`, id, row.ConversationEpoch, row.InputSeq, row.InputRevision, row.Kind, row.Status, string(row.PayloadJSON), now, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_inputs SET input_revision=?,status=? WHERE conversation_id=? AND conversation_epoch=? AND input_seq=?`, row.InputRevision, row.Status, id, row.ConversationEpoch, row.InputSeq); err != nil {
			return err
		}
	}
	return nil
}

// persistInputQueueStateTx validates the mailbox generation and syncs its
// rows before the caller replaces runtime_state_json in the same transaction.
func persistInputQueueStateTx(ctx context.Context, tx *sql.Tx, conversationID string, runtimeState RuntimeState, rows []InputQueueRow) error {
	header, err := parseInputQueueSnapshotHeader(runtimeState.InputBoxState)
	if err != nil {
		return err
	}
	if header.QueueStorageVersion != 1 || header.ConversationEpoch == 0 {
		return fmt.Errorf("input queue snapshot must use storage version 1 and a nonzero epoch")
	}
	if header.Sequence == 0 && len(rows) > 0 {
		return fmt.Errorf("input queue rows exceed the snapshot sequence watermark")
	}
	for _, row := range rows {
		if row.ConversationEpoch != header.ConversationEpoch || row.InputSeq > header.Sequence {
			return fmt.Errorf("input queue row is outside its snapshot epoch/watermark")
		}
	}
	var previousRaw string
	err = tx.QueryRowContext(ctx, `SELECT runtime_state_json FROM agent_runtimes WHERE agent_id=?`, strings.TrimSpace(conversationID)).Scan(&previousRaw)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		var previousState RuntimeState
		if err := json.Unmarshal([]byte(previousRaw), &previousState); err != nil {
			return fmt.Errorf("decode persisted runtime state for input queue: %w", err)
		}
		previous, err := parseInputQueueSnapshotHeader(previousState.InputBoxState)
		if err != nil {
			return err
		}
		if previous.QueueStorageVersion == 1 {
			if header.ConversationEpoch < previous.ConversationEpoch || header.Sequence < previous.Sequence || header.QueueRevision < previous.QueueRevision || header.QueueControlRevision < previous.QueueControlRevision {
				return fmt.Errorf("stale input queue snapshot")
			}
			if header.QueueControlRevision == previous.QueueControlRevision && header.QueuePaused != previous.QueuePaused {
				return fmt.Errorf("input queue pause state changed without control revision advance")
			}
			if rows == nil {
				if header.ConversationEpoch != previous.ConversationEpoch || header.QueueRevision != previous.QueueRevision {
					return fmt.Errorf("input queue rows are required when the queue revision changes")
				}
				return nil
			}
			if header.QueueRevision == previous.QueueRevision {
				if err := inputQueueRowsMatchTx(ctx, tx, conversationID, rows); err != nil {
					return fmt.Errorf("input queue changed without queue revision advance: %w", err)
				}
			}
		}
		if rows == nil {
			if header.Sequence == 0 {
				return nil
			}
			return fmt.Errorf("input queue rows are required to initialize nonempty row-backed storage")
		}
	} else if rows == nil {
		if header.Sequence == 0 {
			return nil
		}
		return fmt.Errorf("input queue rows are required to initialize a nonempty sequence")
	}
	return syncInputQueueRowsTx(ctx, tx, conversationID, rows)
}

func inputQueueRowsMatchTx(ctx context.Context, tx *sql.Tx, conversationID string, rows []InputQueueRow) error {
	wanted, err := validateInputQueueRows(rows)
	if err != nil {
		return err
	}
	rowsCursor, err := tx.QueryContext(ctx, `SELECT conversation_epoch,input_seq,input_revision,kind,status,payload_json FROM conversation_input_queue WHERE conversation_id=?`, strings.TrimSpace(conversationID))
	if err != nil {
		return err
	}
	defer rowsCursor.Close()
	count := 0
	for rowsCursor.Next() {
		var key [2]uint64
		var revision uint64
		var kind, status string
		var payload []byte
		if err := rowsCursor.Scan(&key[0], &key[1], &revision, &kind, &status, &payload); err != nil {
			return err
		}
		want, ok := wanted[key]
		if !ok || want.InputRevision != revision || want.Kind != kind || want.Status != status || !bytes.Equal(want.PayloadJSON, payload) {
			return fmt.Errorf("persisted queue row differs at seq %d", key[1])
		}
		count++
	}
	if err := rowsCursor.Err(); err != nil {
		return err
	}
	if count != len(wanted) {
		return fmt.Errorf("persisted row count %d differs from snapshot row count %d", count, len(wanted))
	}
	return nil
}

// MigrateLegacyInputState moves the pre-v1 mailbox payload and inline retry
// receipts into their indexed stores while replacing the runtime snapshot with
// the bounded v1 queue header. Both data moves commit atomically.
func (s *SQLiteStore) MigrateLegacyInputState(ctx context.Context, conversationID string, inputState json.RawMessage, queueRows []InputQueueRow, receipts []InputReceipt, completedSeq, completedRevision uint64) error {
	id := strings.TrimSpace(conversationID)
	if id == "" {
		return fmt.Errorf("conversation_id is required")
	}
	header, err := parseInputQueueSnapshotHeader(inputState)
	if err != nil || header.QueueStorageVersion != 1 {
		return fmt.Errorf("invalid migrated input queue snapshot")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var runtimeRaw string
	if err := tx.QueryRowContext(ctx, `SELECT runtime_state_json FROM agent_runtimes WHERE agent_id=?`, id).Scan(&runtimeRaw); err != nil {
		return err
	}
	var state RuntimeState
	if err := json.Unmarshal([]byte(runtimeRaw), &state); err != nil {
		return fmt.Errorf("decode runtime state for input migration: %w", err)
	}
	previous, err := parseInputQueueSnapshotHeader(state.InputBoxState)
	if err != nil {
		return err
	}
	if previous.QueueStorageVersion == 1 {
		return tx.Commit()
	}
	for _, receipt := range receipts {
		if err := saveLegacyInputReceiptTx(ctx, tx, id, receipt); err != nil {
			return err
		}
	}
	if err := syncInputQueueRowsTx(ctx, tx, id, queueRows); err != nil {
		return err
	}
	if completedSeq > 0 {
		if completedRevision == 0 {
			return fmt.Errorf("completed legacy input revision is required")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_inputs SET status='consumed',input_revision=? WHERE conversation_id=? AND input_seq=?`, completedRevision, id, completedSeq); err != nil {
			return err
		}
	}
	state.InputBoxState = append(json.RawMessage(nil), inputState...)
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE agent_runtimes SET runtime_state_json=?,updated_at=? WHERE agent_id=?`, string(encoded), time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteStore) LoadInputQueue(ctx context.Context, conversationID string) ([]InputQueueRow, error) {
	return s.loadInputQueue(ctx, conversationID, "", false)
}

// LoadInputQueueByKind returns only queued or claimed inputs of one kind.
// Hydrate uses this to avoid loading and decoding internal trigger/child-agent
// payloads while projecting the user-visible pending-input list.
func (s *SQLiteStore) LoadInputQueueByKind(ctx context.Context, conversationID, kind string) ([]InputQueueRow, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, fmt.Errorf("input queue kind is required")
	}
	return s.loadInputQueue(ctx, conversationID, kind, true)
}

func (s *SQLiteStore) loadInputQueue(ctx context.Context, conversationID, kind string, filterKind bool) ([]InputQueueRow, error) {
	query := `SELECT conversation_epoch,input_seq,input_revision,kind,status,payload_json FROM conversation_input_queue WHERE conversation_id=?`
	args := []any{strings.TrimSpace(conversationID)}
	if filterKind {
		query += ` AND kind=?`
		args = append(args, kind)
	}
	query += ` ORDER BY input_seq`
	cursor, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer cursor.Close()
	out := make([]InputQueueRow, 0)
	for cursor.Next() {
		var row InputQueueRow
		var payload string
		if err := cursor.Scan(&row.ConversationEpoch, &row.InputSeq, &row.InputRevision, &row.Kind, &row.Status, &payload); err != nil {
			return nil, err
		}
		row.PayloadJSON = json.RawMessage(payload)
		out = append(out, row)
	}
	return out, cursor.Err()
}

func (s *SQLiteStore) CountQueuedInputs(ctx context.Context, conversationID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_input_queue WHERE conversation_id=? AND status='queued'`, strings.TrimSpace(conversationID)).Scan(&count)
	return count, err
}

// saveInputReceiptTx persists an accepted message's receipt in the same
// transaction as its runtime mailbox snapshot.
func saveInputReceiptTx(ctx context.Context, tx *sql.Tx, conversationID string, receipt InputReceipt) error {
	id := strings.TrimSpace(conversationID)
	requestID := strings.TrimSpace(receipt.ClientRequestID)
	if id == "" || requestID == "" || receipt.ConversationEpoch == 0 || receipt.InputSeq == 0 || receipt.InputRevision == 0 || strings.TrimSpace(receipt.PayloadDigest) == "" {
		return fmt.Errorf("invalid input receipt")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO conversation_inputs(conversation_id,client_request_id,payload_digest,conversation_epoch,input_seq,input_revision,status,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, requestID, receipt.PayloadDigest, receipt.ConversationEpoch, receipt.InputSeq, receipt.InputRevision, "queued", now)
	if err == nil {
		return nil
	}
	var existing InputReceipt
	lookupErr := tx.QueryRowContext(ctx, `SELECT client_request_id,payload_digest,conversation_epoch,input_seq,input_revision,status FROM conversation_inputs WHERE conversation_id=? AND client_request_id=?`, id, requestID).Scan(&existing.ClientRequestID, &existing.PayloadDigest, &existing.ConversationEpoch, &existing.InputSeq, &existing.InputRevision, &existing.Status)
	if lookupErr == nil && existing.PayloadDigest == receipt.PayloadDigest && existing.ConversationEpoch == receipt.ConversationEpoch && existing.InputSeq == receipt.InputSeq && existing.InputRevision == receipt.InputRevision {
		return fmt.Errorf("input request id conflict: receipt was already committed")
	}
	if lookupErr != nil && lookupErr != sql.ErrNoRows {
		return lookupErr
	}
	return fmt.Errorf("persist input receipt: %w", err)
}

func saveLegacyInputReceiptTx(ctx context.Context, tx *sql.Tx, conversationID string, receipt InputReceipt) error {
	id := strings.TrimSpace(conversationID)
	requestID := strings.TrimSpace(receipt.ClientRequestID)
	if id == "" || requestID == "" || receipt.ConversationEpoch == 0 || receipt.InputSeq == 0 || receipt.InputRevision == 0 || receipt.PayloadDigest == "" {
		return fmt.Errorf("invalid legacy input receipt")
	}
	var existing InputReceipt
	err := tx.QueryRowContext(ctx, `SELECT client_request_id,payload_digest,conversation_epoch,input_seq,input_revision,status FROM conversation_inputs WHERE conversation_id=? AND client_request_id=?`, id, requestID).Scan(&existing.ClientRequestID, &existing.PayloadDigest, &existing.ConversationEpoch, &existing.InputSeq, &existing.InputRevision, &existing.Status)
	if err == nil {
		if existing.PayloadDigest != receipt.PayloadDigest || existing.ConversationEpoch != receipt.ConversationEpoch || existing.InputSeq != receipt.InputSeq {
			return fmt.Errorf("legacy input receipt conflicts with stored receipt")
		}
		_, err = tx.ExecContext(ctx, `UPDATE conversation_inputs SET input_revision=?,status=? WHERE conversation_id=? AND client_request_id=?`, receipt.InputRevision, receipt.Status, id, requestID)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO conversation_inputs(conversation_id,client_request_id,payload_digest,conversation_epoch,input_seq,input_revision,status,created_at) VALUES(?,?,?,?,?,?,?,?)`, id, requestID, receipt.PayloadDigest, receipt.ConversationEpoch, receipt.InputSeq, receipt.InputRevision, receipt.Status, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *SQLiteStore) LookupInputReceipt(ctx context.Context, conversationID, requestID string) (InputReceipt, bool, error) {
	var receipt InputReceipt
	err := s.db.QueryRowContext(ctx, `SELECT client_request_id,payload_digest,conversation_epoch,input_seq,input_revision,status FROM conversation_inputs WHERE conversation_id=? AND client_request_id=?`, strings.TrimSpace(conversationID), strings.TrimSpace(requestID)).Scan(&receipt.ClientRequestID, &receipt.PayloadDigest, &receipt.ConversationEpoch, &receipt.InputSeq, &receipt.InputRevision, &receipt.Status)
	if err == sql.ErrNoRows {
		return InputReceipt{}, false, nil
	}
	if err != nil {
		return InputReceipt{}, false, err
	}
	return receipt, true, nil
}

func (s *SQLiteStore) LookupInputReceiptWithRuntimeState(ctx context.Context, conversationID, requestID string) (InputReceipt, bool, RuntimeState, error) {
	id := strings.TrimSpace(conversationID)
	var runtimeRaw string
	if err := s.db.QueryRowContext(ctx, `SELECT runtime_state_json FROM agent_runtimes WHERE agent_id=?`, id).Scan(&runtimeRaw); err != nil {
		return InputReceipt{}, false, RuntimeState{}, err
	}
	var state RuntimeState
	if err := json.Unmarshal([]byte(runtimeRaw), &state); err != nil {
		return InputReceipt{}, false, RuntimeState{}, fmt.Errorf("decode runtime state: %w", err)
	}
	receipt, found, err := s.LookupInputReceipt(ctx, id, requestID)
	return receipt, found, state, err
}
