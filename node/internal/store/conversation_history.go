package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

var (
	ErrConversationHistoryNotFound = errors.New("conversation history not found")
	ErrConversationHistoryCursor   = errors.New("conversation history cursor is stale")
	ErrConversationRoundNotFound   = errors.New("conversation round not found")
	ErrConversationRoundActive     = errors.New("conversation round is still active")
)

type ConversationRoundSummary struct {
	RoundID               string `json:"round_id"`
	Ordinal               int64  `json:"ordinal"`
	SelectedRevisionID    string `json:"selected_revision_id"`
	TurnID                string `json:"turn_id"`
	Source                string `json:"source"`
	Generation            uint64 `json:"generation"`
	Status                string `json:"status"`
	FinishReason          string `json:"finish_reason"`
	RecordRevision        uint64 `json:"record_revision"`
	CheckpointRevision    uint64 `json:"checkpoint_revision"`
	PreCheckpointRevision uint64 `json:"pre_checkpoint_revision"`
	ItemCount             int64  `json:"item_count"`
}

type ConversationRoundPage struct {
	AgentID              string                     `json:"agent_id"`
	ConversationID       string                     `json:"conversation_id"`
	ConversationRevision uint64                     `json:"conversation_revision"`
	HistoryCompleteness  string                     `json:"history_completeness"`
	Rounds               []ConversationRoundSummary `json:"rounds"`
	HasMore              bool                       `json:"has_more"`
	NextBeforeRoundID    string                     `json:"next_before_round_id,omitempty"`
}

type ConversationRoundItem struct {
	ItemID      string      `json:"item_id"`
	Ordinal     int64       `json:"ordinal"`
	Role        string      `json:"role"`
	ItemVersion uint64      `json:"item_version"`
	Payload     llm.Message `json:"payload"`
}

type ConversationRoundItemsPage struct {
	AgentID              string                  `json:"agent_id"`
	ConversationID       string                  `json:"conversation_id"`
	ConversationRevision uint64                  `json:"conversation_revision"`
	RoundID              string                  `json:"round_id"`
	RevisionID           string                  `json:"revision_id"`
	RevisionStatus       string                  `json:"revision_status"`
	Selected             bool                    `json:"selected"`
	Items                []ConversationRoundItem `json:"items"`
	HasMore              bool                    `json:"has_more"`
	NextAfterItemID      string                  `json:"next_after_item_id,omitempty"`
}

type ConversationRevisionSummary struct {
	RevisionID            string `json:"revision_id"`
	TurnID                string `json:"turn_id"`
	Source                string `json:"source"`
	Generation            uint64 `json:"generation"`
	Status                string `json:"status"`
	FinishReason          string `json:"finish_reason"`
	RecordRevision        uint64 `json:"record_revision"`
	CheckpointRevision    uint64 `json:"checkpoint_revision"`
	PreCheckpointRevision uint64 `json:"pre_checkpoint_revision"`
	ItemCount             int64  `json:"item_count"`
	Selected              bool   `json:"selected"`
}

type ConversationRevisionPage struct {
	ConversationID       string                        `json:"conversation_id"`
	ConversationRevision uint64                        `json:"conversation_revision"`
	RoundID              string                        `json:"round_id"`
	SelectedRevisionID   string                        `json:"selected_revision_id"`
	Revisions            []ConversationRevisionSummary `json:"revisions"`
}

// ConversationResubmitTarget is the last terminal human revision plus its
// retained pre-round checkpoint identity. The returned input is only used to
// prefill an editor; callers must send a new input through the normal queue.
type ConversationResubmitTarget struct {
	ConversationID        string
	ConversationRevision  uint64
	RoundID               string
	RevisionID            string
	PreCheckpointRevision uint64
	Input                 llm.Message
}

type ConversationLegacyItemsPage struct {
	ConversationID       string                  `json:"conversation_id"`
	ConversationRevision uint64                  `json:"conversation_revision"`
	HistoryCompleteness  string                  `json:"history_completeness"`
	ItemCount            int64                   `json:"item_count"`
	Items                []ConversationRoundItem `json:"items"`
	HasMore              bool                    `json:"has_more"`
	NextAfterItemID      string                  `json:"next_after_item_id,omitempty"`
}

