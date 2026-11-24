package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestConversationRoundHistoryUsesStableAnchorsAndSelectedRevision(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	seedConversationHistory(t, st, "agent-history", 4)
	if err := seedHistoryRevision(t, st, "agent-history", "round-2", "revision-old", "superseded", 20, "older response"); err != nil {
		t.Fatal(err)
	}

	page, err := st.LoadConversationRoundPage(ctx, "agent-history", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Rounds) != 2 || page.Rounds[0].RoundID != "round-3" || page.Rounds[1].RoundID != "round-4" || !page.HasMore || page.NextBeforeRoundID != "round-3" {
		t.Fatalf("first round page=%+v", page)
	}
	// Appending a newer round does not invalidate a cursor anchored to an
	// existing immutable round.
	if err := seedHistoryRound(t, st, "agent-history", 5, "round-5", "revision-5", "response 5"); err != nil {
		t.Fatal(err)
	}
	older, err := st.LoadConversationRoundPage(ctx, "agent-history", page.NextBeforeRoundID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(older.Rounds) != 2 || older.Rounds[0].RoundID != "round-1" || older.Rounds[1].RoundID != "round-2" || older.HasMore {
		t.Fatalf("older round page after append=%+v", older)
	}

	items, err := st.LoadConversationRoundItems(ctx, "agent-history", "round-2", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !items.Selected || items.RevisionID != "revision-2" || len(items.Items) != 2 || items.Items[0].ItemID != "round-2/item-1" || items.Items[1].ItemID != "round-2/item-2" || !items.HasMore || items.NextAfterItemID != "round-2/item-2" {
		t.Fatalf("selected revision item page=%+v", items)
	}
	next, err := st.LoadConversationRoundItems(ctx, "agent-history", "round-2", "", items.NextAfterItemID, 2)
	if err != nil || len(next.Items) != 1 || next.Items[0].ItemID != "round-2/item-3" || next.HasMore {
		t.Fatalf("next item page=%+v err=%v", next, err)
	}

	oldRevision, err := st.LoadConversationRoundItems(ctx, "agent-history", "round-2", "revision-old", "", 10)
	if err != nil || oldRevision.Selected || oldRevision.RevisionStatus != "superseded" || len(oldRevision.Items) != 1 || oldRevision.Items[0].Payload.Content != "older response" {
		t.Fatalf("old revision page=%+v err=%v", oldRevision, err)
	}
	revisions, err := st.LoadConversationRoundRevisions(ctx, "agent-history", "round-2")
	if err != nil || revisions.SelectedRevisionID != "revision-2" || len(revisions.Revisions) != 2 {
		t.Fatalf("revision list=%+v err=%v", revisions, err)
	}
	if revisions.Revisions[0].RevisionID != "revision-2" || !revisions.Revisions[0].Selected || revisions.Revisions[0].ItemCount != 3 {
		t.Fatalf("unexpected selected revision summary: %+v", revisions.Revisions[0])
	}
	if revisions.Revisions[1].RevisionID != "revision-old" || revisions.Revisions[1].Selected || revisions.Revisions[1].Status != "superseded" || revisions.Revisions[1].ItemCount != 1 {
		t.Fatalf("unexpected older revision summary: %+v", revisions.Revisions[1])
	}
	if _, err := st.LoadConversationRoundItems(ctx, "agent-history", "round-2", "", "not-an-item", 10); !errors.Is(err, ErrConversationHistoryCursor) {
		t.Fatalf("invalid item cursor error=%v", err)
	}
}

func TestConversationHistoryCursorBecomesStaleAfterClear(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "history-clear.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedConversationHistory(t, st, "agent-clear", 2)
	page, err := st.LoadConversationRoundPage(context.Background(), "agent-clear", "", 1)
	if err != nil || page.NextBeforeRoundID != "round-2" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := st.db.Exec(`DELETE FROM conversation_round_revisions WHERE conversation_id=?`, "agent-clear"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DELETE FROM conversation_rounds WHERE conversation_id=?`, "agent-clear"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DELETE FROM conversation_records WHERE conversation_id=?`, "agent-clear"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.LoadConversationRoundPage(context.Background(), "agent-clear", page.NextBeforeRoundID, 1); !errors.Is(err, ErrConversationHistoryCursor) {
		t.Fatalf("cleared conversation cursor error=%v", err)
	}
}

func TestConversationLegacyItemsRemainReadableWithStableCursor(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "history-legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	result, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "agent-legacy", NodeID: "node"}, &ConversationDelta{
		ConversationID: "agent-legacy",
		Upserts: []llm.Message{
			{Role: "user", Content: "older user message", RecordID: "legacy-user"},
			{Role: "assistant", Content: "older assistant reply", RecordID: "legacy-assistant"},
		},
	})
	if err != nil || result.ConversationRevision != 1 {
		t.Fatalf("legacy seed result=%+v err=%v", result, err)
	}
	page, err := st.LoadConversationLegacyItems(ctx, "agent-legacy", "", 1)
	if err != nil || page.HistoryCompleteness != "complete" || len(page.Items) != 1 || !page.HasMore || page.Items[0].ItemID != "legacy-user" || page.NextAfterItemID != "legacy-user" {
		t.Fatalf("first legacy page=%+v err=%v", page, err)
	}
	next, err := st.LoadConversationLegacyItems(ctx, "agent-legacy", page.NextAfterItemID, 1)
	if err != nil || len(next.Items) != 1 || next.HasMore || next.Items[0].ItemID != "legacy-assistant" {
		t.Fatalf("next legacy page=%+v err=%v", next, err)
	}
	if _, err := st.LoadConversationLegacyItems(ctx, "agent-legacy", "missing", 1); !errors.Is(err, ErrConversationHistoryCursor) {
		t.Fatalf("invalid cursor error=%v", err)
	}
}

func seedConversationHistory(t *testing.T, st *SQLiteStore, conversationID string, count int) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`INSERT INTO conversations(conversation_id,node_id,revision,current_checkpoint_revision,lifecycle_covered_seq,history_completeness,created_at,updated_at) VALUES(?, 'node', 10, 1, 0, 'complete', ?, ?)`, conversationID, now, now); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= count; i++ {
		if err := seedHistoryRound(t, st, conversationID, int64(i), fmt.Sprintf("round-%d", i), fmt.Sprintf("revision-%d", i), fmt.Sprintf("response %d", i)); err != nil {
			t.Fatal(err)
		}
	}
}

