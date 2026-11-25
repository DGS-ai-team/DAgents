package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
)

func TestLegacyConversationMigrationIsIdempotentAndSeparatesCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agent_runtimes(agent_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,messages_json TEXT NOT NULL,first_user_message TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,loaded_skills_json TEXT NOT NULL,runtime_state_json TEXT NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{
		{Role: "user", Content: "original", RecordID: "source/user-1"},
		{Role: "assistant", Content: "current", DisplayIdentity: &llm.MessageDisplayIdentity{ContentItemID: "step-1/assistant/content", ReasoningItemID: "step-1/assistant/reasoning", ItemVersion: 1}},
	}
	raw, _ := json.Marshal(messages)
	state, _ := json.Marshal(RuntimeState{HistoryRevision: 17, ActiveContextStart: 1})
	_, err = db.Exec(`INSERT INTO agent_runtimes VALUES(?,?,?,?,?,?,?,?)`, "legacy", "node", string(raw), "original", "2026-01-01T00:00:00Z", "2026-01-01T00:00:01Z", "[]", string(state))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := st.LoadConversationRecords(context.Background(), "legacy")
	if err != nil || len(records) != 2 {
		t.Fatalf("records=%+v err=%v", records, err)
	}
	if records[0].RecordID != "source/user-1" {
		t.Fatalf("existing identity lost: %+v", records[0])
	}
	if records[1].RecordID != "step-1/assistant/content" || records[1].DisplayIdentity == nil || records[1].DisplayIdentity.ReasoningItemID != "step-1/assistant/reasoning" {
		t.Fatalf("assistant display identity lost: %+v", records[1])
	}
	checkpoint, err := st.LoadConversationCheckpoint(context.Background(), "legacy")
	if err != nil || checkpoint == nil || len(checkpoint.Messages) != 1 || checkpoint.Messages[0].Content != "current" || checkpoint.Revision == 17 {
		t.Fatalf("checkpoint=%+v err=%v", checkpoint, err)
	}
	firstIDs := []string{records[0].RecordID, records[1].RecordID}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	records, err = st.LoadConversationRecords(context.Background(), "legacy")
	if err != nil || len(records) != 2 || records[0].RecordID != firstIDs[0] || records[1].RecordID != firstIDs[1] {
		t.Fatalf("migration was not idempotent: records=%+v err=%v", records, err)
	}
}

func TestLegacyConversationMigrationBuildsToolCallLookupColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-tools.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE agent_runtimes(agent_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,messages_json TEXT NOT NULL,first_user_message TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,loaded_skills_json TEXT NOT NULL,runtime_state_json TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	messages := []llm.Message{
		{Role: "assistant", RecordID: "source/tool-call-legacy", ToolCalls: []llm.ToolCall{{ID: "legacy-call-1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"notes.txt"}`}}}},
		{Role: "tool", Name: "read_file", ToolCallID: "legacy-call-1", Content: "first legacy result"},
		{Role: "assistant", RecordID: "source/tool-call-later", ToolCalls: []llm.ToolCall{{ID: "legacy-call-1", Type: "function", Function: llm.ToolCallFunction{Name: "read_file", Arguments: `{"path":"later.txt"}`}}}},
		{Role: "tool", Name: "read_file", ToolCallID: "legacy-call-1", Content: "second legacy result"},
	}
	raw, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := json.Marshal(RuntimeState{ActiveContextStart: len(messages)})
	if _, err := db.Exec(`INSERT INTO agent_runtimes VALUES(?,?,?,?,?,?,?,?)`, "legacy-tools", "node", string(raw), "", "2026-01-01T00:00:00Z", "2026-01-01T00:00:01Z", "[]", string(state)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	records, err := st.LoadConversationRecords(context.Background(), "legacy-tools")
	if err != nil || len(records) != 4 {
		t.Fatalf("migrated records=%+v err=%v, want both tool executions", records, err)
	}
	var firstCallID, firstContent, secondCallID, secondContent, secondSource string
	if err := st.db.QueryRow(`SELECT tool_call_id,payload_json FROM conversation_records WHERE conversation_id='legacy-tools' AND item_id=?`, llm.ToolResultRecordID("legacy-call-1")).Scan(&firstCallID, &firstContent); err != nil {
		t.Fatal(err)
	}
	scopedID := llm.ToolResultRecordIDForExecution("legacy-call-1", "source/tool-call-later")
	if err := st.db.QueryRow(`SELECT tool_call_id,payload_json FROM conversation_records WHERE conversation_id='legacy-tools' AND item_id=?`, scopedID).Scan(&secondCallID, &secondContent); err != nil {
		t.Fatal(err)
	}
	var firstMessage, secondMessage llm.Message
	if err := json.Unmarshal([]byte(firstContent), &firstMessage); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(secondContent), &secondMessage); err != nil {
		t.Fatal(err)
	}
	secondSource = secondMessage.ToolCallSourceID
	if firstCallID != "legacy-call-1" || secondCallID != "legacy-call-1" || firstMessage.Content != "first legacy result" || secondMessage.Content != "second legacy result" || secondSource != "source/tool-call-later" {
		t.Fatalf("reused call ID migration lost or mislinked results: first=%+v second=%+v index=%q/%q", firstMessage, secondMessage, firstCallID, secondCallID)
	}
}

func TestLegacyConversationMigrationFailureRollsBackSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad-legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agent_runtimes(agent_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,messages_json TEXT NOT NULL,first_user_message TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,loaded_skills_json TEXT NOT NULL,runtime_state_json TEXT NOT NULL); INSERT INTO agent_runtimes VALUES('bad','node','{broken','', '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','[]','{}')`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if st, err := Open(path); err == nil {
		_ = st.Close()
		t.Fatal("Open unexpectedly migrated malformed legacy payload")
	}
	db, err = sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('conversation_schema_meta','conversations','conversation_records','context_checkpoints')`).Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("migration DDL escaped rollback: found %d tables", count)
	}
}

