package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

const conversationStorageVersion = 4

// ConversationWatermarkSummary is the content-free subset used by clients to
// detect that an Agent snapshot may need reconciliation. It deliberately does
// not include messages, checkpoint bodies, or tool results.
type ConversationWatermarkSummary struct {
	ConversationID       string
	NodeID               string
	ConversationRevision uint64
}

// ListConversationWatermarks reads only the conversation revision table and
// stable Agent identifiers. It never selects either runtime-state JSON or
// message/checkpoint payloads, so its cost does not grow with those payloads.
func (s *SQLiteStore) ListConversationWatermarks(ctx context.Context) ([]ConversationWatermarkSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT r.agent_id, r.node_id, COALESCE(c.revision, 0)
FROM agent_runtimes r
LEFT JOIN conversations c ON c.conversation_id = r.agent_id
ORDER BY r.agent_id
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConversationWatermarkSummary
	for rows.Next() {
		var item ConversationWatermarkSummary
		if err := rows.Scan(&item.ConversationID, &item.NodeID, &item.ConversationRevision); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type RecordOwnership struct {
	RoundID    string
	RevisionID string
}

type RoundRevision struct {
	ConversationID                string
	RoundID                       string
	RevisionID                    string
	TurnID                        string
	Source                        string
	Generation                    uint64
	Status                        string
	FinishReason                  string
	PreCheckpointRevision         uint64
	ResubmitFromRevisionID        string
	ExpectedConversationRevision  uint64
	ExpectedPreCheckpointRevision uint64
	ResubmitCheckpoint            *CheckpointCommit
}

var ErrConversationResubmitConflict = errors.New("conversation resubmit target changed")

func validateConversationResubmitGuardTx(ctx context.Context, tx *sql.Tx, conversationID string, guard ConversationResubmitGuard) error {
	conversationID = strings.TrimSpace(conversationID)
	guard.RoundID = strings.TrimSpace(guard.RoundID)
	guard.ExpectedSelectedRevisionID = strings.TrimSpace(guard.ExpectedSelectedRevisionID)
	if conversationID == "" || guard.RoundID == "" || guard.ExpectedSelectedRevisionID == "" || guard.ExpectedConversationRevision == 0 {
		return fmt.Errorf("%w: incomplete target identity", ErrConversationResubmitConflict)
	}
	var revision uint64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM conversations WHERE conversation_id=?`, conversationID).Scan(&revision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: conversation is missing", ErrConversationResubmitConflict)
		}
		return err
	}
	if revision != guard.ExpectedConversationRevision {
		return fmt.Errorf("%w: conversation revision changed", ErrConversationResubmitConflict)
	}
	var selectedRevision string
	var ordinal int64
	var preCheckpointRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT selected_revision_id,ordinal,pre_checkpoint_revision FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, conversationID, guard.RoundID).Scan(&selectedRevision, &ordinal, &preCheckpointRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: logical round is missing", ErrConversationResubmitConflict)
		}
		return err
	}
	if selectedRevision != guard.ExpectedSelectedRevisionID || preCheckpointRevision != guard.PreCheckpointRevision {
		return fmt.Errorf("%w: selected revision or checkpoint changed", ErrConversationResubmitConflict)
	}
	var latestOrdinal int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0) FROM conversation_rounds WHERE conversation_id=?`, conversationID).Scan(&latestOrdinal); err != nil {
		return err
	}
	if ordinal == 0 || ordinal != latestOrdinal {
		return fmt.Errorf("%w: only the latest logical round can be resubmitted", ErrConversationResubmitConflict)
	}
	var source, status string
	if err := tx.QueryRowContext(ctx, `SELECT source,status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, conversationID, guard.RoundID, selectedRevision).Scan(&source, &status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: selected revision is missing", ErrConversationResubmitConflict)
		}
		return err
	}
	if source != "human" || status == "active" {
		return fmt.Errorf("%w: selected revision is not a terminal human turn", ErrConversationResubmitConflict)
	}
	var checkpointExists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM context_checkpoints WHERE conversation_id=? AND revision=?)`, conversationID, guard.PreCheckpointRevision).Scan(&checkpointExists); err != nil {
		return err
	}
	if checkpointExists == 0 {
		return fmt.Errorf("%w: pre-round checkpoint is not retained", ErrConversationResubmitConflict)
	}
	var activeRevisions int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_round_revisions WHERE conversation_id=? AND status='active'`, conversationID).Scan(&activeRevisions); err != nil {
		return err
	}
	if activeRevisions != 0 {
		return fmt.Errorf("%w: another turn is active", ErrConversationResubmitConflict)
	}
	var queuedInputs int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_input_queue WHERE conversation_id=?`, conversationID).Scan(&queuedInputs); err != nil {
		return err
	}
	if queuedInputs != 0 {
		return fmt.Errorf("%w: pending input queue is not empty", ErrConversationResubmitConflict)
	}
	return nil
}

// ConversationResubmitGuard binds an accepted edit-and-resubmit input to the
// exact terminal revision and retained pre-round checkpoint the user saw.
// It is checked in the same transaction that persists the new input row.
type ConversationResubmitGuard struct {
	RoundID                      string
	ExpectedSelectedRevisionID   string
	ExpectedConversationRevision uint64
	PreCheckpointRevision        uint64
}

// ConversationDelta is the only post-migration write path for transcript data.
// Upserts must be explicit item mutations; the Store never compares snapshots.
type ConversationDelta struct {
	ConversationID   string
	NodeID           string
	ExpectedRevision uint64
	HistoryRevision  uint64
	Upserts          []llm.Message
	RecordOwners     map[string]RecordOwnership
	RoundRevision    *RoundRevision
	DeleteIDs        []string
	Checkpoint       *CheckpointCommit
}

// CheckpointCommit updates the model working set without changing display records.
type CheckpointCommit struct {
	Messages []llm.Message
	Lineage  string
	// Delta is nil for a full snapshot. Otherwise it applies to BaseRevision and
	// stores only the ordered mutations for this checkpoint revision.
	Delta *CheckpointDelta
}

type CheckpointDelta struct {
	BaseRevision uint64
	Operations   []CheckpointOperation
}