func seedHistoryRound(t *testing.T, st *SQLiteStore, conversationID string, ordinal int64, roundID, revisionID, response string) error {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`INSERT INTO conversation_rounds(conversation_id,round_id,ordinal,selected_revision_id,pre_checkpoint_revision) VALUES(?,?,?,?,1)`, conversationID, roundID, ordinal, revisionID); err != nil {
		return err
	}
	if _, err := st.db.Exec(`INSERT INTO conversation_round_revisions(conversation_id,round_id,revision_id,turn_id,source,generation,status,finish_reason,record_revision,checkpoint_revision,pre_checkpoint_revision) VALUES(?,?,?,?,?,1,'completed','success',10,1,1)`, conversationID, roundID, revisionID, "turn-"+roundID, "human"); err != nil {
		return err
	}
	for i := 1; i <= 3; i++ {
		message := llm.Message{Role: "assistant", Content: response}
		if i == 1 {
			message = llm.Message{Role: "user", Content: fmt.Sprintf("request %d", ordinal)}
		}
		message.RecordID = fmt.Sprintf("%s/item-%d", roundID, i)
		raw, err := json.Marshal(message)
		if err != nil {
			return err
		}
		itemOrdinal := ordinal*10 + int64(i)
		if _, err := st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?,1,'',?,?)`, conversationID, message.RecordID, itemOrdinal, roundID, revisionID, message.Role, string(raw), now, now); err != nil {
			return err
		}
	}
	return nil
}

func seedHistoryRevision(t *testing.T, st *SQLiteStore, conversationID, roundID, revisionID, status string, ordinal int64, content string) error {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`INSERT INTO conversation_round_revisions(conversation_id,round_id,revision_id,turn_id,source,generation,status,finish_reason,record_revision,checkpoint_revision,pre_checkpoint_revision) VALUES(?,?,?,?,?,1,?,'success',10,1,1)`, conversationID, roundID, revisionID, "turn-"+revisionID, "human", status); err != nil {
		return err
	}
	message := llm.Message{Role: "assistant", Content: content, RecordID: roundID + "/old"}
	raw, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(`INSERT INTO conversation_records(conversation_id,item_id,ordinal,round_id,revision_id,role,payload_json,revision,legacy_kind,created_at,updated_at) VALUES(?,?,?,?,?,?,?,1,'',?,?)`, conversationID, message.RecordID, ordinal, roundID, revisionID, message.Role, string(raw), now, now)
	return err
}