func TestConversationStorageV1MigrationAddsReadIndexesAfterLegacyColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation-v1.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE agent_runtimes(agent_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,messages_json TEXT NOT NULL,first_user_message TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,loaded_skills_json TEXT NOT NULL,runtime_state_json TEXT NOT NULL);
CREATE TABLE conversation_schema_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);
INSERT INTO conversation_schema_meta(id,version) VALUES(1,1);
CREATE TABLE conversations(conversation_id TEXT PRIMARY KEY,node_id TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL DEFAULT 0,current_checkpoint_revision INTEGER NOT NULL DEFAULT 0,lifecycle_covered_seq INTEGER NOT NULL DEFAULT 0,history_completeness TEXT NOT NULL DEFAULT 'complete',created_at TEXT NOT NULL,updated_at TEXT NOT NULL);
CREATE TABLE conversation_rounds(conversation_id TEXT NOT NULL,round_id TEXT NOT NULL,ordinal INTEGER NOT NULL,PRIMARY KEY(conversation_id,round_id));
CREATE TABLE conversation_round_revisions(conversation_id TEXT NOT NULL,round_id TEXT NOT NULL,revision_id TEXT NOT NULL,turn_id TEXT NOT NULL,source TEXT NOT NULL DEFAULT '',generation INTEGER NOT NULL DEFAULT 0,status TEXT NOT NULL DEFAULT 'active',finish_reason TEXT NOT NULL DEFAULT '',record_revision INTEGER NOT NULL DEFAULT 0,checkpoint_revision INTEGER NOT NULL DEFAULT 0,pre_checkpoint_revision INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(conversation_id,round_id,revision_id),UNIQUE(conversation_id,turn_id));
CREATE TABLE conversation_records(conversation_id TEXT NOT NULL,item_id TEXT NOT NULL,ordinal INTEGER NOT NULL,round_id TEXT NOT NULL DEFAULT '',role TEXT NOT NULL DEFAULT '',payload_json TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 1,legacy_kind TEXT NOT NULL DEFAULT '',created_at TEXT NOT NULL,updated_at TEXT NOT NULL,PRIMARY KEY(conversation_id,item_id),UNIQUE(conversation_id,ordinal));
CREATE TABLE context_checkpoints(conversation_id TEXT NOT NULL,revision INTEGER NOT NULL,messages_json TEXT NOT NULL,lineage TEXT NOT NULL DEFAULT '',lifecycle_covered_seq INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,PRIMARY KEY(conversation_id,revision));
`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("migrate v1 conversation schema: %v", err)
	}
	defer st.Close()
	var hasRevisionID, hasReadIndex int
	for _, check := range []struct {
		query string
		args  []any
		dest  *int
	}{
		{query: `SELECT COUNT(*) FROM pragma_table_info('conversation_records') WHERE name='revision_id'`, dest: &hasRevisionID},
		{query: `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_conversation_records_round_order'`, dest: &hasReadIndex},
	} {
		if err := st.db.QueryRow(check.query, check.args...).Scan(check.dest); err != nil {
			t.Fatal(err)
		}
	}
	if hasRevisionID != 1 || hasReadIndex != 1 {
		t.Fatalf("v1 migration columns/index = %d/%d, want 1/1", hasRevisionID, hasReadIndex)
	}
}

