package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

// ConversationMainWindow is the bounded durable slice used to open or
// reconcile the main chat. Older rounds remain available through the history
// APIs and are never decoded just to build this window.
type ConversationMainWindow struct {
	ConversationID       string
	ConversationRevision uint64
	HistoryCompleteness  string
	Records              []ConversationRecord
	HasEarlierHistory    bool
	WindowStartOrdinal   int64
	WindowEndOrdinal     int64
}

type conversationWindowRound struct {
	RoundID        string
	RevisionID     string
	Ordinal        int64
	RevisionStatus string
}

// LoadConversationMainWindow reads the selected revision of the newest
// terminal rounds and every selected active round. If a conversation has no
// round metadata yet, it falls back to a bounded legacy tail without claiming
// that those records form complete rounds.
func (s *SQLiteStore) LoadConversationMainWindow(ctx context.Context, conversationID string, terminalRoundLimit, legacyItemLimit int) (ConversationMainWindow, error) {
	var window ConversationMainWindow
	conversationID = strings.TrimSpace(conversationID)
	if conversationID == "" {
		return window, fmt.Errorf("conversation_id is required")
	}
	if terminalRoundLimit < 1 || terminalRoundLimit > 100 {
		return window, fmt.Errorf("terminal round limit must be between 1 and 100")
	}
	if legacyItemLimit < 1 || legacyItemLimit > 1000 {
		return window, fmt.Errorf("legacy item limit must be between 1 and 1000")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return window, err
	}
	defer func() { _ = tx.Rollback() }()
	window.ConversationID = conversationID
	if err := tx.QueryRowContext(ctx, `SELECT revision,history_completeness FROM conversations WHERE conversation_id=?`, conversationID).Scan(&window.ConversationRevision, &window.HistoryCompleteness); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// A newly created runtime can publish lifecycle state before its first
			// transcript transaction. Its main-chat window is empty until that
			// transaction commits; history APIs may still report not-found.
			return window, nil
		}
		return window, err
	}
	activeRounds, err := loadConversationWindowRounds(ctx, tx, conversationID, true, 0)
	if err != nil {
		return window, err
	}
	terminalRounds, err := loadConversationWindowRounds(ctx, tx, conversationID, false, terminalRoundLimit+1)
	if err != nil {
		return window, err
	}
	if len(terminalRounds) > terminalRoundLimit {
		window.HasEarlierHistory = true
		terminalRounds = terminalRounds[:terminalRoundLimit]
	}
	rounds := append(activeRounds, terminalRounds...)
	if len(rounds) == 0 {
		window.Records, err = loadConversationLegacyTailTx(ctx, tx, conversationID, legacyItemLimit+1)
		if err != nil {
			return window, err
		}
		if len(window.Records) > legacyItemLimit {
			window.HasEarlierHistory = true
			window.Records = window.Records[len(window.Records)-legacyItemLimit:]
		}
	} else {
		if !window.HasEarlierHistory {
			legacyHistory, historyErr := conversationHasLegacyRecordsTx(ctx, tx, conversationID)
			if historyErr != nil {
				return window, historyErr
			}
			window.HasEarlierHistory = legacyHistory
			for _, round := range rounds {
				if window.HasEarlierHistory {
					break
				}
				superseded, historyErr := conversationRoundHasSupersededRevisionTx(ctx, tx, conversationID, round)
				if historyErr != nil {
					return window, historyErr
				}
				window.HasEarlierHistory = superseded
			}
		}
		for _, round := range rounds {
			records, loadErr := loadConversationRoundWindowTx(ctx, tx, conversationID, round)
			if loadErr != nil {
				return window, loadErr
			}
			window.Records = append(window.Records, records...)
			if window.WindowStartOrdinal == 0 || round.Ordinal < window.WindowStartOrdinal {
				window.WindowStartOrdinal = round.Ordinal
			}
			if round.Ordinal > window.WindowEndOrdinal {
				window.WindowEndOrdinal = round.Ordinal
			}
		}
		if len(window.Records) > 0 {
			lower, upper := window.Records[0].Ordinal, window.Records[0].Ordinal
			for _, record := range window.Records[1:] {
				if record.Ordinal < lower {
					lower = record.Ordinal
				}
				if record.Ordinal > upper {
					upper = record.Ordinal
				}
			}
			boundaries, loadErr := loadConversationBoundaryArtifactsTx(ctx, tx, conversationID, lower, upper)
			if loadErr != nil {
				return window, loadErr
			}
			window.Records = append(window.Records, boundaries...)
		}
	}
	if window.Records == nil {
		window.Records = []ConversationRecord{}
	}
	sort.Slice(window.Records, func(i, j int) bool { return window.Records[i].Ordinal < window.Records[j].Ordinal })
	if err := tx.Commit(); err != nil {
		return ConversationMainWindow{}, err
	}
	return window, nil
}