type CheckpointOperation struct {
	Kind    string // append, insert, or replace
	Index   int
	Message llm.Message
}

type ConversationCommitResult struct {
	ConversationRevision uint64
	CheckpointRevision   uint64
	ChangedItems         []ConversationItemRevision
	ChangedPayloads      []llm.Message
	RemovedItemIDs       []string
	RemovedPayloads      []llm.Message
}

// ConversationItemRevision is the committed display-record identity metadata
// needed to publish a stable transcript patch without reading the rows again.
type ConversationItemRevision struct {
	ItemID       string
	Ordinal      int64
	Revision     uint64
	RoundID      string
	RevisionID   string
	RoundOrdinal int64
}

func validateRoundRevisionIdentity(conversationID string, revision RoundRevision) error {
	if strings.TrimSpace(conversationID) == "" || strings.TrimSpace(revision.ConversationID) != strings.TrimSpace(conversationID) ||
		strings.TrimSpace(revision.RoundID) == "" || strings.TrimSpace(revision.RevisionID) == "" ||
		strings.TrimSpace(revision.TurnID) == "" || strings.TrimSpace(revision.Source) == "" || revision.Generation == 0 {
		return fmt.Errorf("round revision identity is incomplete")
	}
	return nil
}

func validateCheckpointOperation(operation CheckpointOperation) error {
	switch operation.Kind {
	case "append":
	case "insert", "replace":
		if operation.Index < 0 {
			return fmt.Errorf("checkpoint %s index must be non-negative", operation.Kind)
		}
	default:
		return fmt.Errorf("unsupported checkpoint operation %q", operation.Kind)
	}
	if strings.TrimSpace(operation.Message.Role) == "" {
		return fmt.Errorf("checkpoint %s operation requires a message role", operation.Kind)
	}
	return nil
}

func applyCheckpointOperations(messages []llm.Message, operations []CheckpointOperation) ([]llm.Message, error) {
	for _, operation := range operations {
		if err := validateCheckpointOperation(operation); err != nil {
			return nil, err
		}
		switch operation.Kind {
		case "append":
			messages = append(messages, operation.Message)
		case "insert":
			if operation.Index > len(messages) {
				return nil, fmt.Errorf("checkpoint insert index %d exceeds message count %d", operation.Index, len(messages))
			}
			messages = append(messages, llm.Message{})
			copy(messages[operation.Index+1:], messages[operation.Index:])
			messages[operation.Index] = operation.Message
		case "replace":
			if operation.Index >= len(messages) {
				return nil, fmt.Errorf("checkpoint replace index %d exceeds message count %d", operation.Index, len(messages))
			}
			messages[operation.Index] = operation.Message
		}
	}
	return messages, nil
}

