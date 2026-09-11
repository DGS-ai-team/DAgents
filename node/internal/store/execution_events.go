package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ExecutionEventRecord is the durable, low-frequency portion of a process
// lifecycle. Output chunks intentionally do not use this record; they remain
// runtime events delivered through SSE.
type ExecutionEventRecord struct {
	ID             int64
	AgentID        string
	SessionID      string
	ProcessID      string
	ProcessSeq     uint64
	EventType      string
	Stream         string
	TurnID         string
	ToolCallID     string
	TargetKind     string
	TargetID       string
	PolicyDecision string
	ApprovalID     string
	RiskLevel      string
	CommandDigest  string
	OutputBytes    int64
	ExitCode       *int
	ExitError      string
	CreatedAt      time.Time
}

func (s *SQLiteStore) initExecutionEventSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS execution_events (
  event_id INTEGER PRIMARY KEY AUTOINCREMENT,
  agent_id TEXT NOT NULL DEFAULT '',
  session_id TEXT NOT NULL,
  process_id TEXT NOT NULL,
  process_seq INTEGER NOT NULL,
  event_type TEXT NOT NULL,
  stream TEXT NOT NULL DEFAULT '',
  turn_id TEXT NOT NULL DEFAULT '',
  tool_call_id TEXT NOT NULL DEFAULT '',
  target_kind TEXT NOT NULL DEFAULT '',
  target_id TEXT NOT NULL DEFAULT '',
  policy_decision TEXT NOT NULL DEFAULT '',
  approval_id TEXT NOT NULL DEFAULT '',
  risk_level TEXT NOT NULL DEFAULT '',
  command_digest TEXT NOT NULL DEFAULT '',
  output_bytes INTEGER NOT NULL DEFAULT 0,
  exit_code INTEGER,
  exit_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_execution_events_session
  ON execution_events(session_id, event_id);
CREATE INDEX IF NOT EXISTS idx_execution_events_process
  ON execution_events(process_id, process_seq);
`)
	if err != nil {
		return err
	}
	return nil
}

// AppendExecutionEvent persists one lifecycle event. Callers should omit
// high-frequency process_output events and keep this method off the process IO
// path when possible.
func (s *SQLiteStore) AppendExecutionEvent(ctx context.Context, event ExecutionEventRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("store is nil")
	}
	if strings.TrimSpace(event.SessionID) == "" {
		return fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(event.ProcessID) == "" {
		return fmt.Errorf("process_id is required")
	}
	if event.ProcessSeq == 0 {
		return fmt.Errorf("process_seq must be positive")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return fmt.Errorf("event_type is required")
	}
	created := event.CreatedAt
	if created.IsZero() {
		created = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO execution_events(
  agent_id, session_id, process_id, process_seq, event_type, stream,
  turn_id, tool_call_id, target_kind, target_id, policy_decision,
  approval_id, risk_level, command_digest, output_bytes, exit_code,
  exit_error, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.AgentID, event.SessionID, event.ProcessID, event.ProcessSeq,
		event.EventType, event.Stream, event.TurnID, event.ToolCallID,
		event.TargetKind, event.TargetID, event.PolicyDecision, event.ApprovalID,
		event.RiskLevel, event.CommandDigest, event.OutputBytes, event.ExitCode,
		event.ExitError, created.Format(time.RFC3339Nano))
	return err
}

// ListExecutionEvents returns lifecycle events in process order for audit and
// recovery diagnostics. A bounded limit prevents an audit read from becoming
// an unbounded history query.
func (s *SQLiteStore) ListExecutionEvents(ctx context.Context, sessionID string, limit int) ([]ExecutionEventRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("store is nil")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT event_id, agent_id, session_id, process_id, process_seq, event_type,
       stream, turn_id, tool_call_id, target_kind, target_id, policy_decision,
       approval_id, risk_level, command_digest, output_bytes, exit_code,
       exit_error, created_at
FROM execution_events
WHERE session_id = ?
ORDER BY event_id ASC
LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []ExecutionEventRecord
	for rows.Next() {
		var event ExecutionEventRecord
		var exitCode sql.NullInt64
		var created string
		if err := rows.Scan(
			&event.ID, &event.AgentID, &event.SessionID, &event.ProcessID,
			&event.ProcessSeq, &event.EventType, &event.Stream, &event.TurnID,
			&event.ToolCallID, &event.TargetKind, &event.TargetID,
			&event.PolicyDecision, &event.ApprovalID, &event.RiskLevel,
			&event.CommandDigest, &event.OutputBytes, &exitCode,
			&event.ExitError, &created,
		); err != nil {
			return nil, err
		}
		if exitCode.Valid {
			code := int(exitCode.Int64)
			event.ExitCode = &code
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		events = append(events, event)
	}
	return events, rows.Err()
}