func conversationHasLegacyRecordsTx(ctx context.Context, tx *sql.Tx, conversationID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_records WHERE conversation_id=? AND round_id='')`, conversationID).Scan(&exists)
	return exists, err
}

func conversationRoundHasSupersededRevisionTx(ctx context.Context, tx *sql.Tx, conversationID string, round conversationWindowRound) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND status<>'active' AND revision_id<>?)`, conversationID, round.RoundID, round.RevisionID).Scan(&exists)
	return exists, err
}

func loadConversationBoundaryArtifactsTx(ctx context.Context, tx *sql.Tx, conversationID string, lower, upper int64) ([]ConversationRecord, error) {
	rows, err := tx.QueryContext(ctx, `SELECT conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind
FROM conversation_records
WHERE conversation_id=? AND legacy_kind IN ('context_artifact','legacy_context_artifact') AND ordinal<=?
AND (ordinal>=? OR ordinal=(SELECT MAX(ordinal) FROM conversation_records WHERE conversation_id=? AND legacy_kind IN ('context_artifact','legacy_context_artifact') AND ordinal<?))
ORDER BY ordinal`, conversationID, upper, lower, conversationID, lower)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ConversationRecord
	for rows.Next() {
		var record ConversationRecord
		var raw string
		if err := rows.Scan(&record.ConversationID, &record.ItemID, &record.Ordinal, &record.RoundID, &record.RevisionID, &record.Role, &raw, &record.Revision, &record.LegacyKind); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &record.Payload); err != nil {
			return nil, err
		}
		// Only the explicit boundary marker is part of the chat's visible
		// recent window. Other hidden context artifacts stay internal.
		if record.Payload.Name != llm.UserNameConversationBoundary {
			continue
		}
		record.Payload.RecordID = record.ItemID
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func loadConversationWindowRounds(ctx context.Context, tx *sql.Tx, conversationID string, active bool, limit int) ([]conversationWindowRound, error) {
	query := `SELECT r.round_id,r.selected_revision_id,r.ordinal,rr.status
FROM conversation_rounds r
JOIN conversation_round_revisions rr ON rr.conversation_id=r.conversation_id AND rr.round_id=r.round_id AND rr.revision_id=r.selected_revision_id
WHERE r.conversation_id=? AND rr.status<>'active' ORDER BY r.ordinal DESC`
	args := []any{conversationID}
	if active {
		query = `SELECT r.round_id,r.selected_revision_id,r.ordinal,rr.status
FROM conversation_round_revisions rr
JOIN conversation_rounds r ON r.conversation_id=rr.conversation_id AND r.round_id=rr.round_id AND r.selected_revision_id=rr.revision_id
WHERE rr.conversation_id=? AND rr.status='active'`
	}
	if !active {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rounds []conversationWindowRound
	for rows.Next() {
		var round conversationWindowRound
		if err := rows.Scan(&round.RoundID, &round.RevisionID, &round.Ordinal, &round.RevisionStatus); err != nil {
			return nil, err
		}
		rounds = append(rounds, round)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rounds, nil
}

func loadConversationRoundWindowTx(ctx context.Context, tx *sql.Tx, conversationID string, round conversationWindowRound) ([]ConversationRecord, error) {
	// Tool results can be much larger than the visible transcript. Decode the
	// assistant/user sequence first, then fetch only results corresponding to
	// the first page of visible tool calls in each uninterrupted visual group.
	// Full results remain available through the history detail endpoints.
	rows, err := tx.QueryContext(ctx, `SELECT conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind FROM conversation_records WHERE conversation_id=? AND round_id=? AND revision_id=? AND role<>'tool' ORDER BY ordinal`, conversationID, round.RoundID, round.RevisionID)
	if err != nil {
		return nil, err
	}
	var records []ConversationRecord
	for rows.Next() {
		var record ConversationRecord
		var raw string
		if err := rows.Scan(&record.ConversationID, &record.ItemID, &record.Ordinal, &record.RoundID, &record.RevisionID, &record.Role, &raw, &record.Revision, &record.LegacyKind); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &record.Payload); err != nil {
			return nil, err
		}
		record.Payload.RecordID = record.ItemID
		record.RoundOrdinal = round.Ordinal
		record.RoundStatus = round.RevisionStatus
		records = append(records, record)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	toolCallIDs := initialVisibleToolCallIDs(records)
	toolRecords, err := loadConversationToolResultRecordsTx(ctx, tx, conversationID, round, toolCallIDs)
	if err != nil {
		return nil, err
	}
	return append(records, toolRecords...), nil
}

const initialToolResultCallsPerGroup = 50

// initialVisibleToolCallIDs mirrors the transcript's grouping rules without
// decoding tool-result payloads: hidden injected users and reasoning are
// transparent, visible user/content entries split groups, and ask-user calls
// are not displayed as tool rows.
func initialVisibleToolCallIDs(records []ConversationRecord) []string {
	var ids []string
	seen := make(map[string]struct{})
	groupCount := 0
	for _, record := range records {
		message := record.Payload
		switch strings.TrimSpace(message.Role) {
		case "user":
			if !isConversationBoundaryNoticeForStore(message) && llm.IsHiddenInjectedUserMessage(message) {
				continue
			}
			groupCount = 0
		case "assistant":
			if strings.TrimSpace(message.Content) != "" {
				groupCount = 0
			}
			for _, call := range message.ToolCalls {
				if tools.IsAskUserInformation(call.Function.Name) {
					continue
				}
				id := strings.TrimSpace(call.ID)
				if id == "" {
					continue
				}
				if groupCount < initialToolResultCallsPerGroup {
					if _, exists := seen[id]; !exists {
						ids = append(ids, id)
						seen[id] = struct{}{}
					}
				}
				groupCount++
			}
		}
	}
	return ids
}

func isConversationBoundaryNoticeForStore(message llm.Message) bool {
	return llm.IsMessageSource(message, llm.MessageSourceRuntime, llm.MessageFormNotice, "idle_context") &&
		strings.TrimSpace(message.Name) == llm.UserNameConversationBoundary &&
		strings.EqualFold(strings.TrimSpace(llm.EffectiveMessageProvenance(message).Operation), "continue-from-archive")
}

func loadConversationToolResultRecordsTx(ctx context.Context, tx *sql.Tx, conversationID string, round conversationWindowRound, callIDs []string) ([]ConversationRecord, error) {
	if len(callIDs) == 0 {
		return nil, nil
	}
	const queryChunkSize = 400 // stay comfortably below SQLite's bind-variable limit
	var records []ConversationRecord
	for start := 0; start < len(callIDs); start += queryChunkSize {
		end := start + queryChunkSize
		if end > len(callIDs) {
			end = len(callIDs)
		}
		chunk := callIDs[start:end]
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")
		args := make([]any, 0, 3+len(chunk))
		args = append(args, conversationID, round.RoundID, round.RevisionID)
		for _, id := range chunk {
			args = append(args, id)
		}
		rows, err := tx.QueryContext(ctx, `SELECT conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind FROM conversation_records WHERE conversation_id=? AND round_id=? AND revision_id=? AND role='tool' AND tool_call_id IN (`+placeholders+`) ORDER BY ordinal`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var record ConversationRecord
			var raw string
			if err := rows.Scan(&record.ConversationID, &record.ItemID, &record.Ordinal, &record.RoundID, &record.RevisionID, &record.Role, &raw, &record.Revision, &record.LegacyKind); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if err := json.Unmarshal([]byte(raw), &record.Payload); err != nil {
				_ = rows.Close()
				return nil, err
			}
			record.Payload.RecordID = record.ItemID
			record.RoundOrdinal = round.Ordinal
			record.RoundStatus = round.RevisionStatus
			records = append(records, record)
		}
		rowsErr := rows.Err()
		closeErr := rows.Close()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	return records, nil
}

func loadConversationLegacyTailTx(ctx context.Context, tx *sql.Tx, conversationID string, limit int) ([]ConversationRecord, error) {
	rows, err := tx.QueryContext(ctx, `SELECT conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind FROM conversation_records WHERE conversation_id=? AND round_id='' ORDER BY ordinal DESC LIMIT ?`, conversationID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ConversationRecord
	for rows.Next() {
		var record ConversationRecord
		var raw string
		if err := rows.Scan(&record.ConversationID, &record.ItemID, &record.Ordinal, &record.RoundID, &record.RevisionID, &record.Role, &raw, &record.Revision, &record.LegacyKind); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &record.Payload); err != nil {
			return nil, err
		}
		record.Payload.RecordID = record.ItemID
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
	return records, nil
}
