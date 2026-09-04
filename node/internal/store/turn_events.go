package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// initTurnEventSchema stores low-frequency lifecycle facts. Streaming deltas
// and large tool output remain outside this table and use PayloadRef when a
// durable reference is needed.
func (s *SQLiteStore) initTurnEventSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS turn_events (
  event_id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  step_id TEXT NOT NULL DEFAULT '',
  tool_batch_id TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  tool_execution_id TEXT NOT NULL DEFAULT '',
  interaction_id TEXT NOT NULL DEFAULT '',
  session_seq INTEGER NOT NULL,
  turn_seq INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  event_version INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL DEFAULT '',
  command_id TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  payload_ref TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_turn_events_command
  ON turn_events(session_id, command_id) WHERE command_id != '';
CREATE INDEX IF NOT EXISTS idx_turn_events_session_seq
  ON turn_events(session_id, session_seq);
CREATE INDEX IF NOT EXISTS idx_turn_events_turn_seq
  ON turn_events(turn_id, turn_seq);
`)
	if err != nil {
		return err
	}
	return nil
}

// AppendTurnEvent appends one lifecycle fact and allocates the two monotonic
// sequence numbers in the same SQLite transaction. Reusing command_id is
// idempotent and returns the originally stored event.
func (s *SQLiteStore) AppendTurnEvent(ctx context.Context, event turn.TurnEventEnvelope) (turn.TurnEventEnvelope, error) {
	if s == nil || s.db == nil {
		return event, fmt.Errorf("store is nil")
	}
	if strings.TrimSpace(event.SessionID) == "" {
		return event, fmt.Errorf("event session id is required")
	}
	if strings.TrimSpace(event.TurnID) == "" {
		return event, fmt.Errorf("event turn id is required")
	}
	if !event.EventType.Valid() {
		return event, fmt.Errorf("unknown event type %q", event.EventType)
	}
	if event.EventVersion == 0 {
		event.EventVersion = 1
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(event.Payload) {
		return event, fmt.Errorf("event payload is invalid JSON")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return event, err
	}
	defer func() { _ = tx.Rollback() }()

	if strings.TrimSpace(event.CommandID) != "" {
		var existing turn.TurnEventEnvelope
		var payload, created string
		err = tx.QueryRowContext(ctx, `
SELECT event_id, agent_id, session_id, turn_id, step_id, tool_batch_id, tool_call_id,
       tool_execution_id, interaction_id, session_seq, turn_seq, event_type, event_version,
       source, command_id, payload_json, payload_ref, created_at
FROM turn_events WHERE session_id = ? AND command_id = ?`, event.SessionID, event.CommandID).
			Scan(&existing.ID, &existing.AgentID, &existing.SessionID, &existing.TurnID, &existing.StepID,
				&existing.ToolBatchID, &existing.ToolCallID, &existing.ToolExecutionID, &existing.InteractionID, &existing.SessionSeq,
				&existing.TurnSeq, &existing.EventType, &existing.EventVersion,
				&existing.Source, &existing.CommandID, &payload, &existing.PayloadRef, &created)
		if err == nil {
			existing.Payload = json.RawMessage(payload)
			existing.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
			if existing.AgentID != event.AgentID || existing.TurnID != event.TurnID ||
				existing.StepID != event.StepID || existing.EventType != event.EventType ||
				existing.ToolBatchID != event.ToolBatchID || existing.ToolCallID != event.ToolCallID ||
				existing.ToolExecutionID != event.ToolExecutionID || existing.InteractionID != event.InteractionID ||
				existing.PayloadRef != event.PayloadRef || string(existing.Payload) != string(event.Payload) {
				return event, fmt.Errorf("command id %q is already used by a different lifecycle event", event.CommandID)
			}
			return existing, tx.Commit()
		}
		if err != sql.ErrNoRows {
			return event, err
		}
	}

	var maxSession, maxTurn uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(session_seq), 0) FROM turn_events WHERE session_id = ?`, event.SessionID).Scan(&maxSession); err != nil {
		return event, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_seq), 0) FROM turn_events WHERE session_id = ? AND turn_id = ?`, event.SessionID, event.TurnID).Scan(&maxTurn); err != nil {
		return event, err
	}
	if event.SessionSeq == 0 {
		event.SessionSeq = maxSession + 1
	}
	if event.TurnSeq == 0 {
		event.TurnSeq = maxTurn + 1
	}
	if event.SessionSeq != maxSession+1 {
		return event, fmt.Errorf("session event sequence is not contiguous: got=%d want=%d", event.SessionSeq, maxSession+1)
	}
	if event.TurnSeq != maxTurn+1 {
		return event, fmt.Errorf("turn event sequence is not contiguous: got=%d want=%d", event.TurnSeq, maxTurn+1)
	}

	result, err := tx.ExecContext(ctx, `
INSERT INTO turn_events(
  agent_id, session_id, turn_id, step_id, tool_batch_id, tool_call_id, tool_execution_id, interaction_id,
  session_seq, turn_seq, event_type, event_version, source, command_id,
  payload_json, payload_ref, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.AgentID, event.SessionID, event.TurnID, event.StepID, event.ToolBatchID,
		event.ToolCallID, event.ToolExecutionID, event.InteractionID, event.SessionSeq, event.TurnSeq,
		event.EventType, event.EventVersion, event.Source, event.CommandID,
		string(event.Payload), event.PayloadRef, event.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return event, err
	}
	event.ID, err = result.LastInsertId()
	if err != nil {
		return event, err
	}
	if err := tx.Commit(); err != nil {
		return event, err
	}
	return event, nil
}

// ListTurnEvents reads a bounded session timeline after session_seq.
func (s *SQLiteStore) ListTurnEvents(ctx context.Context, sessionID string, afterSeq uint64, limit int) ([]turn.TurnEventEnvelope, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is nil")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT event_id, agent_id, session_id, turn_id, step_id, tool_batch_id, tool_call_id,
       tool_execution_id, interaction_id, session_seq, turn_seq, event_type, event_version,
       source, command_id, payload_json, payload_ref, created_at
FROM turn_events
WHERE session_id = ? AND session_seq > ?
ORDER BY session_seq ASC LIMIT ?`, sessionID, afterSeq, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []turn.TurnEventEnvelope
	for rows.Next() {
		var event turn.TurnEventEnvelope
		var payload, created string
		if err := rows.Scan(&event.ID, &event.AgentID, &event.SessionID, &event.TurnID, &event.StepID,
			&event.ToolBatchID, &event.ToolCallID, &event.ToolExecutionID, &event.InteractionID, &event.SessionSeq, &event.TurnSeq,
			&event.EventType, &event.EventVersion, &event.Source, &event.CommandID,
			&payload, &event.PayloadRef, &created); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		events = append(events, event)
	}
	return events, rows.Err()
}
