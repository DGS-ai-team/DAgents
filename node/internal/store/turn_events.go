package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// CompletedTurnSnapshot is the durable, bounded reader input for maintenance.
type CompletedTurnSnapshot struct {
	EventID   int64
	AgentID   string
	SessionID string
	TurnID    string
	Messages  []llm.Message
	Status    string
}

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
CREATE TABLE IF NOT EXISTS completed_turn_snapshots (
  event_id INTEGER PRIMARY KEY,
  agent_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  turn_id TEXT NOT NULL,
  messages_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'readable',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_completed_turn_snapshots_agent_event
  ON completed_turn_snapshots(agent_id, event_id);
`)
	if err != nil {
		return err
	}
	return nil
}

// AppendTurnEventWithSnapshot atomically appends a completed event and its
// message boundary. It is used at the completion durability fence.
func (s *SQLiteStore) AppendTurnEventWithSnapshot(ctx context.Context, event turn.TurnEventEnvelope, messages []llm.Message) (turn.TurnEventEnvelope, error) {
	if event.EventType != turn.EventTurnCompleted {
		return s.AppendTurnEvent(ctx, event)
	}
	if err := validateTurnEvent(s, &event); err != nil {
		return event, err
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		return event, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return event, err
	}
	defer func() { _ = tx.Rollback() }()
	stored, inserted, err := s.appendTurnEventTx(ctx, tx, event)
	if err != nil {
		return event, err
	}
	if !inserted {
		return stored, tx.Commit()
	}
	status := "readable"
	if len(messages) == 0 {
		status = "unreadable"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO completed_turn_snapshots(event_id,agent_id,session_id,turn_id,messages_json,status,created_at) VALUES(?,?,?,?,?,?,?)`, stored.ID, stored.AgentID, stored.SessionID, stored.TurnID, string(raw), status, stored.CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return stored, err
	}
	return stored, tx.Commit()
}

