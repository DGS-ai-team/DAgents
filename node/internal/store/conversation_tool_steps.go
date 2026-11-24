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

// ConversationToolStep joins one model tool call with its result. The result
// is nil while an active tool is still running or waiting for approval.
type ConversationToolStep struct {
	ToolCallID   string
	SourceItemID string
	Call         llm.ToolCall
	Result       *llm.Message
}

type ConversationToolStepsPage struct {
	ConversationID       string
	ConversationRevision uint64
	RoundID              string
	RevisionID           string
	RevisionStatus       string
	Selected             bool
	SourceItemID         string
	TotalCount           int
	Steps                []ConversationToolStep
	HasMore              bool
	NextAfterToolCallID  string
}

func (s *SQLiteStore) LoadConversationItemContent(ctx context.Context, conversationID, roundID, revisionID, itemID, part string) (string, uint64, bool, error) {
	conversationID, roundID = strings.TrimSpace(conversationID), strings.TrimSpace(roundID)
	revisionID, itemID, part = strings.TrimSpace(revisionID), strings.TrimSpace(itemID), strings.TrimSpace(part)
	if conversationID == "" || roundID == "" || itemID == "" {
		return "", 0, false, fmt.Errorf("conversation_id, round_id and item_id are required")
	}
	if part == "" {
		part = "content"
	}
	if part != "content" && part != "reasoning" {
		return "", 0, false, fmt.Errorf("part must be content or reasoning")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return "", 0, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var selectedRevision string
	if err := tx.QueryRowContext(ctx, `SELECT selected_revision_id FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, conversationID, roundID).Scan(&selectedRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, false, ErrConversationRoundNotFound
		}
		return "", 0, false, err
	}
	if revisionID == "" {
		revisionID = selectedRevision
	}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, conversationID, roundID, revisionID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, false, ErrConversationRoundNotFound
		}
		return "", 0, false, err
	}
	var raw string
	var version uint64
	if err := tx.QueryRowContext(ctx, `SELECT payload_json,revision FROM conversation_records WHERE conversation_id=? AND item_id=? AND round_id=? AND revision_id=?`, conversationID, itemID, roundID, revisionID).Scan(&raw, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, false, ErrConversationRoundNotFound
		}
		return "", 0, false, err
	}
	var header struct {
		Content          string `json:"content"`
		ReasoningContent string `json:"reasoning_content"`
	}
	if err := json.Unmarshal([]byte(raw), &header); err != nil {
		return "", 0, false, err
	}
	text := header.Content
	if part == "reasoning" {
		text = header.ReasoningContent
	}
	if err := tx.Commit(); err != nil {
		return "", 0, false, err
	}
	return text, version, revisionID == selectedRevision && status != "active", nil
}

// LoadConversationToolSteps pages calls within one assistant record. Results
// are fetched through the indexed tool_call_id column, so loading the next
// page does not deserialize the whole result history.
func (s *SQLiteStore) LoadConversationToolSteps(ctx context.Context, conversationID, roundID, revisionID, sourceItemID, afterToolCallID string, limit int) (ConversationToolStepsPage, error) {
	var page ConversationToolStepsPage
	if limit < 1 || limit > 50 {
		return page, fmt.Errorf("tool step limit must be between 1 and 50")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	message, calls, err := loadConversationToolCallRecordTx(ctx, tx, conversationID, roundID, revisionID, sourceItemID, &page)
	if err != nil {
		return page, err
	}
	page.SourceItemID = message.RecordID
	page.TotalCount = len(calls)
	start := 0
	if afterToolCallID != "" {
		found := false
		for index, call := range calls {
			if call.ID == afterToolCallID {
				start, found = index+1, true
				break
			}
		}
		if !found {
			return page, ErrConversationHistoryCursor
		}
	}
	end := start + limit
	if end > len(calls) {
		end = len(calls)
	}
	selectedCalls := calls[start:end]
	page.Steps, err = loadConversationToolResultsTx(ctx, tx, conversationID, roundID, page.RevisionID, message.RecordID, selectedCalls)
	if err != nil {
		return page, err
	}
	if end < len(calls) && end > start {
		page.HasMore = true
		page.NextAfterToolCallID = selectedCalls[len(selectedCalls)-1].ID
	}
	if page.Steps == nil {
		page.Steps = []ConversationToolStep{}
	}
	if err := tx.Commit(); err != nil {
		return ConversationToolStepsPage{}, err
	}
	return page, nil
}

// LoadConversationToolStep reads one complete call/result pair on demand.
// Unlike the summary page, it intentionally returns full argument and result
// bodies, but only for the requested call.
func (s *SQLiteStore) LoadConversationToolStep(ctx context.Context, conversationID, roundID, revisionID, sourceItemID, toolCallID string) (ConversationToolStepsPage, error) {
	var page ConversationToolStepsPage
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer func() { _ = tx.Rollback() }()
	message, calls, err := loadConversationToolCallRecordTx(ctx, tx, conversationID, roundID, revisionID, sourceItemID, &page)
	if err != nil {
		return page, err
	}
	var selected *llm.ToolCall
	for index := range calls {
		if calls[index].ID == toolCallID {
			selected = &calls[index]
			break
		}
	}
	if selected == nil {
		return page, ErrConversationRoundNotFound
	}
	page.SourceItemID = message.RecordID
	page.TotalCount = len(calls)
	page.Steps, err = loadConversationToolResultsTx(ctx, tx, conversationID, roundID, page.RevisionID, message.RecordID, []llm.ToolCall{*selected})
	if err != nil {
		return page, err
	}
	if err := tx.Commit(); err != nil {
		return ConversationToolStepsPage{}, err
	}
	return page, nil
}

func loadConversationToolCallRecordTx(ctx context.Context, tx *sql.Tx, conversationID, roundID, revisionID, sourceItemID string, page *ConversationToolStepsPage) (llm.Message, []llm.ToolCall, error) {
	conversationID, roundID, sourceItemID = strings.TrimSpace(conversationID), strings.TrimSpace(roundID), strings.TrimSpace(sourceItemID)
	revisionID = strings.TrimSpace(revisionID)
	if conversationID == "" || roundID == "" || sourceItemID == "" {
		return llm.Message{}, nil, fmt.Errorf("conversation_id, round_id and source_item_id are required")
	}
	page.ConversationID, page.RoundID = conversationID, roundID
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM conversations WHERE conversation_id=?`, conversationID).Scan(&page.ConversationRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return llm.Message{}, nil, ErrConversationHistoryNotFound
		}
		return llm.Message{}, nil, err
	}
	var selectedRevision string
	if err := tx.QueryRowContext(ctx, `SELECT selected_revision_id FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, conversationID, roundID).Scan(&selectedRevision); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return llm.Message{}, nil, ErrConversationRoundNotFound
		}
		return llm.Message{}, nil, err
	}
	if revisionID == "" {
		revisionID = selectedRevision
	}
	page.RevisionID, page.Selected = revisionID, revisionID == selectedRevision
	if err := tx.QueryRowContext(ctx, `SELECT status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, conversationID, roundID, revisionID).Scan(&page.RevisionStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return llm.Message{}, nil, ErrConversationRoundNotFound
		}
		return llm.Message{}, nil, err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT payload_json FROM conversation_records WHERE conversation_id=? AND item_id=? AND round_id=? AND revision_id=? AND role='assistant'`, conversationID, sourceItemID, roundID, revisionID).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return llm.Message{}, nil, ErrConversationRoundNotFound
		}
		return llm.Message{}, nil, err
	}
	var message llm.Message
	if err := json.Unmarshal([]byte(raw), &message); err != nil {
		return llm.Message{}, nil, err
	}
	message.RecordID = sourceItemID
	if len(message.ToolCalls) == 0 {
		return llm.Message{}, nil, ErrConversationRoundNotFound
	}
	return message, message.ToolCalls, nil
}

func loadConversationToolResultsTx(ctx context.Context, tx *sql.Tx, conversationID, roundID, revisionID, sourceItemID string, calls []llm.ToolCall) ([]ConversationToolStep, error) {
	steps := make([]ConversationToolStep, 0, len(calls))
	callIDs := make(map[string]int, len(calls))
	for _, call := range calls {
		if strings.TrimSpace(call.ID) == "" {
			continue
		}
		callIDs[call.ID] = len(steps)
		steps = append(steps, ConversationToolStep{ToolCallID: call.ID, SourceItemID: sourceItemID, Call: call})
	}
	if len(steps) == 0 {
		return steps, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(callIDs)), ",")
	args := make([]any, 0, 3+len(callIDs))
	args = append(args, conversationID, roundID, revisionID)
	for id := range callIDs {
		args = append(args, id)
	}
	rows, err := tx.QueryContext(ctx, `SELECT item_id,payload_json FROM conversation_records WHERE conversation_id=? AND round_id=? AND revision_id=? AND role='tool' AND tool_call_id IN (`+placeholders+`) ORDER BY ordinal`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID, raw string
		if err := rows.Scan(&itemID, &raw); err != nil {
			return nil, err
		}
		var message llm.Message
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			return nil, err
		}
		index, ok := callIDs[strings.TrimSpace(message.ToolCallID)]
		if !ok {
			continue
		}
		message.RecordID = itemID
		steps[index].Result = &message
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return steps, nil
}