// PersistRuntimeDelta saves runtime metadata and, only when supplied, an
// explicit records/checkpoint delta in one transaction. A nil delta touches no
// conversation_records or context_checkpoints rows.
func (s *SQLiteStore) PersistRuntimeDelta(ctx context.Context, rec Record, delta *ConversationDelta) (ConversationCommitResult, error) {
	var result ConversationCommitResult
	if strings.TrimSpace(rec.AgentID) == "" {
		return result, fmt.Errorf("agent_id is required")
	}
	skillsRaw, err := json.Marshal(rec.LoadedSkills)
	if err != nil {
		return result, err
	}
	runtimeRaw, err := json.Marshal(rec.RuntimeState)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	created, updated := rec.CreatedAt, rec.UpdatedAt
	if created.IsZero() {
		created = now
	}
	if updated.IsZero() {
		updated = now
	}
	first := strings.TrimSpace(rec.FirstUserMessage)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	if rec.ResubmitGuard != nil {
		if err := validateConversationResubmitGuardTx(ctx, tx, rec.AgentID, *rec.ResubmitGuard); err != nil {
			return result, err
		}
	}
	if inputQueueRowsProjected(rec.RuntimeState.InputBoxState, rec.InputQueueRows) {
		if err := persistInputQueueStateTx(ctx, tx, rec.AgentID, rec.RuntimeState, rec.InputQueueRows); err != nil {
			return result, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO agent_runtimes(agent_id,node_id,messages_json,loaded_skills_json,runtime_state_json,first_user_message,created_at,updated_at) VALUES(?,?,'[]',?,?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET node_id=excluded.node_id,loaded_skills_json=excluded.loaded_skills_json,runtime_state_json=excluded.runtime_state_json,first_user_message=CASE WHEN excluded.first_user_message!='' THEN excluded.first_user_message ELSE agent_runtimes.first_user_message END,updated_at=excluded.updated_at`, rec.AgentID, rec.NodeID, string(skillsRaw), string(runtimeRaw), first, created.Format(time.RFC3339Nano), updated.Format(time.RFC3339Nano)); err != nil {
		return result, err
	}
	if delta != nil {
		if strings.TrimSpace(delta.ConversationID) != rec.AgentID {
			return result, fmt.Errorf("delta conversation_id does not match runtime")
		}
		result, err = applyConversationDeltaTx(ctx, tx, *delta)
		if err != nil {
			return ConversationCommitResult{}, err
		}
	} else {
		result.ConversationRevision = rec.RuntimeState.ConversationRevision
		result.CheckpointRevision = rec.RuntimeState.ContextCheckpointRevision
	}
	if rec.AcceptedInputReceipt != nil {
		if err := saveInputReceiptTx(ctx, tx, rec.AgentID, *rec.AcceptedInputReceipt); err != nil {
			return ConversationCommitResult{}, err
		}
	}
	if rec.CompletedInputSeq > 0 {
		if rec.CompletedInputRevision == 0 {
			return ConversationCommitResult{}, fmt.Errorf("completed input revision is required")
		}
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_inputs SET status='consumed',input_revision=? WHERE conversation_id=? AND input_seq=?`, rec.CompletedInputRevision, rec.AgentID, rec.CompletedInputSeq); err != nil {
			return ConversationCommitResult{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ConversationCommitResult{}, err
	}
	return result, nil
}

func applyConversationDeltaTx(ctx context.Context, tx *sql.Tx, delta ConversationDelta) (ConversationCommitResult, error) {
	var result ConversationCommitResult
	id := strings.TrimSpace(delta.ConversationID)
	if id == "" {
		return result, fmt.Errorf("conversation_id is required")
	}
	var current, checkpointRevision uint64
	err := tx.QueryRowContext(ctx, `SELECT revision,current_checkpoint_revision FROM conversations WHERE conversation_id=?`, id).Scan(&current, &checkpointRevision)
	if err == sql.ErrNoRows {
		if delta.ExpectedRevision != 0 {
			return result, fmt.Errorf("conversation revision conflict: expected %d, found 0", delta.ExpectedRevision)
		}
		current, checkpointRevision = 0, 0
	} else if err != nil {
		return result, err
	} else if current != delta.ExpectedRevision {
		return result, fmt.Errorf("conversation revision conflict: expected %d, found %d", delta.ExpectedRevision, current)
	}
	changed := len(delta.Upserts) > 0 || len(delta.DeleteIDs) > 0 || delta.Checkpoint != nil
	if !changed {
		result.ConversationRevision, result.CheckpointRevision = current, checkpointRevision
		return result, nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var ordinal int64
	for _, message := range delta.Upserts {
		itemID := strings.TrimSpace(message.RecordID)
		if itemID == "" {
			return result, fmt.Errorf("record identity required for %s item", message.Role)
		}
		if message.Name == llm.UserNameCompression {
			return result, fmt.Errorf("compression artifacts cannot be conversation records")
		}
		payload, err := json.Marshal(message)
		if err != nil {
			return result, err
		}
		legacyKind := ""
		if llm.IsHiddenInjectedUserMessage(message) || llm.IsMessageSource(message, llm.MessageSourceRuntime, llm.MessageFormSnapshot, "") {
			legacyKind = "context_artifact"
		}
		var existingOrdinal int64
		var existingRevision uint64
		var existingPayload, existingRoundID, existingRevisionID string
		err = tx.QueryRowContext(ctx, `SELECT ordinal,payload_json,round_id,revision_id,revision FROM conversation_records WHERE conversation_id=? AND item_id=?`, id, itemID).Scan(&existingOrdinal, &existingPayload, &existingRoundID, &existingRevisionID, &existingRevision)
		owner := delta.RecordOwners[itemID]
		if (owner.RoundID == "") != (owner.RevisionID == "") {
			return result, fmt.Errorf("record %s has incomplete round ownership", itemID)
		}
		itemOrdinal := existingOrdinal
		itemRevision := existingRevision
		itemRoundID, itemRevisionID := owner.RoundID, owner.RevisionID
		payloadChanged := false
		if err == sql.ErrNoRows {
			if ordinal == 0 {
				if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0) FROM conversation_records WHERE conversation_id=?`, id).Scan(&ordinal); err != nil {
					return result, err
				}
			}
			ordinal++
			itemOrdinal = ordinal
			itemRevision = 1
			payloadChanged = true
			_, err = tx.ExecContext(ctx, `INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,1,?,?,?)`, id, itemID, ordinal, owner.RoundID, owner.RevisionID, message.Role, string(payload), conversationToolCallID(message), legacyKind, now, now)
		} else if err == nil {
			if owner.RoundID != "" && existingRoundID != "" && (owner.RoundID != existingRoundID || owner.RevisionID != existingRevisionID) {
				return result, fmt.Errorf("record %s ownership is immutable", itemID)
			}
			if itemRoundID == "" {
				itemRoundID, itemRevisionID = existingRoundID, existingRevisionID
			}
			if owner.RoundID != "" && existingRoundID == "" {
				if _, err := tx.ExecContext(ctx, `UPDATE conversation_records SET round_id=?,revision_id=? WHERE conversation_id=? AND item_id=?`, owner.RoundID, owner.RevisionID, id, itemID); err != nil {
					return result, err
				}
			}
			// Item version is the durable display-payload revision. Replaying the
			// exact same serialized item must not invalidate client caches.
			if existingPayload != string(payload) {
				_, err = tx.ExecContext(ctx, `UPDATE conversation_records SET role=?,payload_json=?,tool_call_id=?,revision=revision+1,legacy_kind=?,updated_at=? WHERE conversation_id=? AND item_id=?`, message.Role, string(payload), conversationToolCallID(message), legacyKind, now, id, itemID)
				itemRevision++
				payloadChanged = true
			}
		}
		if err != nil {
			return result, err
		}
		if payloadChanged {
			var roundOrdinal int64
			if itemRoundID != "" {
				roundErr := tx.QueryRowContext(ctx, `SELECT ordinal FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, id, itemRoundID).Scan(&roundOrdinal)
				if roundErr != nil && roundErr != sql.ErrNoRows {
					return result, roundErr
				}
			}
			result.ChangedItems = append(result.ChangedItems, ConversationItemRevision{
				ItemID: itemID, Ordinal: itemOrdinal, Revision: itemRevision,
				RoundID: itemRoundID, RevisionID: itemRevisionID, RoundOrdinal: roundOrdinal,
			})
			result.ChangedPayloads = append(result.ChangedPayloads, message)
		}
	}
	for _, itemID := range delta.DeleteIDs {
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			return result, fmt.Errorf("record identity required for delete")
		}
		var payload string
		lookupErr := tx.QueryRowContext(ctx, `SELECT payload_json FROM conversation_records WHERE conversation_id=? AND item_id=?`, id, itemID).Scan(&payload)
		if lookupErr != nil && lookupErr != sql.ErrNoRows {
			return result, lookupErr
		}
		deleted, err := tx.ExecContext(ctx, `DELETE FROM conversation_records WHERE conversation_id=? AND item_id=?`, id, itemID)
		if err != nil {
			return result, err
		}
		if affected, err := deleted.RowsAffected(); err != nil {
			return result, err
		} else if affected > 0 {
			result.RemovedItemIDs = append(result.RemovedItemIDs, itemID)
			if payload != "" {
				var message llm.Message
				if err := json.Unmarshal([]byte(payload), &message); err != nil {
					return result, fmt.Errorf("decode removed conversation record %s: %w", itemID, err)
				}
				result.RemovedPayloads = append(result.RemovedPayloads, message)
			}
		}
	}
	newRevision := current + 1
	if delta.Checkpoint != nil {
		checkpointRevision++
		lineage := strings.TrimSpace(delta.Checkpoint.Lineage)
		if lineage == "" {
			lineage = "live_append"
		}
		if patch := delta.Checkpoint.Delta; patch != nil {
			if checkpointRevision <= 1 || patch.BaseRevision != checkpointRevision-1 {
				return result, fmt.Errorf("checkpoint delta base revision conflict: base %d, current %d", patch.BaseRevision, checkpointRevision-1)
			}
			if len(patch.Operations) == 0 {
				return result, fmt.Errorf("checkpoint delta requires at least one operation")
			}
			for _, operation := range patch.Operations {
				if err := validateCheckpointOperation(operation); err != nil {
					return result, err
				}
			}
			deltaRaw, err := json.Marshal(patch.Operations)
			if err != nil {
				return result, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at,checkpoint_kind,base_revision,delta_json) VALUES(?,?,'[]',?,0,?,'delta',?,?)`, id, checkpointRevision, lineage, now, patch.BaseRevision, string(deltaRaw)); err != nil {
				return result, err
			}
		} else {
			cpRaw, err := json.Marshal(delta.Checkpoint.Messages)
			if err != nil {
				return result, err
			}
			// lifecycle_covered_seq is a reserved legacy column. Lifecycle events
			// are retained and checkpoint pruning does not claim to cover them.
			if _, err := tx.ExecContext(ctx, `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at,checkpoint_kind,base_revision,delta_json) VALUES(?,?,?,?,0,?,'snapshot',0,'')`, id, checkpointRevision, string(cpRaw), lineage, now); err != nil {
				return result, err
			}
		}
		if err := pruneConversationCheckpointsTx(ctx, tx, id, checkpointRevision); err != nil {
			return result, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversations(conversation_id,node_id,revision,current_checkpoint_revision,lifecycle_covered_seq,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(conversation_id) DO UPDATE SET node_id=excluded.node_id,revision=excluded.revision,current_checkpoint_revision=excluded.current_checkpoint_revision,lifecycle_covered_seq=excluded.lifecycle_covered_seq,updated_at=excluded.updated_at`, id, delta.NodeID, newRevision, checkpointRevision, uint64(0), now, now); err != nil {
		return result, err
	}
	if delta.RoundRevision != nil {
		if err := validateRoundRevisionIdentity(id, *delta.RoundRevision); err != nil {
			return result, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE conversation_round_revisions SET record_revision=?,checkpoint_revision=? WHERE conversation_id=? AND round_id=? AND revision_id=? AND turn_id=? AND status='active'`, newRevision, checkpointRevision, id, delta.RoundRevision.RoundID, delta.RoundRevision.RevisionID, delta.RoundRevision.TurnID)
		if err != nil {
			return result, err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return result, fmt.Errorf("active round revision not found for turn %s", delta.RoundRevision.TurnID)
		}
	}
	result.ConversationRevision = newRevision
	result.CheckpointRevision = checkpointRevision
	return result, nil
}

// ConversationRecord is an immutable-display-history message snapshot. The
// JSON payload preserves the full source message; model context is stored
// separately in ContextCheckpoint.
type ConversationRecord struct {
	ConversationID string
	ItemID         string
	Ordinal        int64
	RoundID        string
	RoundOrdinal   int64
	RoundStatus    string
	RevisionID     string
	Role           string
	Payload        llm.Message
	LegacyKind     string
	Revision       uint64
}

func ensureConversationReadIndexes(ctx context.Context, tx *sql.Tx) error {
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_conversation_rounds_order ON conversation_rounds(conversation_id,ordinal DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_round_revisions_active ON conversation_round_revisions(conversation_id,round_id,revision_id) WHERE status='active'`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_records_round_order ON conversation_records(conversation_id,round_id,revision_id,ordinal)`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_records_boundary_order ON conversation_records(conversation_id,ordinal) WHERE legacy_kind IN ('context_artifact','legacy_context_artifact')`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_records_tool_call ON conversation_records(conversation_id,round_id,revision_id,tool_call_id,ordinal) WHERE tool_call_id<>''`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func ensureConversationToolCallColumn(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(conversation_records)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "tool_call_id" {
			found = true
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = tx.ExecContext(ctx, `ALTER TABLE conversation_records ADD COLUMN tool_call_id TEXT NOT NULL DEFAULT ''`)
	return err
}

func ensureConversationCheckpointDeltaColumns(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(context_checkpoints)`)
	if err != nil {
		return err
	}
	found := make(map[string]bool, 3)
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			rows.Close()
			return err
		}
		found[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		sql  string
	}{
		{name: "checkpoint_kind", sql: `ALTER TABLE context_checkpoints ADD COLUMN checkpoint_kind TEXT NOT NULL DEFAULT 'snapshot'`},
		{name: "base_revision", sql: `ALTER TABLE context_checkpoints ADD COLUMN base_revision INTEGER NOT NULL DEFAULT 0`},
		{name: "delta_json", sql: `ALTER TABLE context_checkpoints ADD COLUMN delta_json TEXT NOT NULL DEFAULT ''`},
	} {
		if found[column.name] {
			continue
		}
		if _, err := tx.ExecContext(ctx, column.sql); err != nil {
			return err
		}
	}
	return nil
}