// LoadConversationRoundPage reads terminal round metadata by stable round
// anchor. A new round appended after a page was opened does not invalidate the
// cursor; clear/replacement does because its anchor is removed.
func (s *SQLiteStore) LoadConversationRoundPage(ctx context.Context, conversationID, beforeRoundID string, limit int) (ConversationRoundPage, error) {
	var page ConversationRoundPage
	conversationID = strings.TrimSpace(conversationID)
	beforeRoundID = strings.TrimSpace(beforeRoundID)
	if conversationID == "" {
		return page, fmt.Errorf("conversation_id is required")
	}
	if limit < 1 || limit > 100 {
		return page, fmt.Errorf("history round limit must be between 1 and 100")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	page.ConversationID = conversationID
	if err := tx.QueryRowContext(ctx, `SELECT revision,history_completeness FROM conversations WHERE conversation_id=?`, conversationID).Scan(&page.ConversationRevision, &page.HistoryCompleteness); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return page, ErrConversationHistoryNotFound
		}
		return page, err
	}
	var rows *sql.Rows
	if beforeRoundID == "" {
		rows, err = tx.QueryContext(ctx, `SELECT r.round_id,r.ordinal,r.selected_revision_id,rr.turn_id,rr.source,rr.generation,rr.status,rr.finish_reason,rr.record_revision,rr.checkpoint_revision,rr.pre_checkpoint_revision,(SELECT COUNT(*) FROM conversation_records cr WHERE cr.conversation_id=r.conversation_id AND cr.round_id=r.round_id AND cr.revision_id=r.selected_revision_id) FROM conversation_rounds r JOIN conversation_round_revisions rr ON rr.conversation_id=r.conversation_id AND rr.round_id=r.round_id AND rr.revision_id=r.selected_revision_id WHERE r.conversation_id=? AND rr.status<>'active' ORDER BY r.ordinal DESC LIMIT ?`, conversationID, limit+1)
	} else {
		var anchorOrdinal int64
		if err := tx.QueryRowContext(ctx, `SELECT ordinal FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, conversationID, beforeRoundID).Scan(&anchorOrdinal); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return page, ErrConversationHistoryCursor
			}
			return page, err
		}
		rows, err = tx.QueryContext(ctx, `SELECT r.round_id,r.ordinal,r.selected_revision_id,rr.turn_id,rr.source,rr.generation,rr.status,rr.finish_reason,rr.record_revision,rr.checkpoint_revision,rr.pre_checkpoint_revision,(SELECT COUNT(*) FROM conversation_records cr WHERE cr.conversation_id=r.conversation_id AND cr.round_id=r.round_id AND cr.revision_id=r.selected_revision_id) FROM conversation_rounds r JOIN conversation_round_revisions rr ON rr.conversation_id=r.conversation_id AND rr.round_id=r.round_id AND rr.revision_id=r.selected_revision_id WHERE r.conversation_id=? AND r.ordinal<? AND rr.status<>'active' ORDER BY r.ordinal DESC LIMIT ?`, conversationID, anchorOrdinal, limit+1)
	}
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Rounds = make([]ConversationRoundSummary, 0, limit)
	for rows.Next() {
		var item ConversationRoundSummary
		if err := rows.Scan(&item.RoundID, &item.Ordinal, &item.SelectedRevisionID, &item.TurnID, &item.Source, &item.Generation, &item.Status, &item.FinishReason, &item.RecordRevision, &item.CheckpointRevision, &item.PreCheckpointRevision, &item.ItemCount); err != nil {
			return page, err
		}
		page.Rounds = append(page.Rounds, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Rounds) > limit {
		page.HasMore = true
		page.Rounds = page.Rounds[:limit]
		page.NextBeforeRoundID = page.Rounds[len(page.Rounds)-1].RoundID
	}
	for left, right := 0, len(page.Rounds)-1; left < right; left, right = left+1, right-1 {
		page.Rounds[left], page.Rounds[right] = page.Rounds[right], page.Rounds[left]
	}
	if page.Rounds == nil {
		page.Rounds = []ConversationRoundSummary{}
	}
	if err := tx.Commit(); err != nil {
		return ConversationRoundPage{}, err
	}
	return page, nil
}

// LoadConversationRoundItems returns one bounded page from a terminal round
// revision. Payloads remain revision-scoped so old executions cannot be mixed
// with the currently selected revision.
func (s *SQLiteStore) LoadConversationRoundItems(ctx context.Context, conversationID, roundID, revisionID, afterItemID string, limit int) (ConversationRoundItemsPage, error) {
	var page ConversationRoundItemsPage
	conversationID, roundID = strings.TrimSpace(conversationID), strings.TrimSpace(roundID)
	revisionID, afterItemID = strings.TrimSpace(revisionID), strings.TrimSpace(afterItemID)
	if conversationID == "" || roundID == "" {
		return page, fmt.Errorf("conversation_id and round_id are required")
	}
	if limit < 1 || limit > 200 {
		return page, fmt.Errorf("history item limit must be between 1 and 200")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	page.ConversationID, page.RoundID = conversationID, roundID
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM conversations WHERE conversation_id=?`, conversationID).Scan(&page.ConversationRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return page, ErrConversationHistoryNotFound
		}
		return page, err
	}
	var selectedRevision string
	if err := tx.QueryRowContext(ctx, `SELECT selected_revision_id FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, conversationID, roundID).Scan(&selectedRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return page, ErrConversationRoundNotFound
		}
		return page, err
	}
	if revisionID == "" {
		revisionID = selectedRevision
	}
	page.RevisionID = revisionID
	var revisionStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, conversationID, roundID, revisionID).Scan(&revisionStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return page, ErrConversationRoundNotFound
		}
		return page, err
	}
	if revisionStatus == "active" {
		return page, ErrConversationRoundActive
	}
	page.RevisionStatus = revisionStatus
	page.Selected = revisionID == selectedRevision
	var rows *sql.Rows
	if afterItemID == "" {
		rows, err = tx.QueryContext(ctx, `SELECT item_id,ordinal,role,payload_json,revision FROM conversation_records WHERE conversation_id=? AND round_id=? AND revision_id=? ORDER BY ordinal LIMIT ?`, conversationID, roundID, revisionID, limit+1)
	} else {
		var anchorOrdinal int64
		if err := tx.QueryRowContext(ctx, `SELECT ordinal FROM conversation_records WHERE conversation_id=? AND item_id=? AND round_id=? AND revision_id=?`, conversationID, afterItemID, roundID, revisionID).Scan(&anchorOrdinal); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return page, ErrConversationHistoryCursor
			}
			return page, err
		}
		rows, err = tx.QueryContext(ctx, `SELECT item_id,ordinal,role,payload_json,revision FROM conversation_records WHERE conversation_id=? AND round_id=? AND revision_id=? AND ordinal>? ORDER BY ordinal LIMIT ?`, conversationID, roundID, revisionID, anchorOrdinal, limit+1)
	}
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]ConversationRoundItem, 0, limit)
	for rows.Next() {
		var item ConversationRoundItem
		var raw string
		if err := rows.Scan(&item.ItemID, &item.Ordinal, &item.Role, &raw, &item.ItemVersion); err != nil {
			return page, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Payload); err != nil {
			return page, err
		}
		item.Payload.RecordID = item.ItemID
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
		page.NextAfterItemID = page.Items[len(page.Items)-1].ItemID
	}
	if page.Items == nil {
		page.Items = []ConversationRoundItem{}
	}
	if err := tx.Commit(); err != nil {
		return ConversationRoundItemsPage{}, err
	}
	return page, nil
}

// LoadConversationRoundRevisions lists immutable terminal revisions for a
// logical round. It does not change which revision is selected for the live
// conversation; callers can use the IDs to inspect old transcripts read-only.
func (s *SQLiteStore) LoadConversationRoundRevisions(ctx context.Context, conversationID, roundID string) (ConversationRevisionPage, error) {
	var page ConversationRevisionPage
	conversationID, roundID = strings.TrimSpace(conversationID), strings.TrimSpace(roundID)
	if conversationID == "" || roundID == "" {
		return page, fmt.Errorf("conversation_id and round_id are required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	page.ConversationID, page.RoundID = conversationID, roundID
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM conversations WHERE conversation_id=?`, conversationID).Scan(&page.ConversationRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return page, ErrConversationHistoryNotFound
		}
		return page, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT selected_revision_id FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, conversationID, roundID).Scan(&page.SelectedRevisionID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return page, ErrConversationRoundNotFound
		}
		return page, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT rr.revision_id,rr.turn_id,rr.source,rr.generation,rr.status,rr.finish_reason,rr.record_revision,rr.checkpoint_revision,rr.pre_checkpoint_revision,(SELECT COUNT(*) FROM conversation_records cr WHERE cr.conversation_id=rr.conversation_id AND cr.round_id=rr.round_id AND cr.revision_id=rr.revision_id) FROM conversation_round_revisions rr WHERE rr.conversation_id=? AND rr.round_id=? AND rr.status<>'active' ORDER BY rr.generation,rr.revision_id`, conversationID, roundID)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Revisions = make([]ConversationRevisionSummary, 0)
	for rows.Next() {
		var item ConversationRevisionSummary
		if err := rows.Scan(&item.RevisionID, &item.TurnID, &item.Source, &item.Generation, &item.Status, &item.FinishReason, &item.RecordRevision, &item.CheckpointRevision, &item.PreCheckpointRevision, &item.ItemCount); err != nil {
			return page, err
		}
		item.Selected = item.RevisionID == page.SelectedRevisionID
		page.Revisions = append(page.Revisions, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Revisions) == 0 {
		return ConversationRevisionPage{}, ErrConversationRoundActive
	}
	if err := tx.Commit(); err != nil {
		return ConversationRevisionPage{}, err
	}
	return page, nil
}

// LoadConversationResubmitTarget returns the editable input only when the
// requested revision is the selected terminal revision of the latest human
// round and its pre-round checkpoint is still retained.
func (s *SQLiteStore) LoadConversationResubmitTarget(ctx context.Context, conversationID, roundID, revisionID string) (ConversationResubmitTarget, error) {
	var target ConversationResubmitTarget
	conversationID, roundID, revisionID = strings.TrimSpace(conversationID), strings.TrimSpace(roundID), strings.TrimSpace(revisionID)
	if conversationID == "" || roundID == "" || revisionID == "" {
		return target, fmt.Errorf("conversation_id, round_id, and revision_id are required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return target, err
	}
	defer func() { _ = tx.Rollback() }()
	target.ConversationID, target.RoundID, target.RevisionID = conversationID, roundID, revisionID
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM conversations WHERE conversation_id=?`, conversationID).Scan(&target.ConversationRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return target, ErrConversationHistoryNotFound
		}
		return target, err
	}
	var selectedRevision string
	var ordinal int64
	var roundPreCheckpointRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT selected_revision_id,ordinal,pre_checkpoint_revision FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, conversationID, roundID).Scan(&selectedRevision, &ordinal, &roundPreCheckpointRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return target, ErrConversationRoundNotFound
		}
		return target, err
	}
	var latestOrdinal int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ordinal),0) FROM conversation_rounds WHERE conversation_id=?`, conversationID).Scan(&latestOrdinal); err != nil {
		return target, err
	}
	if ordinal == 0 || ordinal != latestOrdinal || selectedRevision != revisionID {
		return target, ErrConversationResubmitConflict
	}
	var source, status string
	var revisionPreCheckpointRevision uint64
	if err := tx.QueryRowContext(ctx, `SELECT source,status,pre_checkpoint_revision FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, conversationID, roundID, revisionID).Scan(&source, &status, &revisionPreCheckpointRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return target, ErrConversationRoundNotFound
		}
		return target, err
	}
	if source != "human" || status == "active" || revisionPreCheckpointRevision != roundPreCheckpointRevision {
		return target, ErrConversationResubmitConflict
	}
	target.PreCheckpointRevision = roundPreCheckpointRevision
	var checkpointExists int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM context_checkpoints WHERE conversation_id=? AND revision=?)`, conversationID, target.PreCheckpointRevision).Scan(&checkpointExists); err != nil {
		return target, err
	}
	if checkpointExists == 0 {
		return target, ErrConversationResubmitConflict
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM conversation_records WHERE conversation_id=? AND round_id=? AND revision_id=? AND role='user' ORDER BY ordinal DESC`, conversationID, roundID, revisionID)
	if err != nil {
		return target, err
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var raw string
		var message llm.Message
		if err := rows.Scan(&raw); err != nil {
			return target, err
		}
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			return target, err
		}
		if llm.IsHiddenInjectedUserMessage(message) || message.Name == llm.UserNameCompression {
			continue
		}
		target.Input = message
		found = true
		break
	}
	if err := rows.Err(); err != nil {
		return target, err
	}
	if !found {
		return target, ErrConversationResubmitConflict
	}
	if err := tx.Commit(); err != nil {
		return ConversationResubmitTarget{}, err
	}
	return target, nil
}