func TestConversationStorageV3MigrationAddsCheckpointDeltaColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conversation-v3.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE conversation_schema_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL);
INSERT INTO conversation_schema_meta(id,version) VALUES(1,3);
CREATE TABLE context_checkpoints(conversation_id TEXT NOT NULL,revision INTEGER NOT NULL,messages_json TEXT NOT NULL,lineage TEXT NOT NULL DEFAULT '',lifecycle_covered_seq INTEGER NOT NULL DEFAULT 0,created_at TEXT NOT NULL,PRIMARY KEY(conversation_id,revision));
INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES('legacy',1,'[]','legacy',0,'2026-01-01T00:00:00Z');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatalf("migrate v3 conversation schema: %v", err)
	}
	defer st.Close()
	var version, columns int
	if err := st.db.QueryRow(`SELECT version FROM conversation_schema_meta WHERE id=1`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('context_checkpoints') WHERE name IN ('checkpoint_kind','base_revision','delta_json')`).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	if version != conversationStorageVersion || columns != 3 {
		t.Fatalf("schema version=%d checkpoint delta columns=%d", version, columns)
	}
	checkpoint, err := st.LoadConversationCheckpointRevision(context.Background(), "legacy", 1)
	if err != nil || checkpoint == nil || checkpoint.Lineage != "legacy" || len(checkpoint.Messages) != 0 {
		t.Fatalf("legacy snapshot after migration=%+v err=%v", checkpoint, err)
	}
}

func TestLegacyCompressedSnapshotDoesNotInventMissingRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compressed-legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agent_runtimes(agent_id TEXT PRIMARY KEY,node_id TEXT NOT NULL,messages_json TEXT NOT NULL,first_user_message TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL,loaded_skills_json TEXT NOT NULL,runtime_state_json TEXT NOT NULL)`)
	if err != nil {
		t.Fatal(err)
	}
	messages, _ := json.Marshal([]llm.Message{{Role: "user", Name: llm.UserNameCompression, Content: "legacy summary"}})
	_, err = db.Exec(`INSERT INTO agent_runtimes VALUES('compressed','node',?,'', '2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','[]','{}')`, string(messages))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	records, err := st.LoadConversationRecords(context.Background(), "compressed")
	if err != nil || len(records) != 1 || records[0].Content != "legacy summary" {
		t.Fatalf("migration fabricated or lost source records: %+v err=%v", records, err)
	}
	var kind, completeness string
	if err := st.db.QueryRow(`SELECT r.legacy_kind,c.history_completeness FROM conversation_records r JOIN conversations c USING(conversation_id) WHERE r.conversation_id='compressed'`).Scan(&kind, &completeness); err != nil {
		t.Fatal(err)
	}
	if kind != "legacy_summary_unknown_predecessors" || completeness != "legacy_snapshot_may_be_incomplete" {
		t.Fatalf("legacy unknown relationship not marked: kind=%q completeness=%q", kind, completeness)
	}
}