func (s *SQLiteStore) ListCompletedTurnSnapshots(ctx context.Context, agentID string, afterEventID int64, limit int) ([]CompletedTurnSnapshot, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,agent_id,session_id,turn_id,messages_json,status FROM completed_turn_snapshots WHERE agent_id=? AND event_id>? ORDER BY event_id LIMIT ?`, strings.TrimSpace(agentID), afterEventID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CompletedTurnSnapshot
	for rows.Next() {
		var item CompletedTurnSnapshot
		var raw string
		if err := rows.Scan(&item.EventID, &item.AgentID, &item.SessionID, &item.TurnID, &raw, &item.Status); err != nil {
			return nil, err
		}
		if item.Status != "readable" {
			out = append(out, item)
			continue
		}
		if err := json.Unmarshal([]byte(raw), &item.Messages); err != nil {
			item.Status = "unreadable"
			out = append(out, item)
			continue
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// RebuildTurnMessages reconstructs the durable portion of a Turn from its
// lifecycle facts. It deliberately does not use runtime slice offsets.
func (s *SQLiteStore) RebuildTurnMessages(ctx context.Context, sessionID, turnID string) ([]llm.Message, error) {
	events, err := s.ListTurnEventsForTurn(ctx, sessionID, turnID)
	if err != nil {
		return nil, err
	}
	var out []llm.Message
	hasInput, hasAssistant := false, false
	for _, event := range events {
		var p struct {
			InputMessage     *llm.Message `json:"input_message"`
			AssistantMessage *llm.Message `json:"assistant_message"`
			ResultContent    string       `json:"result_content"`
			ToolName         string       `json:"tool_name"`
			ToolCallID       string       `json:"tool_call_id"`
			ExecutionStatus  string       `json:"execution_status"`
		}
		if err := json.Unmarshal(event.Payload, &p); err != nil {
			return nil, fmt.Errorf("turn %s event %d payload: %w", turnID, event.ID, err)
		}
		switch event.EventType {
		case turn.EventTurnStarted:
			if p.InputMessage != nil {
				out = append(out, *p.InputMessage)
				hasInput = true
			}
		case turn.EventAssistantMessageRecorded:
			if p.AssistantMessage != nil {
				out = append(out, *p.AssistantMessage)
				hasAssistant = true
			} else if p.ResultContent != "" {
				out = append(out, llm.Message{Role: "assistant", Content: p.ResultContent})
			}
		case turn.EventToolResultRecorded, turn.EventToolExecutionCompleted:
			if p.ResultContent != "" {
				callID := event.ToolCallID
				if callID == "" {
					callID = p.ToolCallID
				}
				duplicate := false
				for _, existing := range out {
					if existing.Role == "tool" && existing.ToolCallID == callID && existing.Content == p.ResultContent {
						duplicate = true
						break
					}
				}
				if !duplicate {
					out = append(out, llm.Message{Role: "tool", Content: p.ResultContent, ToolCallID: callID, Name: p.ToolName})
				}
			}
		}
	}
	if !hasInput || !hasAssistant {
		return nil, fmt.Errorf("turn %s durable transcript is incomplete", turnID)
	}
	return out, nil
}

func (s *SQLiteStore) ListTurnEventsForTurn(ctx context.Context, sessionID, turnID string) ([]turn.TurnEventEnvelope, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,agent_id,session_id,turn_id,step_id,tool_batch_id,tool_call_id,tool_execution_id,interaction_id,session_seq,turn_seq,event_type,event_version,source,command_id,payload_json,payload_ref,created_at FROM turn_events WHERE session_id=? AND turn_id=? ORDER BY event_id`, sessionID, turnID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []turn.TurnEventEnvelope
	for rows.Next() {
		var e turn.TurnEventEnvelope
		var payload, created string
		if err := rows.Scan(&e.ID, &e.AgentID, &e.SessionID, &e.TurnID, &e.StepID, &e.ToolBatchID, &e.ToolCallID, &e.ToolExecutionID, &e.InteractionID, &e.SessionSeq, &e.TurnSeq, &e.EventType, &e.EventVersion, &e.Source, &e.CommandID, &payload, &e.PayloadRef, &created); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListTurnEventsForTurnBounded reads a complete, bounded lifecycle transcript.
// It returns no partial result when the event or payload budget is exceeded.
func (s *SQLiteStore) ListTurnEventsForTurnBounded(ctx context.Context, sessionID, turnID string, maxEvents, maxBytes int) ([]turn.TurnEventEnvelope, error) {
	if s == nil || s.db == nil || maxEvents <= 0 || maxEvents > 100000 || maxBytes <= 0 || maxBytes > 64*1024*1024 {
		return nil, fmt.Errorf("invalid bounded turn event limits")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,agent_id,session_id,turn_id,step_id,tool_batch_id,tool_call_id,tool_execution_id,interaction_id,session_seq,turn_seq,event_type,event_version,source,command_id,CASE WHEN length(CAST(payload_json AS BLOB))<=? THEN payload_json ELSE '' END,length(CAST(payload_json AS BLOB)),payload_ref,created_at FROM turn_events WHERE session_id=? AND turn_id=? ORDER BY event_id LIMIT ?`, maxBytes, sessionID, turnID, maxEvents+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]turn.TurnEventEnvelope, 0, minIntStore(maxEvents, 16))
	total := 0
	for rows.Next() {
		if len(out) >= maxEvents {
			return nil, fmt.Errorf("turn event count exceeds limit")
		}
		var e turn.TurnEventEnvelope
		var payload, created string
		var payloadLen int
		if err := rows.Scan(&e.ID, &e.AgentID, &e.SessionID, &e.TurnID, &e.StepID, &e.ToolBatchID, &e.ToolCallID, &e.ToolExecutionID, &e.InteractionID, &e.SessionSeq, &e.TurnSeq, &e.EventType, &e.EventVersion, &e.Source, &e.CommandID, &payload, &payloadLen, &e.PayloadRef, &created); err != nil {
			return nil, err
		}
		if payloadLen < 0 || payloadLen > maxBytes || total > maxBytes-payloadLen {
			return nil, fmt.Errorf("turn event payload bytes exceed limit")
		}
		total += payloadLen
		e.Payload = json.RawMessage(payload)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func minIntStore(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// AppendTurnEvent appends one lifecycle fact and allocates the two monotonic
// sequence numbers in the same SQLite transaction. Reusing command_id is
// idempotent and returns the originally stored event.
func (s *SQLiteStore) AppendTurnEvent(ctx context.Context, event turn.TurnEventEnvelope) (turn.TurnEventEnvelope, error) {
	if err := validateTurnEvent(s, &event); err != nil {
		return event, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return event, err
	}
	defer func() { _ = tx.Rollback() }()
	stored, _, err := s.appendTurnEventTx(ctx, tx, event)
	if err != nil {
		return event, err
	}
	if err := tx.Commit(); err != nil {
		return event, err
	}
	return stored, nil
}

func validateTurnEvent(s *SQLiteStore, event *turn.TurnEventEnvelope) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is nil")
	}
	if strings.TrimSpace(event.SessionID) == "" {
		return fmt.Errorf("event session id is required")
	}
	if strings.TrimSpace(event.TurnID) == "" {
		return fmt.Errorf("event turn id is required")
	}
	if !event.EventType.Valid() {
		return fmt.Errorf("unknown event type %q", event.EventType)
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
		return fmt.Errorf("event payload is invalid JSON")
	}
	return nil
}

// appendTurnEventTx appends or replays an event using the caller's transaction.
// The bool reports whether a new row was inserted.
func (s *SQLiteStore) appendTurnEventTx(ctx context.Context, tx *sql.Tx, event turn.TurnEventEnvelope) (turn.TurnEventEnvelope, bool, error) {
	if strings.TrimSpace(event.CommandID) != "" {
		var existing turn.TurnEventEnvelope
		var payload, created string
		err := tx.QueryRowContext(ctx, `
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
				return event, false, fmt.Errorf("command id %q is already used by a different lifecycle event", event.CommandID)
			}
			return existing, false, nil
		}
		if err != sql.ErrNoRows {
			return event, false, err
		}
	}

	var maxSession, maxTurn uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(session_seq), 0) FROM turn_events WHERE session_id = ?`, event.SessionID).Scan(&maxSession); err != nil {
		return event, false, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(turn_seq), 0) FROM turn_events WHERE session_id = ? AND turn_id = ?`, event.SessionID, event.TurnID).Scan(&maxTurn); err != nil {
		return event, false, err
	}
	if event.SessionSeq == 0 {
		event.SessionSeq = maxSession + 1
	}
	if event.TurnSeq == 0 {
		event.TurnSeq = maxTurn + 1
	}
	if event.SessionSeq != maxSession+1 {
		return event, false, fmt.Errorf("session event sequence is not contiguous: got=%d want=%d", event.SessionSeq, maxSession+1)
	}
	if event.TurnSeq != maxTurn+1 {
		return event, false, fmt.Errorf("turn event sequence is not contiguous: got=%d want=%d", event.TurnSeq, maxTurn+1)
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
		return event, false, err
	}
	event.ID, err = result.LastInsertId()
	if err != nil {
		return event, false, err
	}
	return event, true, nil
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
