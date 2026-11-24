package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

// CompletedTurnSnapshot is the durable, bounded reader input for maintenance.
type CompletedTurnSnapshot struct {
	EventID              int64
	AgentID              string
	SessionID            string
	TurnID               string
	Messages             []llm.Message
	Status               string
	ConversationRevision uint64
	CheckpointRevision   uint64
}

// TerminalBoundary is the single SQLite durability fence for a terminal Turn.
// Runtime metadata (including mailbox/Ack state), the explicit transcript and
// checkpoint delta, lifecycle facts, and the terminal snapshot commit together.
type TerminalBoundary struct {
	Record                 Record
	Delta                  *ConversationDelta
	RoundRevision          *RoundRevision
	Events                 []turn.TurnEventEnvelope
	SnapshotMessages       []llm.Message
	SnapshotStatus         string // readable, no_work, or unreadable
	CompletedInputSeq      uint64
	CompletedInputRevision uint64
}

type terminalBatchFact struct {
	CommandID string         `json:"command_id"`
	EventType turn.EventType `json:"event_type"`
}

type terminalBoundaryFingerprint struct {
	AgentID                string
	NodeID                 string
	FirstUserMessage       string
	LoadedSkills           any
	RuntimeState           RuntimeState
	InputQueueRows         []InputQueueRow
	Delta                  *ConversationDelta
	RoundRevision          *RoundRevision
	CompletedInputSeq      uint64
	CompletedInputRevision uint64
}