func TestConversationSaveAtomicallyCommitsRecordsAndCheckpoint(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "atomic.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	msgs := []llm.Message{{Role: "user", Content: "archived"}, {Role: "assistant", Content: "summary"}, {Role: "user", Content: "next"}}
	if err := st.SaveConversationSnapshot(ctx, Record{AgentID: "s", NodeID: "n", Messages: msgs, RuntimeState: RuntimeState{HistoryRevision: 42, ActiveContextStart: 1, InputBoxState: json.RawMessage(`{"seq":7}`)}}); err != nil {
		t.Fatal(err)
	}
	full, err := st.LoadConversationRecords(ctx, "s")
	if err != nil || len(full) != 3 || full[0].Content != "archived" {
		t.Fatalf("full history=%+v err=%v", full, err)
	}
	rec, err := st.Load(ctx, "s")
	if err != nil || rec == nil || len(rec.Messages) != 2 || rec.Messages[0].Content != "summary" || rec.RuntimeState.ActiveContextStart != 0 || string(rec.RuntimeState.InputBoxState) != `{"seq":7}` {
		t.Fatalf("restored checkpoint=%+v err=%v", rec, err)
	}
	var historyRevision uint64
	if err := st.db.QueryRow(`SELECT revision FROM conversations WHERE conversation_id='s'`).Scan(&historyRevision); err != nil {
		t.Fatal(err)
	}
	if historyRevision != 42 || rec.RuntimeState.ContextCheckpointRevision == historyRevision {
		t.Fatalf("revision namespaces collided: history=%d checkpoint=%d", historyRevision, rec.RuntimeState.ContextCheckpointRevision)
	}
	var recordRevision int
	if err := st.db.QueryRow(`SELECT revision FROM conversation_records WHERE conversation_id='s' AND item_id=?`, full[0].RecordID).Scan(&recordRevision); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER fail_checkpoint BEFORE INSERT ON context_checkpoints BEGIN SELECT RAISE(ABORT,'injected checkpoint failure'); END`); err != nil {
		t.Fatal(err)
	}
	// Runtime-only mailbox updates keep the same history revision. They must
	// not touch records or checkpoints, even while checkpoint writes fail.
	if err := st.Save(ctx, Record{AgentID: "s", NodeID: "n", Messages: msgs, RuntimeState: RuntimeState{HistoryRevision: 42, InputBoxState: json.RawMessage(`{"seq":9}`)}}); err != nil {
		t.Fatalf("metadata-only save touched checkpoint: %v", err)
	}
	var recordRevisionAfter, checkpointRevisionAfter uint64
	if err := st.db.QueryRow(`SELECT revision FROM conversation_records WHERE conversation_id='s' AND item_id=?`, full[0].RecordID).Scan(&recordRevisionAfter); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT current_checkpoint_revision FROM conversations WHERE conversation_id='s'`).Scan(&checkpointRevisionAfter); err != nil {
		t.Fatal(err)
	}
	if recordRevisionAfter != uint64(recordRevision) || checkpointRevisionAfter != rec.RuntimeState.ContextCheckpointRevision {
		t.Fatalf("metadata-only save rewrote content: record revision %d->%d checkpoint %d->%d", recordRevision, recordRevisionAfter, rec.RuntimeState.ContextCheckpointRevision, checkpointRevisionAfter)
	}
	if err := st.SaveConversationSnapshot(ctx, Record{AgentID: "s", NodeID: "n", Messages: msgs, RuntimeState: RuntimeState{HistoryRevision: 43, ActiveContextStart: 1}}); err == nil {
		t.Fatal("history revision change did not attempt checkpoint write")
	}
	var checkpointCount int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM context_checkpoints WHERE conversation_id='s'`).Scan(&checkpointCount); err != nil || checkpointCount != 1 {
		t.Fatalf("checkpoint retention count=%d err=%v, want only current checkpoint", checkpointCount, err)
	}
	err = st.SaveConversationSnapshot(ctx, Record{AgentID: "s2", NodeID: "n", Messages: []llm.Message{{Role: "user", Content: "must rollback"}}, RuntimeState: RuntimeState{InputBoxState: json.RawMessage(`{"seq":1}`)}})
	if err == nil {
		t.Fatal("injected checkpoint failure was ignored")
	}
	if rec, loadErr := st.Load(ctx, "s2"); loadErr != nil || rec != nil {
		t.Fatalf("runtime row escaped failed tx: rec=%+v err=%v", rec, loadErr)
	}
	if records, loadErr := st.LoadConversationRecords(ctx, "s2"); loadErr != nil || len(records) != 0 {
		t.Fatalf("records escaped failed tx: %+v err=%v", records, loadErr)
	}
}

func TestListConversationWatermarksDoesNotReadMessagePayloads(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "watermarks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SaveConversationSnapshot(ctx, Record{
		AgentID:      "watermark-agent",
		NodeID:       "node-1",
		Messages:     []llm.Message{{Role: "user", Content: "hello", RecordID: "user-1"}},
		RuntimeState: RuntimeState{HistoryRevision: 7},
	}); err != nil {
		t.Fatal(err)
	}
	// Deliberately corrupt both legacy payload columns. The watermark query must
	// depend only on indexed conversation revision metadata.
	if _, err := st.db.ExecContext(ctx, `UPDATE agent_runtimes SET messages_json='not-json', runtime_state_json='not-json' WHERE agent_id='watermark-agent'`); err != nil {
		t.Fatal(err)
	}

	items, err := st.ListConversationWatermarks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("watermarks=%+v, want one conversation", items)
	}
	if got := items[0]; got.ConversationID != "watermark-agent" || got.NodeID != "node-1" || got.ConversationRevision == 0 {
		t.Fatalf("watermark=%+v, want the lightweight revision metadata", got)
	}
}

func TestConversationDeltaTouchesOnlyNamedItemsAndCheckpointIsOptional(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "delta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	initial := []llm.Message{{Role: "user", Content: "first", RecordID: "input/1"}, {Role: "assistant", Content: "answer", RecordID: "step/assistant/content"}}
	if err := st.SaveConversationSnapshot(ctx, Record{AgentID: "delta", NodeID: "node", Messages: initial, RuntimeState: RuntimeState{HistoryRevision: 2, ActiveContextStart: 0}}); err != nil {
		t.Fatal(err)
	}
	type stamp struct {
		revision int
		updated  string
	}
	readStamp := func(id string) stamp {
		var got stamp
		if err := st.db.QueryRow(`SELECT revision,updated_at FROM conversation_records WHERE conversation_id='delta' AND item_id=?`, id).Scan(&got.revision, &got.updated); err != nil {
			t.Fatal(err)
		}
		return got
	}
	oldUser, oldAssistant := readStamp("input/1"), readStamp("step/assistant/content")
	var cpBefore uint64
	if err := st.db.QueryRow(`SELECT current_checkpoint_revision FROM conversations WHERE conversation_id='delta'`).Scan(&cpBefore); err != nil {
		t.Fatal(err)
	}
	result, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "delta", NodeID: "node", RuntimeState: RuntimeState{HistoryRevision: 3}}, &ConversationDelta{ConversationID: "delta", NodeID: "node", ExpectedRevision: 2, HistoryRevision: 3, Upserts: []llm.Message{{Role: "tool", Content: "ok", ToolCallID: "call-1", RecordID: llm.ToolResultRecordID("call-1")}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := readStamp("input/1"); got != oldUser {
		t.Fatalf("unrelated user record changed: before=%+v after=%+v", oldUser, got)
	}
	if got := readStamp("step/assistant/content"); got != oldAssistant {
		t.Fatalf("unrelated assistant record changed: before=%+v after=%+v", oldAssistant, got)
	}
	if result.ConversationRevision != 3 || result.CheckpointRevision != cpBefore {
		t.Fatalf("append result=%+v, checkpoint should remain %d", result, cpBefore)
	}
	if len(result.ChangedItems) != 1 || result.ChangedItems[0] != (ConversationItemRevision{ItemID: llm.ToolResultRecordID("call-1"), Ordinal: 3, Revision: 1}) || len(result.RemovedItemIDs) != 0 {
		t.Fatalf("append display delta metadata=%+v, want the new result at ordinal 3 revision 1", result)
	}
	if len(result.ChangedPayloads) != 1 || result.ChangedPayloads[0].RecordID != llm.ToolResultRecordID("call-1") || result.ChangedPayloads[0].Content != "ok" {
		t.Fatalf("committed changed payloads=%+v, want only the committed tool result", result.ChangedPayloads)
	}
	var triggerCount int
	if _, err := st.db.Exec(`CREATE TRIGGER block_content_access BEFORE INSERT ON conversation_records BEGIN SELECT RAISE(ABORT,'records were touched'); END; CREATE TRIGGER block_checkpoint_access BEFORE INSERT ON context_checkpoints BEGIN SELECT RAISE(ABORT,'checkpoint was touched'); END`); err != nil {
		t.Fatal(err)
	}
	if err := st.Save(ctx, Record{AgentID: "delta", NodeID: "node", Messages: []llm.Message{{Role: "assistant", Content: "ignored metadata payload"}}, RuntimeState: RuntimeState{HistoryRevision: 3}}); err != nil {
		t.Fatalf("metadata-only persistence accessed content tables: %v", err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM conversation_records WHERE conversation_id='delta'`).Scan(&triggerCount); err != nil || triggerCount != 3 {
		t.Fatalf("records=%d err=%v", triggerCount, err)
	}
	if _, err := st.db.Exec(`DROP TRIGGER block_content_access; DROP TRIGGER block_checkpoint_access`); err != nil {
		t.Fatal(err)
	}
	result, err = st.PersistRuntimeDelta(ctx, Record{AgentID: "delta", NodeID: "node", RuntimeState: RuntimeState{HistoryRevision: 4}}, &ConversationDelta{ConversationID: "delta", NodeID: "node", ExpectedRevision: 3, HistoryRevision: 4, Checkpoint: &CheckpointCommit{Messages: []llm.Message{{Role: "user", Name: llm.UserNameCompression, Content: "compact"}}, Lineage: "compression"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ConversationRevision != 4 || result.CheckpointRevision != cpBefore+1 {
		t.Fatalf("checkpoint-only result=%+v", result)
	}
	if got := readStamp("input/1"); got != oldUser {
		t.Fatalf("checkpoint-only write changed transcript record: before=%+v after=%+v", oldUser, got)
	}
	if records, err := st.LoadConversationRecords(ctx, "delta"); err != nil || len(records) != 3 {
		t.Fatalf("compression changed display history: records=%d err=%v", len(records), err)
	}
}

func TestConversationDeltaAcceptsReusedToolCallIDAcrossExecutions(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "reused-tool-call.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.SaveConversationSnapshot(ctx, Record{
		AgentID: "reused-tool", NodeID: "node", Messages: []llm.Message{{Role: "user", Content: "start", RecordID: "input/1"}},
		RuntimeState: RuntimeState{HistoryRevision: 1},
	}); err != nil {
		t.Fatal(err)
	}
	for index, scope := range []string{"turn-one/step-one", "turn-two/step-one"} {
		recordID := llm.ToolResultRecordIDForExecution("provider-reused-id", scope)
		result, err := st.PersistRuntimeDelta(ctx,
			Record{AgentID: "reused-tool", NodeID: "node", RuntimeState: RuntimeState{HistoryRevision: uint64(index + 2)}},
			&ConversationDelta{
				ConversationID: "reused-tool", NodeID: "node", ExpectedRevision: uint64(index + 1), HistoryRevision: uint64(index + 2),
				Upserts: []llm.Message{{Role: "tool", Name: "read_file", ToolCallID: "provider-reused-id", RecordID: recordID, Content: scope}},
			},
		)
		if err != nil {
			t.Fatalf("persist execution %q: %v", scope, err)
		}
		if len(result.ChangedItems) != 1 || result.ChangedItems[0].ItemID != recordID {
			t.Fatalf("execution %q changed items=%+v", scope, result.ChangedItems)
		}
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM conversation_records WHERE conversation_id=? AND role='tool' AND tool_call_id=?`, "reused-tool", "provider-reused-id").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("stored tool result records=%d, want 2 independent executions", count)
	}
}

func TestConversationCheckpointDeltaRoundTripsAndRetainsBaseChain(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "checkpoint-delta.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	base := []llm.Message{{Role: "user", Content: "first"}, {Role: "assistant", Content: "second"}}
	initial, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "checkpoint-delta", NodeID: "node"}, &ConversationDelta{
		ConversationID: "checkpoint-delta", NodeID: "node", ExpectedRevision: 0,
		Checkpoint: &CheckpointCommit{Messages: base, Lineage: "base"},
	})
	if err != nil || initial.CheckpointRevision != 1 {
		t.Fatalf("initial checkpoint=%+v err=%v", initial, err)
	}
	second, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "checkpoint-delta", NodeID: "node"}, &ConversationDelta{
		ConversationID: "checkpoint-delta", NodeID: "node", ExpectedRevision: initial.ConversationRevision,
		Checkpoint: &CheckpointCommit{Lineage: "live_append", Delta: &CheckpointDelta{BaseRevision: initial.CheckpointRevision, Operations: []CheckpointOperation{
			{Kind: "insert", Index: 1, Message: llm.Message{Role: "system", Content: "injected before second"}},
			{Kind: "append", Message: llm.Message{Role: "user", Content: "third"}},
		}}},
	})
	if err != nil || second.CheckpointRevision != 2 {
		t.Fatalf("delta checkpoint=%+v err=%v", second, err)
	}
	third, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "checkpoint-delta", NodeID: "node"}, &ConversationDelta{
		ConversationID: "checkpoint-delta", NodeID: "node", ExpectedRevision: second.ConversationRevision,
		Checkpoint: &CheckpointCommit{Lineage: "live_append", Delta: &CheckpointDelta{BaseRevision: second.CheckpointRevision, Operations: []CheckpointOperation{
			{Kind: "replace", Index: 2, Message: llm.Message{Role: "assistant", Content: "second updated"}},
		}}},
	})
	if err != nil || third.CheckpointRevision != 3 {
		t.Fatalf("second delta checkpoint=%+v err=%v", third, err)
	}
	want := []llm.Message{
		{Role: "user", Content: "first"},
		{Role: "system", Content: "injected before second"},
		{Role: "assistant", Content: "second updated"},
		{Role: "user", Content: "third"},
	}
	for _, revision := range []uint64{2, 3} {
		checkpoint, err := st.LoadConversationCheckpointRevision(ctx, "checkpoint-delta", revision)
		if err != nil || checkpoint == nil {
			t.Fatalf("load checkpoint revision %d: checkpoint=%+v err=%v", revision, checkpoint, err)
		}
		if revision == 2 {
			wantAtTwo := []llm.Message{base[0], {Role: "system", Content: "injected before second"}, base[1], {Role: "user", Content: "third"}}
			if !reflect.DeepEqual(checkpoint.Messages, wantAtTwo) {
				t.Fatalf("revision 2 messages=%+v want %+v", checkpoint.Messages, wantAtTwo)
			}
		} else if !reflect.DeepEqual(checkpoint.Messages, want) {
			t.Fatalf("revision 3 messages=%+v want %+v", checkpoint.Messages, want)
		}
	}
	current, err := st.LoadConversationCheckpoint(ctx, "checkpoint-delta")
	if err != nil || current == nil || !reflect.DeepEqual(current.Messages, want) {
		t.Fatalf("current checkpoint=%+v err=%v", current, err)
	}
	var retained int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM context_checkpoints WHERE conversation_id='checkpoint-delta'`).Scan(&retained); err != nil || retained != 3 {
		t.Fatalf("retained checkpoint chain=%d err=%v, want 3 rows", retained, err)
	}
	if _, err := st.db.Exec(`INSERT INTO conversation_rounds(conversation_id,round_id,ordinal,pre_checkpoint_revision) VALUES('checkpoint-delta','round-1',1,2)`); err != nil {
		t.Fatal(err)
	}
	branched, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "checkpoint-delta", NodeID: "node"}, &ConversationDelta{
		ConversationID: "checkpoint-delta", NodeID: "node", ExpectedRevision: third.ConversationRevision,
		Checkpoint: &CheckpointCommit{Messages: []llm.Message{{Role: "user", Content: "resubmit base"}}, Lineage: "resubmit_base"},
	})
	if err != nil {
		t.Fatalf("commit new full checkpoint branch: %v", err)
	}
	pinned, err := st.LoadConversationCheckpointRevision(ctx, "checkpoint-delta", 2)
	if err != nil || pinned == nil || len(pinned.Messages) != 4 {
		t.Fatalf("pinned pre-round checkpoint=%+v err=%v", pinned, err)
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM context_checkpoints WHERE conversation_id='checkpoint-delta'`).Scan(&retained); err != nil || retained != 3 {
		t.Fatalf("branched checkpoint retention=%d err=%v, want pre-round chain plus current", retained, err)
	}
	badBase := &ConversationDelta{ConversationID: "checkpoint-delta", NodeID: "node", ExpectedRevision: branched.ConversationRevision,
		Checkpoint: &CheckpointCommit{Lineage: "live_append", Delta: &CheckpointDelta{BaseRevision: 1, Operations: []CheckpointOperation{{Kind: "append", Message: llm.Message{Role: "user", Content: "stale"}}}}}}
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "checkpoint-delta", NodeID: "node"}, badBase); err == nil {
		t.Fatal("stale checkpoint base unexpectedly committed")
	}
}

func TestConversationRecordRevisionTracksPayloadChanges(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "record-revision.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	assistant := llm.Message{
		Role:             "assistant",
		Content:          "answer v1",
		ReasoningContent: "reason v1",
		RecordID:         "step-1/assistant",
		DisplayIdentity:  llm.AssistantDisplayIdentityForStep("step-1"),
	}
	if err := st.SaveConversationSnapshot(ctx, Record{AgentID: "record-revision", NodeID: "node", Messages: []llm.Message{assistant}, RuntimeState: RuntimeState{HistoryRevision: 1}}); err != nil {
		t.Fatal(err)
	}
	items, err := st.LoadConversationRecordsWithRevisions(ctx, "record-revision")
	if err != nil || len(items) != 1 || items[0].ItemID != assistant.RecordID || items[0].Revision != 1 {
		t.Fatalf("initial records=%+v err=%v, want row revision 1", items, err)
	}
	// An exact replay does not invalidate the durable item version.
	exactReplay, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "record-revision", NodeID: "node", RuntimeState: RuntimeState{HistoryRevision: 2}}, &ConversationDelta{
		ConversationID: "record-revision", NodeID: "node", ExpectedRevision: 1, HistoryRevision: 2, Upserts: []llm.Message{assistant},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(exactReplay.ChangedItems) != 0 || len(exactReplay.RemovedItemIDs) != 0 {
		t.Fatalf("identical replay should not report a display patch: %+v", exactReplay)
	}
	items, err = st.LoadConversationRecordsWithRevisions(ctx, "record-revision")
	if err != nil || len(items) != 1 || items[0].Revision != 1 {
		t.Fatalf("identical upsert records=%+v err=%v, want unchanged row revision 1", items, err)
	}
	assistant.Content = "answer v2"
	assistant.ReasoningContent = "reason v2"
	changed, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "record-revision", NodeID: "node", RuntimeState: RuntimeState{HistoryRevision: 3}}, &ConversationDelta{
		ConversationID: "record-revision", NodeID: "node", ExpectedRevision: 2, HistoryRevision: 3, Upserts: []llm.Message{assistant},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.ChangedItems) != 1 || changed.ChangedItems[0] != (ConversationItemRevision{ItemID: assistant.RecordID, Ordinal: 1, Revision: 2}) {
		t.Fatalf("changed display metadata=%+v, want item version 2 at stable ordinal 1", changed)
	}
	if len(changed.ChangedPayloads) != 1 || changed.ChangedPayloads[0].Content != "answer v2" {
		t.Fatalf("changed payloads=%+v, want the committed v2 assistant item", changed.ChangedPayloads)
	}
	items, err = st.LoadConversationRecordsWithRevisions(ctx, "record-revision")
	if err != nil || len(items) != 1 || items[0].Revision != 2 || items[0].Payload.Content != "answer v2" {
		t.Fatalf("changed upsert records=%+v err=%v, want row revision 2", items, err)
	}
	removed, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "record-revision", NodeID: "node", RuntimeState: RuntimeState{HistoryRevision: 4}}, &ConversationDelta{
		ConversationID: "record-revision", NodeID: "node", ExpectedRevision: 3, HistoryRevision: 4, DeleteIDs: []string{assistant.RecordID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.ChangedItems) != 0 || len(removed.RemovedItemIDs) != 1 || removed.RemovedItemIDs[0] != assistant.RecordID {
		t.Fatalf("removed display metadata=%+v, want the deleted item identity only", removed)
	}
	if len(removed.RemovedPayloads) != 1 || removed.RemovedPayloads[0].RecordID != assistant.RecordID || removed.RemovedPayloads[0].Content != "answer v2" {
		t.Fatalf("removed payloads=%+v, want the exact deleted display record", removed.RemovedPayloads)
	}
}

func TestSaveConversationSnapshotDoesNotBumpIdenticalItemVersion(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "snapshot-item-version.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	message := llm.Message{Role: "user", Content: "same item", RecordID: "user/1"}
	if err := st.SaveConversationSnapshot(ctx, Record{AgentID: "snapshot-version", NodeID: "node", Messages: []llm.Message{message}, RuntimeState: RuntimeState{HistoryRevision: 1}}); err != nil {
		t.Fatal(err)
	}
	var beforeRevision uint64
	var beforeUpdated string
	if err := st.db.QueryRow(`SELECT revision,updated_at FROM conversation_records WHERE conversation_id=? AND item_id=?`, "snapshot-version", message.RecordID).Scan(&beforeRevision, &beforeUpdated); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveConversationSnapshot(ctx, Record{AgentID: "snapshot-version", NodeID: "node", Messages: []llm.Message{message}, RuntimeState: RuntimeState{HistoryRevision: 2}}); err != nil {
		t.Fatal(err)
	}
	var afterRevision uint64
	var afterUpdated string
	if err := st.db.QueryRow(`SELECT revision,updated_at FROM conversation_records WHERE conversation_id=? AND item_id=?`, "snapshot-version", message.RecordID).Scan(&afterRevision, &afterUpdated); err != nil {
		t.Fatal(err)
	}
	if beforeRevision != 1 || afterRevision != beforeRevision || afterUpdated != beforeUpdated {
		t.Fatalf("identical snapshot changed item version or timestamp: before=(%d,%q) after=(%d,%q)", beforeRevision, beforeUpdated, afterRevision, afterUpdated)
	}
}

func TestConversationDeltaFailureCanRetrySameDirtyBatch(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "delta-retry.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Save(ctx, Record{AgentID: "retry", NodeID: "node", RuntimeState: RuntimeState{HistoryRevision: 1}}); err != nil {
		t.Fatal(err)
	}
	_, err = st.db.Exec(`CREATE TRIGGER fail_delta BEFORE INSERT ON conversation_records BEGIN SELECT RAISE(ABORT,'injected delta failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	delta := &ConversationDelta{ConversationID: "retry", NodeID: "node", ExpectedRevision: 0, HistoryRevision: 2, Upserts: []llm.Message{{Role: "assistant", Content: "result", RecordID: "step/assistant/content"}}, Checkpoint: &CheckpointCommit{Messages: []llm.Message{{Role: "assistant", Content: "result", RecordID: "step/assistant/content"}}}}
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "retry", NodeID: "node"}, delta); err == nil {
		t.Fatal("injected delta failure was ignored")
	}
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM conversation_records WHERE conversation_id='retry'`).Scan(new(int)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DROP TRIGGER fail_delta`); err != nil {
		t.Fatal(err)
	}
	result, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "retry", NodeID: "node"}, delta)
	if err != nil {
		t.Fatal(err)
	}
	if result.ConversationRevision != 1 || result.CheckpointRevision != 1 {
		t.Fatalf("retry result=%+v", result)
	}
}