// LoadConversationLegacyItems exposes records written before round ownership
// was introduced. They remain readable as a chronological transcript, but are
// deliberately not grouped into guessed turns.
func (s *SQLiteStore) LoadConversationLegacyItems(ctx context.Context, conversationID, afterItemID string, limit int) (ConversationLegacyItemsPage, error) {
	var page ConversationLegacyItemsPage
	conversationID, afterItemID = strings.TrimSpace(conversationID), strings.TrimSpace(afterItemID)
	if conversationID == "" {
		return page, fmt.Errorf("conversation_id is required")
	}
	if limit < 1 || limit > 200 {
		return page, fmt.Errorf("legacy history item limit must be between 1 and 200")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	page.ConversationID = conversationID
	if err := tx.QueryRowContext(ctx, `SELECT revision,history_completeness FROM conversations WHERE conversation_id=?`, conversationID).Scan(&page.ConversationRevision, &page.HistoryCompleteness); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return page, ErrConversationHistoryNotFound
		}
		return page, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversation_records WHERE conversation_id=? AND round_id='' AND revision_id=''`, conversationID).Scan(&page.ItemCount); err != nil {
		return page, err
	}
	var rows *sql.Rows
	if afterItemID == "" {
		rows, err = tx.QueryContext(ctx, `SELECT item_id,ordinal,role,payload_json,revision FROM conversation_records WHERE conversation_id=? AND round_id='' AND revision_id='' ORDER BY ordinal LIMIT ?`, conversationID, limit+1)
	} else {
		var anchorOrdinal int64
		if err := tx.QueryRowContext(ctx, `SELECT ordinal FROM conversation_records WHERE conversation_id=? AND item_id=? AND round_id='' AND revision_id=''`, conversationID, afterItemID).Scan(&anchorOrdinal); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return page, ErrConversationHistoryCursor
			}
			return page, err
		}
		rows, err = tx.QueryContext(ctx, `SELECT item_id,ordinal,role,payload_json,revision FROM conversation_records WHERE conversation_id=? AND round_id='' AND revision_id='' AND ordinal>? ORDER BY ordinal LIMIT ?`, conversationID, anchorOrdinal, limit+1)
	}
	if err != nil {
		return page, err
	}
	defer rows.Close()
	page.Items = make([]ConversationRoundItem, 0, limit)
	for rows.Next() {
		var item ConversationRoundItem
		var raw string
		if err := rows.Scan(&item.ItemID, &item.Ordinal, &item.Role, &raw, &item.ItemVersion); err != nil {
			return page, err
		}
		if err := json.Unmarshal([]byte(raw), &item.Payload); err != nil {
			return page, err
		}
		item.Payload.RecordID = item.ItemID
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.HasMore = true
		page.Items = page.Items[:limit]
		page.NextAfterItemID = page.Items[len(page.Items)-1].ItemID
	}
	if page.Items == nil {
		page.Items = []ConversationRoundItem{}
	}
	if err := tx.Commit(); err != nil {
		return ConversationLegacyItemsPage{}, err
	}
	return page, nil
}