func terminalBoundaryDigest(rec Record, delta *ConversationDelta, roundRevision *RoundRevision, completedInputSeq, completedInputRevision, conversationRevision, checkpointRevision uint64) (string, error) {
	state := rec.RuntimeState
	state.ConversationRevision = conversationRevision
	state.ContextCheckpointRevision = checkpointRevision
	fingerprint := terminalBoundaryFingerprint{
		AgentID:                strings.TrimSpace(rec.AgentID),
		NodeID:                 rec.NodeID,
		FirstUserMessage:       strings.TrimSpace(rec.FirstUserMessage),
		LoadedSkills:           rec.LoadedSkills,
		RuntimeState:           state,
		InputQueueRows:         rec.InputQueueRows,
		Delta:                  delta,
		RoundRevision:          roundRevision,
		CompletedInputSeq:      completedInputSeq,
		CompletedInputRevision: completedInputRevision,
	}
	raw, err := json.Marshal(fingerprint)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
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
  conversation_revision INTEGER NOT NULL DEFAULT 0,
  checkpoint_revision INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_completed_turn_snapshots_agent_event
  ON completed_turn_snapshots(agent_id, event_id);
`)
	if err != nil {
		return err
	}
	for _, column := range []struct{ name, ddl string }{
		{"conversation_revision", `ALTER TABLE completed_turn_snapshots ADD COLUMN conversation_revision INTEGER NOT NULL DEFAULT 0`},
		{"checkpoint_revision", `ALTER TABLE completed_turn_snapshots ADD COLUMN checkpoint_revision INTEGER NOT NULL DEFAULT 0`},
	} {
		rows, err := s.db.Query(`PRAGMA table_info(completed_turn_snapshots)`)
		if err != nil {
			return err
		}
		found := false
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				_ = rows.Close()
				return err
			}
			if name == column.name {
				found = true
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if !found {
			if _, err := s.db.Exec(column.ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

// CommitTerminalBoundary atomically stores runtime state, optional conversation
// delta/checkpoint, terminal lifecycle facts and the revision-bound snapshot.
// The caller must pass the lifecycle events in their required order.
func (s *SQLiteStore) CommitTerminalBoundary(ctx context.Context, boundary TerminalBoundary) ([]turn.TurnEventEnvelope, ConversationCommitResult, error) {
	var zero ConversationCommitResult
	rec := boundary.Record
	if strings.TrimSpace(rec.AgentID) == "" || len(boundary.Events) == 0 {
		return nil, zero, fmt.Errorf("terminal boundary requires runtime and lifecycle events")
	}
	terminal := boundary.Events[len(boundary.Events)-1]
	manifest := make([]terminalBatchFact, 0, len(boundary.Events))
	seenCommands := make(map[string]struct{}, len(boundary.Events))
	for _, event := range boundary.Events {
		commandID := strings.TrimSpace(event.CommandID)
		if commandID == "" {
			return nil, zero, fmt.Errorf("terminal batch command id is required")
		}
		if _, exists := seenCommands[commandID]; exists {
			return nil, zero, fmt.Errorf("duplicate terminal batch command id %q", commandID)
		}
		seenCommands[commandID] = struct{}{}
		manifest = append(manifest, terminalBatchFact{CommandID: commandID, EventType: event.EventType})
	}
	if terminal.EventType != turn.EventTurnCompleted && terminal.EventType != turn.EventTurnFailed && terminal.EventType != turn.EventTurnCancelled && terminal.EventType != turn.EventTurnInterrupted && terminal.EventType != turn.EventTurnBudgetExhausted {
		return nil, zero, fmt.Errorf("terminal boundary does not end with a terminal Turn fact")
	}
	if terminal.EventType == turn.EventTurnCompleted && boundary.SnapshotStatus == "" {
		return nil, zero, fmt.Errorf("completed terminal boundary requires explicit snapshot status")
	}
	if boundary.Delta != nil && boundary.Delta.ConversationID != rec.AgentID {
		return nil, zero, fmt.Errorf("terminal delta conversation_id does not match runtime")
	}
	skillsRaw, err := json.Marshal(rec.LoadedSkills)
	if err != nil {
		return nil, zero, err
	}
	runtimeRaw, err := json.Marshal(rec.RuntimeState)
	if err != nil {
		return nil, zero, err
	}
	created, updated := rec.CreatedAt, rec.UpdatedAt
	now := time.Now().UTC()
	if created.IsZero() {
		created = now
	}
	if updated.IsZero() {
		updated = now
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, zero, err
	}
	defer func() { _ = tx.Rollback() }()
	// A retry after an ambiguous COMMIT must not reapply the content delta or
	// advance either revision. The terminal command is the batch idempotency key.
	if terminal.CommandID != "" {
		var existingType, existingTurn string
		err := tx.QueryRowContext(ctx, `SELECT event_type,turn_id FROM turn_events WHERE session_id=? AND command_id=?`, terminal.SessionID, terminal.CommandID).Scan(&existingType, &existingTurn)
		if err == nil {
			if existingType != string(terminal.EventType) || existingTurn != terminal.TurnID {
				return nil, zero, fmt.Errorf("terminal command id %q is already used by another fact", terminal.CommandID)
			}
			stored := make([]turn.TurnEventEnvelope, 0, len(boundary.Events))
			var revisionFields map[string]json.RawMessage
			var terminalPayload string
			if err := tx.QueryRowContext(ctx, `SELECT payload_json FROM turn_events WHERE session_id=? AND command_id=?`, terminal.SessionID, terminal.CommandID).Scan(&terminalPayload); err != nil {
				return nil, zero, err
			}
			if err := json.Unmarshal([]byte(terminalPayload), &revisionFields); err != nil {
				return nil, zero, err
			}
			var storedConversationRevision, storedCheckpointRevision uint64
			if err := json.Unmarshal(revisionFields["conversation_revision"], &storedConversationRevision); err != nil {
				return nil, zero, fmt.Errorf("stored terminal conversation revision missing or invalid: %w", err)
			}
			if err := json.Unmarshal(revisionFields["checkpoint_revision"], &storedCheckpointRevision); err != nil {
				return nil, zero, fmt.Errorf("stored terminal checkpoint revision missing or invalid: %w", err)
			}
			storedDigest := ""
			if err := json.Unmarshal(revisionFields["boundary_digest"], &storedDigest); err != nil || storedDigest == "" {
				return nil, zero, fmt.Errorf("stored terminal boundary digest missing or invalid")
			}
			expectedDigest, err := terminalBoundaryDigest(rec, boundary.Delta, boundary.RoundRevision, boundary.CompletedInputSeq, boundary.CompletedInputRevision, storedConversationRevision, storedCheckpointRevision)
			if err != nil {
				return nil, zero, err
			}
			if storedDigest != expectedDigest {
				return nil, zero, fmt.Errorf("terminal command %q retry runtime/delta differs from committed boundary", terminal.CommandID)
			}
			var storedManifest []terminalBatchFact
			if err := json.Unmarshal(revisionFields["terminal_batch"], &storedManifest); err != nil {
				return nil, zero, fmt.Errorf("stored terminal batch manifest missing or invalid: %w", err)
			}
			if len(storedManifest) != len(manifest) {
				return nil, zero, fmt.Errorf("terminal command %q retry batch length differs from committed batch", terminal.CommandID)
			}
			for i := range manifest {
				if storedManifest[i] != manifest[i] {
					return nil, zero, fmt.Errorf("terminal command %q retry batch differs at index %d", terminal.CommandID, i)
				}
			}
			for _, input := range boundary.Events {
				if input.CommandID == "" {
					return nil, zero, fmt.Errorf("terminal batch command id is required")
				}
				var e turn.TurnEventEnvelope
				var payload, created string
				err := tx.QueryRowContext(ctx, `SELECT event_id,agent_id,session_id,turn_id,step_id,tool_batch_id,tool_call_id,tool_execution_id,interaction_id,session_seq,turn_seq,event_type,event_version,source,command_id,payload_json,payload_ref,created_at FROM turn_events WHERE session_id=? AND command_id=?`, input.SessionID, input.CommandID).Scan(&e.ID, &e.AgentID, &e.SessionID, &e.TurnID, &e.StepID, &e.ToolBatchID, &e.ToolCallID, &e.ToolExecutionID, &e.InteractionID, &e.SessionSeq, &e.TurnSeq, &e.EventType, &e.EventVersion, &e.Source, &e.CommandID, &payload, &e.PayloadRef, &created)
				if err != nil {
					return nil, zero, fmt.Errorf("terminal batch is incomplete at command %q: %w", input.CommandID, err)
				}
				var expectedPayload map[string]json.RawMessage
				if err := json.Unmarshal(input.Payload, &expectedPayload); err != nil {
					return nil, zero, err
				}
				for _, key := range []string{"conversation_revision", "checkpoint_revision", "lifecycle_covered_seq"} {
					if value, ok := revisionFields[key]; ok {
						expectedPayload[key] = value
					}
				}
				if input.CommandID == terminal.CommandID {
					expectedPayload["boundary_digest"] = revisionFields["boundary_digest"]
					manifestRaw, _ := json.Marshal(manifest)
					expectedPayload["terminal_batch"] = manifestRaw
				}
				expectedRaw, marshalErr := json.Marshal(expectedPayload)
				if marshalErr != nil {
					return nil, zero, marshalErr
				}
				if e.AgentID != input.AgentID || e.EventType != input.EventType || e.TurnID != input.TurnID || e.StepID != input.StepID || e.ToolBatchID != input.ToolBatchID || e.ToolCallID != input.ToolCallID || e.ToolExecutionID != input.ToolExecutionID || e.InteractionID != input.InteractionID || e.Source != input.Source || e.PayloadRef != input.PayloadRef || payload != string(expectedRaw) {
					return nil, zero, fmt.Errorf("terminal command id %q conflicts with stored fact", input.CommandID)
				}
				e.Payload = json.RawMessage(payload)
				e.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
				stored = append(stored, e)
			}
			if terminal.EventType == turn.EventTurnCompleted {
				var storedMessages, storedStatus string
				if err := tx.QueryRowContext(ctx, `SELECT conversation_revision,checkpoint_revision,messages_json,status FROM completed_turn_snapshots WHERE event_id=?`, stored[len(stored)-1].ID).Scan(&zero.ConversationRevision, &zero.CheckpointRevision, &storedMessages, &storedStatus); err != nil {
					return nil, ConversationCommitResult{}, fmt.Errorf("terminal snapshot missing for committed command: %w", err)
				}
				expectedMessages, _ := json.Marshal(boundary.SnapshotMessages)
				if storedStatus != boundary.SnapshotStatus || storedMessages != string(expectedMessages) {
					return nil, ConversationCommitResult{}, fmt.Errorf("terminal command %q retry snapshot differs from committed snapshot", terminal.CommandID)
				}
			} else {
				_ = tx.QueryRowContext(ctx, `SELECT revision,current_checkpoint_revision FROM conversations WHERE conversation_id=?`, rec.AgentID).Scan(&zero.ConversationRevision, &zero.CheckpointRevision)
			}
			if err := tx.Commit(); err != nil {
				return nil, ConversationCommitResult{}, err
			}
			return stored, zero, nil
		} else if err != sql.ErrNoRows {
			return nil, zero, err
		}
	}
	if inputQueueRowsProjected(rec.RuntimeState.InputBoxState, rec.InputQueueRows) {
		if err := persistInputQueueStateTx(ctx, tx, rec.AgentID, rec.RuntimeState, rec.InputQueueRows); err != nil {
			return nil, zero, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_runtimes(agent_id,node_id,messages_json,loaded_skills_json,runtime_state_json,first_user_message,created_at,updated_at) VALUES(?,?,'[]',?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET node_id=excluded.node_id,loaded_skills_json=excluded.loaded_skills_json,runtime_state_json=excluded.runtime_state_json,first_user_message=CASE WHEN excluded.first_user_message!='' THEN excluded.first_user_message ELSE agent_runtimes.first_user_message END,updated_at=excluded.updated_at`, rec.AgentID, rec.NodeID, string(skillsRaw), string(runtimeRaw), strings.TrimSpace(rec.FirstUserMessage), created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano)); err != nil {
		return nil, zero, err
	}
	result := ConversationCommitResult{ConversationRevision: rec.RuntimeState.ConversationRevision, CheckpointRevision: rec.RuntimeState.ContextCheckpointRevision}
	if boundary.Delta != nil {
		result, err = applyConversationDeltaTx(ctx, tx, *boundary.Delta)
		if err != nil {
			return nil, zero, err
		}
	}
	if boundary.RoundRevision != nil {
		if err := validateRoundRevisionIdentity(rec.AgentID, *boundary.RoundRevision); err != nil {
			return nil, zero, err
		}
		if boundary.RoundRevision.TurnID != terminal.TurnID || boundary.RoundRevision.Source != terminal.Source || boundary.RoundRevision.Generation == 0 {
			return nil, zero, fmt.Errorf("terminal lifecycle fact does not match round revision identity")
		}
		status := "failed"
		finishReason := string(terminal.EventType)
		switch terminal.EventType {
		case turn.EventTurnCompleted:
			status = "completed"
			if boundary.SnapshotStatus == "no_work" {
				finishReason = "no_work"
			}
		case turn.EventTurnCancelled:
			status = "cancelled"
		case turn.EventTurnInterrupted:
			status = "interrupted"
		case turn.EventTurnBudgetExhausted:
			status = "budget_exhausted"
		case turn.EventTurnFailed:
			status = "failed"
		default:
			return nil, zero, fmt.Errorf("unsupported terminal event for round revision")
		}
		res, err := tx.ExecContext(ctx, `UPDATE conversation_round_revisions SET status=?,finish_reason=?,record_revision=?,checkpoint_revision=? WHERE conversation_id=? AND round_id=? AND revision_id=? AND turn_id=? AND source=? AND generation=? AND status='active'`, status, finishReason, result.ConversationRevision, result.CheckpointRevision, rec.AgentID, boundary.RoundRevision.RoundID, boundary.RoundRevision.RevisionID, boundary.RoundRevision.TurnID, boundary.RoundRevision.Source, boundary.RoundRevision.Generation)
		if err != nil {
			return nil, zero, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return nil, zero, fmt.Errorf("active round revision not found for terminal turn %s", terminal.TurnID)
		}
	}
	rec.RuntimeState.ConversationRevision = result.ConversationRevision
	rec.RuntimeState.ContextCheckpointRevision = result.CheckpointRevision
	boundaryDigest, err := terminalBoundaryDigest(rec, boundary.Delta, boundary.RoundRevision, boundary.CompletedInputSeq, boundary.CompletedInputRevision, result.ConversationRevision, result.CheckpointRevision)
	if err != nil {
		return nil, zero, err
	}
	runtimeRaw, err = json.Marshal(rec.RuntimeState)
	if err != nil {
		return nil, zero, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE agent_runtimes SET runtime_state_json=? WHERE agent_id=?`, string(runtimeRaw), rec.AgentID); err != nil {
		return nil, zero, err
	}
	if boundary.CompletedInputSeq > 0 {
		if boundary.CompletedInputRevision == 0 {
			return nil, zero, fmt.Errorf("completed input revision is required")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_inputs SET status='consumed',input_revision=? WHERE conversation_id=? AND input_seq=?`, boundary.CompletedInputRevision, rec.AgentID, boundary.CompletedInputSeq); err != nil {
			return nil, zero, err
		}
	}
	storedEvents := make([]turn.TurnEventEnvelope, 0, len(boundary.Events))
	for _, event := range boundary.Events {
		if err := validateTurnEvent(s, &event); err != nil {
			return nil, zero, err
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return nil, zero, err
		}
		payload["conversation_revision"] = result.ConversationRevision
		payload["checkpoint_revision"] = result.CheckpointRevision
		payload["lifecycle_covered_seq"] = uint64(0)
		if event.CommandID == terminal.CommandID {
			payload["terminal_batch"] = manifest
			payload["boundary_digest"] = boundaryDigest
		}
		event.Payload, err = json.Marshal(payload)
		if err != nil {
			return nil, zero, err
		}
		stored, _, appendErr := s.appendTurnEventTx(ctx, tx, event)
		if appendErr != nil {
			return nil, zero, appendErr
		}
		storedEvents = append(storedEvents, stored)
	}
	if terminal.EventType == turn.EventTurnCompleted {
		raw, err := json.Marshal(boundary.SnapshotMessages)
		if err != nil {
			return nil, zero, err
		}
		status := boundary.SnapshotStatus
		if status != "readable" && status != "no_work" && status != "unreadable" {
			return nil, zero, fmt.Errorf("invalid completed snapshot status %q", status)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO completed_turn_snapshots(event_id,agent_id,session_id,turn_id,messages_json,status,conversation_revision,checkpoint_revision,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, storedEvents[len(storedEvents)-1].ID, terminal.AgentID, terminal.SessionID, terminal.TurnID, string(raw), status, result.ConversationRevision, result.CheckpointRevision, storedEvents[len(storedEvents)-1].CreatedAt.UTC().Format(time.RFC3339Nano)); err != nil {
			return nil, zero, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, zero, err
	}
	return storedEvents, result, nil
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
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,agent_id,session_id,turn_id,messages_json,status,conversation_revision,checkpoint_revision FROM completed_turn_snapshots WHERE agent_id=? AND event_id>? ORDER BY event_id LIMIT ?`, strings.TrimSpace(agentID), afterEventID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CompletedTurnSnapshot
	for rows.Next() {
		var item CompletedTurnSnapshot
		var raw string
		if err := rows.Scan(&item.EventID, &item.AgentID, &item.SessionID, &item.TurnID, &raw, &item.Status, &item.ConversationRevision, &item.CheckpointRevision); err != nil {
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

// AppendTurnStartBoundary commits the lifecycle start fact and its explicit
// logical-round/revision mapping together. A retry with the same command is
// accepted only when the complete mapping is unchanged.
func (s *SQLiteStore) AppendTurnStartBoundary(ctx context.Context, events []turn.TurnEventEnvelope, revision RoundRevision, runtimeState RuntimeState, inputQueueRows ...[]InputQueueRow) ([]turn.TurnEventEnvelope, error) {
	if len(events) != 2 || events[0].EventType != turn.EventTurnStarted || events[1].EventType != turn.EventStepStarted {
		return nil, fmt.Errorf("round start boundary requires turn.started followed by step.started")
	}
	if err := validateRoundRevisionIdentity(events[0].SessionID, revision); err != nil {
		return nil, err
	}
	for i := range events {
		event := &events[i]
		if event.TurnID != revision.TurnID || string(event.Source) != revision.Source || event.SessionID != revision.ConversationID {
			return nil, fmt.Errorf("turn start batch does not match round revision")
		}
		if err := validateTurnEvent(s, event); err != nil {
			return nil, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var checkpointRevision, conversationRevision uint64
	conversationErr := tx.QueryRowContext(ctx, `SELECT revision,current_checkpoint_revision FROM conversations WHERE conversation_id=?`, revision.ConversationID).Scan(&conversationRevision, &checkpointRevision)
	if conversationErr != nil && conversationErr != sql.ErrNoRows {
		return nil, conversationErr
	}
	if conversationErr == sql.ErrNoRows {
		conversationRevision, checkpointRevision = 0, 0
	}
	if revision.ResubmitFromRevisionID != "" {
		if revision.ResubmitCheckpoint == nil || revision.ExpectedConversationRevision == 0 || conversationRevision != revision.ExpectedConversationRevision {
			return nil, fmt.Errorf("%w: conversation changed before resubmit started", ErrConversationResubmitConflict)
		}
		if checkpointRevision == ^uint64(0) || conversationRevision == ^uint64(0) {
			return nil, fmt.Errorf("conversation revision exhausted")
		}
		var baseExists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM context_checkpoints WHERE conversation_id=? AND revision=?)`, revision.ConversationID, revision.ExpectedPreCheckpointRevision).Scan(&baseExists); err != nil {
			return nil, err
		}
		if baseExists == 0 {
			return nil, fmt.Errorf("%w: pre-round checkpoint is not retained", ErrConversationResubmitConflict)
		}
		checkpointRevision++
		conversationRevision++
		baseRaw, err := json.Marshal(revision.ResubmitCheckpoint.Messages)
		if err != nil {
			return nil, err
		}
		stamp := events[0].CreatedAt.UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES(?,?,?,?,0,?)`, revision.ConversationID, checkpointRevision, string(baseRaw), "resubmit_base", stamp); err != nil {
			return nil, err
		}
		result, err := tx.ExecContext(ctx, `UPDATE conversations SET revision=?,current_checkpoint_revision=?,updated_at=? WHERE conversation_id=? AND revision=?`, conversationRevision, checkpointRevision, stamp, revision.ConversationID, revision.ExpectedConversationRevision)
		if err != nil {
			return nil, err
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: conversation changed before resubmit started", ErrConversationResubmitConflict)
		}
		runtimeState.ConversationRevision = conversationRevision
		runtimeState.ContextCheckpointRevision = checkpointRevision
		if err := pruneConversationCheckpointsTx(ctx, tx, revision.ConversationID, checkpointRevision); err != nil {
			return nil, err
		}
	}
	if checkpointRevision == 0 {
		// Revision zero is the explicit empty execution base for the first
		// round. Retaining a real row lets resubmit distinguish an empty base
		// from a missing/corrupt checkpoint.
		stamp := events[0].CreatedAt.UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES(?,0,'[]','initial',0,?) ON CONFLICT(conversation_id,revision) DO NOTHING`, revision.ConversationID, stamp); err != nil {
			return nil, err
		}
	}
	revision.PreCheckpointRevision = checkpointRevision
	runtimeState.ActiveRoundID, runtimeState.ActiveRevisionID = revision.RoundID, revision.RevisionID
	runtimeRaw, err := json.Marshal(runtimeState)
	if err != nil {
		return nil, err
	}
	stamp := events[0].CreatedAt.UTC().Format(time.RFC3339Nano)
	var queueRows []InputQueueRow
	if len(inputQueueRows) > 0 {
		queueRows = inputQueueRows[0]
	}
	if inputQueueRowsProjected(runtimeState.InputBoxState, queueRows) {
		if err := persistInputQueueStateTx(ctx, tx, revision.ConversationID, runtimeState, queueRows); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO agent_runtimes(agent_id,node_id,messages_json,loaded_skills_json,runtime_state_json,first_user_message,created_at,updated_at) VALUES(?,?,'[]','[]',?,'',?,?) ON CONFLICT(agent_id) DO UPDATE SET runtime_state_json=excluded.runtime_state_json,node_id=excluded.node_id,updated_at=excluded.updated_at`, revision.ConversationID, events[0].AgentID, string(runtimeRaw), stamp, stamp); err != nil {
		return nil, err
	}
	storedEvents := make([]turn.TurnEventEnvelope, 0, len(events))
	for _, event := range events {
		stored, _, err := s.appendTurnEventTx(ctx, tx, event)
		if err != nil {
			return nil, err
		}
		storedEvents = append(storedEvents, stored)
	}
	var ordinal int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0) FROM conversation_rounds WHERE conversation_id=?`, revision.ConversationID).Scan(&ordinal); err != nil {
		return nil, err
	}
	preCheckpointRevision := checkpointRevision
	if revision.ResubmitFromRevisionID == "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_rounds(conversation_id,round_id,ordinal,selected_revision_id,pre_checkpoint_revision) VALUES(?,?,?,?,?) ON CONFLICT(conversation_id,round_id) DO NOTHING`, revision.ConversationID, revision.RoundID, ordinal+1, revision.RevisionID, checkpointRevision); err != nil {
			return nil, err
		}
		var selected string
		if err := tx.QueryRowContext(ctx, `SELECT selected_revision_id FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, revision.ConversationID, revision.RoundID).Scan(&selected); err != nil {
			return nil, err
		}
		if selected != revision.RevisionID {
			return nil, fmt.Errorf("round %s already selects revision %s", revision.RoundID, selected)
		}
	} else {
		if revision.Source != "human" {
			return nil, fmt.Errorf("only human turns can resubmit a conversation revision")
		}
		var targetOrdinal int64
		var selected string
		var storedPreCheckpoint uint64
		if err := tx.QueryRowContext(ctx, `SELECT ordinal,selected_revision_id,pre_checkpoint_revision FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, revision.ConversationID, revision.RoundID).Scan(&targetOrdinal, &selected, &storedPreCheckpoint); err != nil {
			return nil, fmt.Errorf("resubmit round is unavailable: %w", err)
		}
		if targetOrdinal != ordinal {
			return nil, fmt.Errorf("%w: only the latest logical round can be resubmitted", ErrConversationResubmitConflict)
		}
		if revision.ExpectedPreCheckpointRevision != storedPreCheckpoint {
			return nil, fmt.Errorf("%w: pre-round checkpoint changed", ErrConversationResubmitConflict)
		}
		var oldSource, oldStatus string
		if err := tx.QueryRowContext(ctx, `SELECT source,status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, revision.ConversationID, revision.RoundID, revision.ResubmitFromRevisionID).Scan(&oldSource, &oldStatus); err != nil {
			return nil, fmt.Errorf("%w: prior revision is unavailable", ErrConversationResubmitConflict)
		}
		if oldSource != "human" || oldStatus == "active" {
			return nil, fmt.Errorf("%w: prior revision is not a terminal human turn", ErrConversationResubmitConflict)
		}
		var checkpointExists int
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM context_checkpoints WHERE conversation_id=? AND revision=?)`, revision.ConversationID, storedPreCheckpoint).Scan(&checkpointExists); err != nil {
			return nil, err
		}
		if checkpointExists == 0 {
			return nil, fmt.Errorf("%w: pre-round checkpoint is not retained", ErrConversationResubmitConflict)
		}
		if selected == revision.ResubmitFromRevisionID {
			result, err := tx.ExecContext(ctx, `UPDATE conversation_rounds SET selected_revision_id=? WHERE conversation_id=? AND round_id=? AND selected_revision_id=?`, revision.RevisionID, revision.ConversationID, revision.RoundID, revision.ResubmitFromRevisionID)
			if err != nil {
				return nil, err
			}
			changed, err := result.RowsAffected()
			if err != nil {
				return nil, err
			}
			if changed != 1 {
				return nil, fmt.Errorf("%w: selected revision changed", ErrConversationResubmitConflict)
			}
		} else if selected != revision.RevisionID {
			return nil, fmt.Errorf("%w: selected revision changed", ErrConversationResubmitConflict)
		}
		preCheckpointRevision = storedPreCheckpoint
	}
	revision.PreCheckpointRevision = preCheckpointRevision
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_round_revisions(conversation_id,round_id,revision_id,turn_id,source,generation,status,pre_checkpoint_revision) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(conversation_id,round_id,revision_id) DO NOTHING`, revision.ConversationID, revision.RoundID, revision.RevisionID, revision.TurnID, revision.Source, revision.Generation, "active", preCheckpointRevision); err != nil {
		return nil, err
	}
	var storedTurn, storedSource string
	var storedGeneration uint64
	var storedStatus string
	var storedRevisionPreCheckpoint uint64
	if err := tx.QueryRowContext(ctx, `SELECT turn_id,source,generation,status,pre_checkpoint_revision FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, revision.ConversationID, revision.RoundID, revision.RevisionID).Scan(&storedTurn, &storedSource, &storedGeneration, &storedStatus, &storedRevisionPreCheckpoint); err != nil {
		return nil, err
	}
	if storedTurn != revision.TurnID || storedSource != revision.Source || storedGeneration != revision.Generation || storedStatus != "active" || storedRevisionPreCheckpoint != preCheckpointRevision {
		return nil, fmt.Errorf("round revision identity conflicts with existing execution")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return storedEvents, nil
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

// TurnEventLogVersion returns the newest durable event ID and event count for
// a session. The pair detects both appends and ordinary deletion/rebuilds
// without reading or retaining the journal payloads.
func (s *SQLiteStore) TurnEventLogVersion(ctx context.Context, sessionID string) (latestID int64, count int64, err error) {
	if s == nil || s.db == nil {
		return 0, 0, fmt.Errorf("store is nil")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, 0, fmt.Errorf("session id is required")
	}
	err = s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(event_id), 0), COUNT(*) FROM turn_events WHERE session_id = ?`, sessionID).Scan(&latestID, &count)
	return latestID, count, err
}
