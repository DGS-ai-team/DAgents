package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestConversationMainWindowLoadsRecentCompleteRoundsAndActiveRound(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "main-window.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	seedConversationHistory(t, st, "agent-window", 8)
	if err := seedHistoryRevision(t, st, "agent-window", "round-8", "revision-old", "superseded", 89, "old response"); err != nil {
		t.Fatal(err)
	}
	if err := seedActiveWindowRound(t, st, "agent-window", 9, "round-9", "revision-9"); err != nil {
		t.Fatal(err)
	}
	legacy := llm.Message{Role: "assistant", Content: "legacy record", RecordID: "legacy-record"}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind,created_at,updated_at) VALUES('agent-window','legacy-record',1,'','','assistant',?,1,'legacy',?,?)`, string(raw), now, now); err != nil {
		t.Fatal(err)
	}

	window, err := st.LoadConversationMainWindow(ctx, "agent-window", 5, 180)
	if err != nil {
		t.Fatal(err)
	}
	if len(window.Records) != 18 {
		t.Fatalf("main window returned %d records, want 5 terminal rounds + active round (18 records)", len(window.Records))
	}
	if !window.HasEarlierHistory || window.WindowStartOrdinal != 4 || window.WindowEndOrdinal != 9 {
		t.Fatalf("unexpected window metadata: %+v", window)
	}
	for index, record := range window.Records {
		wantRoundOrdinal := int64(4 + index/3)
		if record.RoundOrdinal != wantRoundOrdinal {
			t.Fatalf("record[%d] round ordinal=%d, want %d (%s)", index, record.RoundOrdinal, wantRoundOrdinal, record.ItemID)
		}
		if record.RoundID == "round-8" && record.RevisionID != "revision-8" {
			t.Fatalf("window leaked a superseded revision: %+v", record)
		}
		if record.ItemID == "legacy-record" || record.Payload.Content == "old response" {
			t.Fatalf("window included history outside selected round window: %+v", record)
		}
	}
	if window.Records[0].RoundID != "round-4" || window.Records[len(window.Records)-1].RoundID != "round-9" {
		t.Fatalf("window round order is not chronological: first=%+v last=%+v", window.Records[0], window.Records[len(window.Records)-1])
	}
}

func TestConversationMainWindowReadHonorsContextWhileConnectionIsBusy(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "main-window-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := seedHistoryRevision(t, st, "agent-window-cancel", "round-1", "revision-1", "completed", 1, "reply"); err != nil {
		t.Fatal(err)
	}

	// SQLiteStore intentionally uses one pooled connection. A transaction that
	// holds it simulates a read queued behind persistence work; cancellation
	// must release the queued request instead of making it wait indefinitely.
	blocker, err := st.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = st.LoadConversationMainWindow(ctx, "agent-window-cancel", 5, 180)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("LoadConversationMainWindow error = %v, want context deadline exceeded", err)
	}
}

func TestConversationMainWindowUsesBoundedLegacyTailOnlyWhenNoRoundExists(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "main-window-legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	messages := make([]llm.Message, 7)
	for index := range messages {
		messages[index] = llm.Message{Role: "user", Content: fmt.Sprintf("legacy-%d", index+1), RecordID: fmt.Sprintf("legacy-%d", index+1)}
	}
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "agent-legacy-window", NodeID: "node"}, &ConversationDelta{ConversationID: "agent-legacy-window", Upserts: messages}); err != nil {
		t.Fatal(err)
	}
	window, err := st.LoadConversationMainWindow(ctx, "agent-legacy-window", 5, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(window.Records) != 3 || !window.HasEarlierHistory || window.WindowStartOrdinal != 0 || window.WindowEndOrdinal != 0 {
		t.Fatalf("unexpected bounded legacy window: %+v", window)
	}
	for index, record := range window.Records {
		if record.ItemID != fmt.Sprintf("legacy-%d", index+5) {
			t.Fatalf("legacy tail order[%d]=%s", index, record.ItemID)
		}
	}
}

func TestConversationMainWindowKeepsHistoryEntryForSupersededRevision(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "main-window-superseded.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	seedConversationHistory(t, st, "agent-window-revision", 1)
	if err := seedHistoryRevision(t, st, "agent-window-revision", "round-1", "revision-old", "superseded", 9, "old response"); err != nil {
		t.Fatal(err)
	}
	window, err := st.LoadConversationMainWindow(ctx, "agent-window-revision", 5, 180)
	if err != nil {
		t.Fatal(err)
	}
	if !window.HasEarlierHistory {
		t.Fatal("main window should keep the history entry when the only round has an older revision")
	}
	for _, record := range window.Records {
		if record.RevisionID == "revision-old" || record.Payload.Content == "old response" {
			t.Fatalf("main window included a superseded revision: %+v", record)
		}
	}
}

func TestConversationMainWindowLoadsOnlyFirstToolResultPagePerVisualGroup(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "main-window-tool-results.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	seedConversationHistory(t, st, "agent-window-tools", 1)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const toolCount = 1000
	calls := make([]llm.ToolCall, toolCount)
	for index := range calls {
		calls[index] = llm.ToolCall{
			ID:   fmt.Sprintf("window-call-%04d", index+1),
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "read_file",
				Arguments: fmt.Sprintf(`{"call_purpose":"read %d","path":"/tmp/%d"}`, index+1, index+1),
			},
		}
	}
	callMessage := llm.Message{Role: "assistant", ToolCalls: calls, RecordID: "round-1/tool-calls"}
	callRaw, err := json.Marshal(callMessage)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := st.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'assistant',?,'',1,'',?,?)`, "agent-window-tools", callMessage.RecordID, 14, "round-1", "revision-1", string(callRaw), now, now); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	largeResult := strings.Repeat("tool output ", 256)
	for index, call := range calls {
		result := llm.Message{Role: "tool", Name: "read_file", ToolCallID: call.ID, Content: largeResult, RecordID: fmt.Sprintf("round-1/result-%03d", index+1)}
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,tool_call_id,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,'tool',?,?,1,'',?,?)`, "agent-window-tools", result.RecordID, 15+int64(index), "round-1", "revision-1", string(raw), call.ID, now, now); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	window, err := st.LoadConversationMainWindow(ctx, "agent-window-tools", 5, 180)
	if err != nil {
		t.Fatal(err)
	}
	var loadedCallRecord *ConversationRecord
	loadedResults := 0
	for index := range window.Records {
		record := &window.Records[index]
		if record.ItemID == callMessage.RecordID {
			loadedCallRecord = record
		}
		if record.Role == "tool" {
			loadedResults++
			if record.Payload.ToolCallID > "window-call-0050" {
				t.Fatalf("initial window loaded tool result beyond first page: %s", record.Payload.ToolCallID)
			}
		}
	}
	if loadedCallRecord == nil || len(loadedCallRecord.Payload.ToolCalls) != len(calls) {
		t.Fatalf("initial window should retain all call summaries for overflow marker, got record=%+v", loadedCallRecord)
	}
	if loadedResults != 50 {
		t.Fatalf("initial window loaded %d tool result bodies, want only 50", loadedResults)
	}
}

func seedActiveWindowRound(t *testing.T, st *SQLiteStore, conversationID string, ordinal int64, roundID, revisionID string) error {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`INSERT INTO conversation_rounds(conversation_id,round_id,ordinal,selected_revision_id,pre_checkpoint_revision) VALUES(?,?,?,?,1)`, conversationID, roundID, ordinal, revisionID); err != nil {
		return err
	}
	if _, err := st.db.Exec(`INSERT INTO conversation_round_revisions(conversation_id,round_id,revision_id,turn_id,source,generation,status,finish_reason,record_revision,checkpoint_revision,pre_checkpoint_revision) VALUES(?,?,?,?,?,1,'active','',10,1,1)`, conversationID, roundID, revisionID, "turn-"+roundID, "human"); err != nil {
		return err
	}
	for index := 1; index <= 3; index++ {
		message := llm.Message{Role: "assistant", Content: fmt.Sprintf("active item %d", index), RecordID: fmt.Sprintf("%s/item-%d", roundID, index)}
		if index == 1 {
			message = llm.Message{Role: "user", Content: "active request", RecordID: fmt.Sprintf("%s/item-%d", roundID, index)}
		}
		raw, err := json.Marshal(message)
		if err != nil {
			return err
		}
		if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?,1,'',?,?)`, conversationID, message.RecordID, ordinal*10+int64(index), roundID, revisionID, message.Role, string(raw), now, now); err != nil {
			return err
		}
	}
	return nil
}