func backfillConversationToolCallIDs(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT conversation_id,item_id,payload_json FROM conversation_records WHERE role='tool' AND tool_call_id=''`)
	if err != nil {
		return err
	}
	type resultRow struct{ conversationID, itemID, toolCallID string }
	var updates []resultRow
	for rows.Next() {
		var conversationID, itemID, raw string
		if err := rows.Scan(&conversationID, &itemID, &raw); err != nil {
			rows.Close()
			return err
		}
		var header struct {
			ToolCallID string `json:"tool_call_id"`
		}
		if err := json.Unmarshal([]byte(raw), &header); err != nil {
			rows.Close()
			return fmt.Errorf("decode stored tool result identity: %w", err)
		}
		if id := strings.TrimSpace(header.ToolCallID); id != "" {
			updates = append(updates, resultRow{conversationID: conversationID, itemID: itemID, toolCallID: id})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE conversation_records SET tool_call_id=? WHERE conversation_id=? AND item_id=?`, row.toolCallID, row.conversationID, row.itemID); err != nil {
			return err
		}
	}
	return nil
}

func conversationToolCallID(message llm.Message) string {
	if strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
		return strings.TrimSpace(message.ToolCallID)
	}
	return ""
}

// ConversationCheckpoint is the provider-valid working set used to resume
// model execution. It is not a source for historical display.
type ConversationCheckpoint struct {
	ConversationID string
	Revision       uint64
	Messages       []llm.Message
	Lineage        string
}

// saveConversationSnapshotTx commits display records and the model checkpoint
// in the same transaction as runtime metadata and queue acknowledgement.
func saveConversationSnapshotTx(ctx context.Context, tx *sql.Tx, rec Record) error {
	var storedHistoryRevision uint64
	err := tx.QueryRowContext(ctx, `SELECT revision FROM conversations WHERE conversation_id=?`, rec.AgentID).Scan(&storedHistoryRevision)
	if err == nil && storedHistoryRevision == rec.RuntimeState.HistoryRevision {
		// HistoryRevision is the content-dirty boundary. Runtime mailbox/notify/
		// hook updates with no history change must not scan or rewrite records or
		// checkpoints. Callers that mutate messages must advance this revision.
		return nil
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	messages := append([]llm.Message(nil), rec.Messages...)
	var ordinal int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0) FROM conversation_records WHERE conversation_id=?`, rec.AgentID).Scan(&ordinal); err != nil {
		return err
	}
	for i := range messages {
		// Compression summaries are model context artifacts. They replace part
		// of the working set, but are not new historical conversation facts.
		if messages[i].Name == llm.UserNameCompression {
			if id := strings.TrimSpace(messages[i].RecordID); id != "" {
				if _, err := tx.ExecContext(ctx, `DELETE FROM conversation_records WHERE conversation_id=? AND item_id=?`, rec.AgentID, id); err != nil {
					return err
				}
			}
			continue
		}
		id := strings.TrimSpace(messages[i].RecordID)
		if id == "" {
			id = migratedMessageRecordID(messages[i])
		}
		if id == "" {
			var err error
			id, err = llm.NewOpaqueRecordID()
			if err != nil {
				return err
			}
		}
		messages[i].RecordID = id
		payload, err := json.Marshal(messages[i])
		if err != nil {
			return err
		}
		legacyKind := ""
		if llm.IsHiddenInjectedUserMessage(messages[i]) || llm.IsMessageSource(messages[i], llm.MessageSourceRuntime, llm.MessageFormSnapshot, "") {
			legacyKind = "context_artifact"
		}
		stamp := time.Now().UTC().Format(time.RFC3339Nano)
		var existing int64
		var existingPayload string
		err = tx.QueryRowContext(ctx, `SELECT ordinal,payload_json FROM conversation_records WHERE conversation_id=? AND item_id=?`, rec.AgentID, id).Scan(&existing, &existingPayload)
		if err == sql.ErrNoRows {
			if _, err = tx.ExecContext(ctx, `INSERT INTO conversation_records(conversation_id,item_id,ordinal,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?,?,?)`, rec.AgentID, id, ordinal+1, messages[i].Role, string(payload), conversationToolCallID(messages[i]), legacyKind, stamp, stamp); err == nil {
				ordinal++
			}
		} else if err == nil {
			if existingPayload != string(payload) {
				_, err = tx.ExecContext(ctx, `UPDATE conversation_records SET role=?,payload_json=?,tool_call_id=?,revision=revision+1,legacy_kind=?,updated_at=? WHERE conversation_id=? AND item_id=?`, messages[i].Role, string(payload), conversationToolCallID(messages[i]), legacyKind, stamp, rec.AgentID, id)
			}
		}
		if err != nil {
			return err
		}
	}
	start := rec.RuntimeState.ActiveContextStart
	if start < 0 || start > len(messages) {
		start = 0
	}
	checkpoint := append([]llm.Message(nil), messages[start:]...)
	cpRaw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	var current uint64
	if err := tx.QueryRowContext(ctx, `SELECT current_checkpoint_revision FROM conversations WHERE conversation_id=?`, rec.AgentID).Scan(&current); err != nil {
		if err != sql.ErrNoRows {
			return err
		}
		current = 0
	}
	cpRevision := current + 1
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversations(conversation_id,node_id,revision,current_checkpoint_revision,lifecycle_covered_seq,created_at,updated_at) VALUES(?,?,?,?,0,?,?) ON CONFLICT(conversation_id) DO UPDATE SET node_id=excluded.node_id,revision=excluded.revision,current_checkpoint_revision=excluded.current_checkpoint_revision,lifecycle_covered_seq=excluded.lifecycle_covered_seq,updated_at=excluded.updated_at`, rec.AgentID, rec.NodeID, rec.RuntimeState.HistoryRevision, cpRevision, stamp, stamp); err != nil {
		return err
	}
	lineage := "live_append"
	if start > 0 || rec.RuntimeState.LastContextResetID != "" || len(rec.RuntimeState.DreamingAttempt) > 0 {
		lineage = "derived_checkpoint"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES(?,?,?,?,0,?)`, rec.AgentID, cpRevision, string(cpRaw), lineage, stamp); err != nil {
		return err
	}
	if err := pruneConversationCheckpointsTx(ctx, tx, rec.AgentID, cpRevision); err != nil {
		return err
	}
	return nil
}

func (s *SQLiteStore) migrateConversationStorage(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS conversation_schema_meta (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var version int
	err = tx.QueryRowContext(ctx, `SELECT version FROM conversation_schema_meta WHERE id=1`).Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if version > conversationStorageVersion {
		return fmt.Errorf("conversation schema version %d is newer than supported version %d", version, conversationStorageVersion)
	}
	if version == 2 {
		if err := ensureConversationToolCallColumn(ctx, tx); err != nil {
			return err
		}
		if err := backfillConversationToolCallIDs(ctx, tx); err != nil {
			return err
		}
		if err := ensureConversationCheckpointDeltaColumns(ctx, tx); err != nil {
			return err
		}
		if err := ensureConversationReadIndexes(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_schema_meta(id,version) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version`, conversationStorageVersion); err != nil {
			return err
		}
		return tx.Commit()
	}
	if version == 3 {
		if err := ensureConversationCheckpointDeltaColumns(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_schema_meta(id,version) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version`, conversationStorageVersion); err != nil {
			return err
		}
		return tx.Commit()
	}
	if version == conversationStorageVersion {
		if err := ensureConversationCheckpointDeltaColumns(ctx, tx); err != nil {
			return err
		}
		if err := ensureConversationReadIndexes(ctx, tx); err != nil {
			return err
		}
		return tx.Commit()
	}
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS conversations (
  conversation_id TEXT PRIMARY KEY, node_id TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 0,
  current_checkpoint_revision INTEGER NOT NULL DEFAULT 0, lifecycle_covered_seq INTEGER NOT NULL DEFAULT 0,
  history_completeness TEXT NOT NULL DEFAULT 'complete', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS conversation_rounds (
  conversation_id TEXT NOT NULL, round_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
  selected_revision_id TEXT NOT NULL DEFAULT '', pre_checkpoint_revision INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(conversation_id,round_id))`,
		`CREATE TABLE IF NOT EXISTS conversation_round_revisions (
  conversation_id TEXT NOT NULL, round_id TEXT NOT NULL, revision_id TEXT NOT NULL,
  turn_id TEXT NOT NULL, source TEXT NOT NULL DEFAULT '', generation INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'active', finish_reason TEXT NOT NULL DEFAULT '',
  record_revision INTEGER NOT NULL DEFAULT 0, checkpoint_revision INTEGER NOT NULL DEFAULT 0,
  pre_checkpoint_revision INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(conversation_id,round_id,revision_id), UNIQUE(conversation_id,turn_id))`,
		`CREATE TABLE IF NOT EXISTS conversation_records (
  conversation_id TEXT NOT NULL, item_id TEXT NOT NULL, ordinal INTEGER NOT NULL,
  round_id TEXT NOT NULL DEFAULT '', revision_id TEXT NOT NULL DEFAULT '', role TEXT NOT NULL DEFAULT '', payload_json TEXT NOT NULL,
  tool_call_id TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL DEFAULT 1, legacy_kind TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL, PRIMARY KEY(conversation_id,item_id), UNIQUE(conversation_id,ordinal))`,
		`CREATE INDEX IF NOT EXISTS idx_conversation_records_order ON conversation_records(conversation_id,ordinal)`,
		`CREATE TABLE IF NOT EXISTS context_checkpoints (
  conversation_id TEXT NOT NULL, revision INTEGER NOT NULL, messages_json TEXT NOT NULL,
  lineage TEXT NOT NULL DEFAULT '', lifecycle_covered_seq INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL, checkpoint_kind TEXT NOT NULL DEFAULT 'snapshot',
  base_revision INTEGER NOT NULL DEFAULT 0, delta_json TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(conversation_id,revision))`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := ensureConversationCheckpointDeltaColumns(ctx, tx); err != nil {
		return err
	}
	if err := ensureConversationToolCallColumn(ctx, tx); err != nil {
		return err
	}
	if err := backfillConversationToolCallIDs(ctx, tx); err != nil {
		return err
	}
	if version == 1 {
		var hasRevisionID int
		rows, err := tx.QueryContext(ctx, `PRAGMA table_info(conversation_records)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == "revision_id" {
				hasRevisionID = 1
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if hasRevisionID == 0 {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE conversation_records ADD COLUMN revision_id TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
		// Keep legacy columns in place; they were never populated. Add the new
		// logical-round fields without destructive table replacement.
		var hasSelectedRevision, hasPreCheckpoint int
		rows, err = tx.QueryContext(ctx, `PRAGMA table_info(conversation_rounds)`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				rows.Close()
				return err
			}
			if name == "selected_revision_id" {
				hasSelectedRevision = 1
			}
			if name == "pre_checkpoint_revision" {
				hasPreCheckpoint = 1
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if hasSelectedRevision == 0 {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE conversation_rounds ADD COLUMN selected_revision_id TEXT NOT NULL DEFAULT ''`); err != nil {
				return err
			}
		}
		if hasPreCheckpoint == 0 {
			if _, err := tx.ExecContext(ctx, `ALTER TABLE conversation_rounds ADD COLUMN pre_checkpoint_revision INTEGER NOT NULL DEFAULT 0`); err != nil {
				return err
			}
		}
		if err := ensureConversationToolCallColumn(ctx, tx); err != nil {
			return err
		}
		if err := backfillConversationToolCallIDs(ctx, tx); err != nil {
			return err
		}
		if err := ensureConversationReadIndexes(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_schema_meta(id,version) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version`, conversationStorageVersion); err != nil {
			return err
		}
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT agent_id,node_id,messages_json,runtime_state_json,created_at,updated_at FROM agent_runtimes ORDER BY agent_id`)
	if err != nil {
		return err
	}
	type legacyRow struct {
		id, nodeID, messagesRaw, stateRaw, created, updated string
	}
	var legacy []legacyRow
	for rows.Next() {
		var row legacyRow
		if err := rows.Scan(&row.id, &row.nodeID, &row.messagesRaw, &row.stateRaw, &row.created, &row.updated); err != nil {
			rows.Close()
			return err
		}
		legacy = append(legacy, row)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, row := range legacy {
		if err := importLegacyConversationTx(ctx, tx, row); err != nil {
			return fmt.Errorf("migrate conversation %q: %w", row.id, err)
		}
	}
	if err := ensureConversationReadIndexes(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_schema_meta(id,version) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version`, conversationStorageVersion); err != nil {
		return err
	}
	return tx.Commit()
}

func importLegacyConversationTx(ctx context.Context, tx *sql.Tx, row struct {
	id, nodeID, messagesRaw, stateRaw, created, updated string
}) error {
	var messages []llm.Message
	if err := json.Unmarshal([]byte(row.messagesRaw), &messages); err != nil {
		return fmt.Errorf("decode legacy messages: %w", err)
	}
	var state RuntimeState
	if err := json.Unmarshal([]byte(row.stateRaw), &state); err != nil {
		return fmt.Errorf("decode legacy runtime state: %w", err)
	}
	created, err := parseLegacyTime(row.created)
	if err != nil {
		return err
	}
	updated, err := parseLegacyTime(row.updated)
	if err != nil {
		return err
	}
	start := state.ActiveContextStart
	if start < 0 || start > len(messages) {
		start = len(messages)
	}
	unknownLegacyCoverage := false
	for _, message := range messages {
		if message.Name == llm.UserNameCompression {
			unknownLegacyCoverage = true
			break
		}
	}
	checkpointRevision := state.ContextCheckpointRevision
	if checkpointRevision == 0 {
		checkpointRevision = 1
	}
	completeness := "complete"
	if unknownLegacyCoverage {
		completeness = "legacy_snapshot_may_be_incomplete"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversations(conversation_id,node_id,revision,current_checkpoint_revision,lifecycle_covered_seq,history_completeness,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, row.id, row.nodeID, state.HistoryRevision, checkpointRevision, 0, completeness, created, updated); err != nil {
		return err
	}
	seen := make(map[string]bool, len(messages))
	pendingToolCallSources := make(map[string][]string)
	for ordinal := range messages {
		message := &messages[ordinal]
		if strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
			callID := strings.TrimSpace(message.ToolCallID)
			sourceID := strings.TrimSpace(message.ToolCallSourceID)
			pending := pendingToolCallSources[callID]
			if sourceID == "" && len(pending) > 0 {
				sourceID = pending[0]
				pending = pending[1:]
			} else if sourceID != "" {
				for index, candidate := range pending {
					if candidate == sourceID {
						pending = append(pending[:index], pending[index+1:]...)
						break
					}
				}
			}
			pendingToolCallSources[callID] = pending
			if sourceID != "" {
				message.ToolCallSourceID = sourceID
			}
		}
		if strings.TrimSpace(message.RecordID) == "" {
			message.RecordID = migratedMessageRecordID(*message)
		}
		if strings.TrimSpace(message.RecordID) == "" {
			id, err := llm.NewOpaqueRecordID()
			if err != nil {
				return fmt.Errorf("allocate legacy record identity: %w", err)
			}
			message.RecordID = id
		}
		if seen[message.RecordID] && strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
			if sourceID := strings.TrimSpace(message.ToolCallSourceID); sourceID != "" {
				scopedID := llm.ToolResultRecordIDForExecution(message.ToolCallID, sourceID)
				if scopedID != "" && !seen[scopedID] {
					message.RecordID = scopedID
				}
			}
			if seen[message.RecordID] {
				id, err := llm.NewOpaqueRecordID()
				if err != nil {
					return fmt.Errorf("allocate duplicate legacy tool result identity: %w", err)
				}
				message.RecordID = id
			}
		}
		if seen[message.RecordID] {
			continue
		}
		seen[message.RecordID] = true
		if strings.EqualFold(strings.TrimSpace(message.Role), "assistant") {
			for _, call := range message.ToolCalls {
				callID := strings.TrimSpace(call.ID)
				if callID != "" {
					pendingToolCallSources[callID] = append(pendingToolCallSources[callID], message.RecordID)
				}
			}
		}
		payload, err := json.Marshal(message)
		if err != nil {
			return err
		}
		legacyKind := "legacy_message"
		if message.Name == llm.UserNameCompression {
			legacyKind = "legacy_summary_unknown_predecessors"
		} else if llm.IsHiddenInjectedUserMessage(*message) || llm.IsMessageSource(*message, llm.MessageSourceRuntime, llm.MessageFormSnapshot, "") {
			legacyKind = "legacy_context_artifact"
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_records(conversation_id,item_id,ordinal,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?,?,?)`, row.id, message.RecordID, ordinal+1, message.Role, string(payload), conversationToolCallID(*message), legacyKind, created, updated); err != nil {
			return err
		}
	}
	checkpointMessages := append([]llm.Message(nil), messages[start:]...)
	checkpointJSON, err := json.Marshal(checkpointMessages)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES(?,?,?,?,0,?)`, row.id, checkpointRevision, string(checkpointJSON), "legacy_snapshot", created); err != nil {
		return err
	}
	// Old compressed snapshots may no longer contain the original exchange.
	// The imported rows are marked legacy, and no lifecycle round is invented.
	return nil
}

func migratedMessageRecordID(message llm.Message) string {
	if strings.TrimSpace(message.RecordID) != "" {
		return strings.TrimSpace(message.RecordID)
	}
	if identity := message.DisplayIdentity; identity != nil && strings.TrimSpace(identity.ContentItemID) != "" {
		return strings.TrimSpace(identity.ContentItemID)
	}
	if strings.EqualFold(strings.TrimSpace(message.Role), "tool") {
		return llm.ToolResultRecordID(message.ToolCallID)
	}
	return ""
}

func parseLegacyTime(raw string) (string, error) {
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return "", fmt.Errorf("parse legacy timestamp: %w", err)
	}
	return t.UTC().Format(time.RFC3339Nano), nil
}

// LoadConversationRecordsWithRevisions returns durable display items and the
// actual per-item row revisions used by hydrate identity projections.
func (s *SQLiteStore) LoadConversationRecordsWithRevisions(ctx context.Context, conversationID string) ([]ConversationRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind FROM conversation_records WHERE conversation_id=? ORDER BY ordinal`, strings.TrimSpace(conversationID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversationRecord
	for rows.Next() {
		var item ConversationRecord
		var raw string
		if err := rows.Scan(&item.ConversationID, &item.ItemID, &item.Ordinal, &item.RoundID, &item.RevisionID, &item.Role, &raw, &item.Revision, &item.LegacyKind); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Payload); err != nil {
			return nil, err
		}
		// The indexed identity column is authoritative even for legacy payloads
		// that predate RecordID serialization.
		item.Payload.RecordID = item.ItemID
		out = append(out, item)
	}
	return out, rows.Err()
}

// LoadConversationRecords returns the durable display history in commit order.
func (s *SQLiteStore) LoadConversationRecords(ctx context.Context, conversationID string) ([]llm.Message, error) {
	items, err := s.LoadConversationRecordsWithRevisions(ctx, conversationID)
	if err != nil {
		return nil, err
	}
	out := make([]llm.Message, 0, len(items))
	for _, item := range items {
		out = append(out, item.Payload)
	}
	return out, nil
}

// LoadConversationCheckpoint returns the current model working set.
func (s *SQLiteStore) LoadConversationCheckpoint(ctx context.Context, conversationID string) (*ConversationCheckpoint, error) {
	return loadConversationCheckpointTx(ctx, s.db, conversationID)
}

// LoadConversationCheckpointRevision loads a retained execution base. The
// current working checkpoint and the latest round's pre-round checkpoint are
// retained together so a future resubmit can rebuild context without relying
// on compressed/current messages.
func (s *SQLiteStore) LoadConversationCheckpointRevision(ctx context.Context, conversationID string, revision uint64) (*ConversationCheckpoint, error) {
	return loadConversationCheckpointRevision(ctx, s.db, strings.TrimSpace(conversationID), revision)
}

func (s *SQLiteStore) LoadRoundRevisionForTurn(ctx context.Context, conversationID, turnID string) (*RoundRevision, error) {
	var revision RoundRevision
	revision.ConversationID = strings.TrimSpace(conversationID)
	err := s.db.QueryRowContext(ctx, `SELECT round_id,revision_id,turn_id,source,generation,status,finish_reason,pre_checkpoint_revision FROM conversation_round_revisions WHERE conversation_id=? AND turn_id=?`, revision.ConversationID, strings.TrimSpace(turnID)).Scan(&revision.RoundID, &revision.RevisionID, &revision.TurnID, &revision.Source, &revision.Generation, &revision.Status, &revision.FinishReason, &revision.PreCheckpointRevision)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &revision, nil
}

// LoadConversationRevisions reads only CAS metadata; it never scans records or
// checkpoint payloads. It supports embedded callers that construct runtimes
// directly instead of going through Manager restoration.
func (s *SQLiteStore) LoadConversationRevisions(ctx context.Context, conversationID string) (conversation, checkpoint uint64, found bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT revision,current_checkpoint_revision FROM conversations WHERE conversation_id=?`, strings.TrimSpace(conversationID)).Scan(&conversation, &checkpoint)
	if err == sql.ErrNoRows {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, err
	}
	return conversation, checkpoint, true, nil
}

type conversationQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadConversationCheckpointTx(ctx context.Context, db conversationQueryer, conversationID string) (*ConversationCheckpoint, error) {
	var revision uint64
	err := db.QueryRowContext(ctx, `SELECT current_checkpoint_revision FROM conversations WHERE conversation_id=?`, strings.TrimSpace(conversationID)).Scan(&revision)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return loadConversationCheckpointRevision(ctx, db, strings.TrimSpace(conversationID), revision)
}

func loadConversationCheckpointRevision(ctx context.Context, db conversationQueryer, conversationID string, revision uint64) (*ConversationCheckpoint, error) {
	type checkpointRow struct {
		revision uint64
		messages string
		lineage  string
		kind     string
		base     uint64
		delta    string
	}
	var chain []struct {
		row        checkpointRow
		operations []CheckpointOperation
	}
	current := revision
	var checkpoint *ConversationCheckpoint
	for depth := 0; depth < 256; depth++ {
		var row checkpointRow
		err := db.QueryRowContext(ctx, `SELECT revision,messages_json,lineage,checkpoint_kind,base_revision,delta_json FROM context_checkpoints WHERE conversation_id=? AND revision=?`, conversationID, current).Scan(&row.revision, &row.messages, &row.lineage, &row.kind, &row.base, &row.delta)
		if err == sql.ErrNoRows {
			if depth == 0 {
				return nil, nil
			}
			return nil, fmt.Errorf("checkpoint revision %d references missing base revision %d", revision, current)
		}
		if err != nil {
			return nil, err
		}
		if depth == 0 {
			checkpoint = &ConversationCheckpoint{ConversationID: conversationID, Revision: revision, Lineage: row.lineage}
		}
		switch row.kind {
		case "", "snapshot":
			if err := json.Unmarshal([]byte(row.messages), &checkpoint.Messages); err != nil {
				return nil, fmt.Errorf("decode checkpoint snapshot revision %d: %w", row.revision, err)
			}
			for i := len(chain) - 1; i >= 0; i-- {
				messages, err := applyCheckpointOperations(checkpoint.Messages, chain[i].operations)
				if err != nil {
					return nil, fmt.Errorf("apply checkpoint delta revision %d: %w", chain[i].row.revision, err)
				}
				checkpoint.Messages = messages
			}
			return checkpoint, nil
		case "delta":
			if row.base >= row.revision {
				return nil, fmt.Errorf("checkpoint revision %d has invalid base revision %d", row.revision, row.base)
			}
			var operations []CheckpointOperation
			if err := json.Unmarshal([]byte(row.delta), &operations); err != nil {
				return nil, fmt.Errorf("decode checkpoint delta revision %d: %w", row.revision, err)
			}
			chain = append(chain, struct {
				row        checkpointRow
				operations []CheckpointOperation
			}{row: row, operations: operations})
			current = row.base
		default:
			return nil, fmt.Errorf("checkpoint revision %d has unsupported kind %q", row.revision, row.kind)
		}
	}
	return nil, fmt.Errorf("checkpoint revision %d exceeds maximum delta depth", revision)
}

func pruneConversationCheckpointsTx(ctx context.Context, tx *sql.Tx, conversationID string, currentRevision uint64) error {
	_, err := tx.ExecContext(ctx, `WITH RECURSIVE keep(revision) AS (
  SELECT ?
  UNION
  SELECT pre_checkpoint_revision FROM conversation_rounds
    WHERE conversation_id=?
      AND ordinal=(SELECT MAX(ordinal) FROM conversation_rounds WHERE conversation_id=?)
      AND pre_checkpoint_revision>0
  UNION
  SELECT c.base_revision FROM context_checkpoints c
    JOIN keep k ON c.conversation_id=? AND c.revision=k.revision
    WHERE c.checkpoint_kind='delta' AND c.base_revision>0
)

DELETE FROM context_checkpoints WHERE conversation_id=? AND revision NOT IN (SELECT revision FROM keep)`, currentRevision, conversationID, conversationID, conversationID, conversationID)
	return err
}
