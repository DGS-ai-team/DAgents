package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func queueInputRow(t *testing.T, seq, revision uint64, status, content string) InputQueueRow {
	return queueInputRowOfKind(t, seq, revision, "user", status, content)
}

func queueInputRowOfKind(t *testing.T, seq, revision uint64, kind, status, content string) InputQueueRow {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"seq": seq, "revision": revision, "kind": kind,
		"env": map[string]any{"request_type": "message", "content": content},
	})
	if err != nil {
		t.Fatal(err)
	}
	return InputQueueRow{ConversationEpoch: 1, InputSeq: seq, InputRevision: revision, Kind: kind, Status: status, PayloadJSON: payload}
}

func TestAcceptedInputReceiptIsIndexedOutsideRuntimeSnapshot(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "inputs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	receipt := InputReceipt{ClientRequestID: "request-1", PayloadDigest: "sha256", ConversationEpoch: 3, InputSeq: 9, InputRevision: 1, Status: "queued"}
	state := RuntimeState{InputBoxState: json.RawMessage(`{"seq":9,"conversation_epoch":3,"items":[{"seq":9,"revision":1,"kind":"user","env":{"content":"hello"}}]}`)}
	if _, err := st.PersistRuntimeDelta(context.Background(), Record{AgentID: "conversation-1", NodeID: "node-1", RuntimeState: state, AcceptedInputReceipt: &receipt}, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load(context.Background(), "conversation-1")
	if err != nil || loaded == nil {
		t.Fatalf("load runtime: record=%+v err=%v", loaded, err)
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(loaded.RuntimeState.InputBoxState, &snapshot); err != nil {
		t.Fatal(err)
	}
	if _, exists := snapshot["receipts"]; exists {
		t.Fatalf("runtime snapshot still embeds receipt history: %s", loaded.RuntimeState.InputBoxState)
	}
	stored, found, err := st.LookupInputReceipt(context.Background(), "conversation-1", receipt.ClientRequestID)
	if err != nil || !found || stored != receipt {
		t.Fatalf("stored receipt=%+v found=%v err=%v", stored, found, err)
	}
}

func TestInputQueueRowsAndReceiptFollowAtomicQueueRevisions(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	conversation := "queue-session"
	receipt := InputReceipt{ClientRequestID: "queue-request", PayloadDigest: "sha256", ConversationEpoch: 1, InputSeq: 1, InputRevision: 1, Status: "queued"}
	state := RuntimeState{InputBoxState: json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":1,"queue_storage_version":1}`)}
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: conversation, NodeID: "node", RuntimeState: state, InputQueueRows: []InputQueueRow{queueInputRow(t, 1, 1, "queued", "first")}, AcceptedInputReceipt: &receipt}, nil); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load(ctx, conversation)
	if err != nil || loaded == nil {
		t.Fatalf("load queue runtime: record=%+v err=%v", loaded, err)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(loaded.RuntimeState.InputBoxState, &header); err != nil {
		t.Fatal(err)
	}
	if _, exists := header["items"]; exists {
		t.Fatalf("runtime snapshot still owns queue payloads: %s", loaded.RuntimeState.InputBoxState)
	}
	rows, err := st.LoadInputQueue(ctx, conversation)
	if err != nil || len(rows) != 1 || rows[0].Status != "queued" {
		t.Fatalf("loaded queue rows=%+v err=%v", rows, err)
	}
	storedReceipt, found, err := st.LookupInputReceipt(ctx, conversation, receipt.ClientRequestID)
	if err != nil || !found || storedReceipt.Status != "queued" {
		t.Fatalf("queued receipt=%+v found=%v err=%v", storedReceipt, found, err)
	}

	state.InputBoxState = json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":2,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: conversation, NodeID: "node", RuntimeState: state, InputQueueRows: []InputQueueRow{queueInputRow(t, 1, 2, "queued", "edited")}}, nil); err != nil {
		t.Fatalf("persist edited row: %v", err)
	}
	storedReceipt, found, err = st.LookupInputReceipt(ctx, conversation, receipt.ClientRequestID)
	if err != nil || !found || storedReceipt.InputRevision != 2 || storedReceipt.Status != "queued" {
		t.Fatalf("edited receipt=%+v found=%v err=%v", storedReceipt, found, err)
	}

	state.InputBoxState = json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":3,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: conversation, NodeID: "node", RuntimeState: state, InputQueueRows: []InputQueueRow{queueInputRow(t, 1, 2, "claimed", "edited")}}, nil); err != nil {
		t.Fatalf("persist claimed row: %v", err)
	}
	storedReceipt, _, _ = st.LookupInputReceipt(ctx, conversation, receipt.ClientRequestID)
	if storedReceipt.Status != "claimed" {
		t.Fatalf("claimed receipt=%+v", storedReceipt)
	}

	state.InputBoxState = json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":4,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: conversation, NodeID: "node", RuntimeState: state, InputQueueRows: []InputQueueRow{}}, nil); err != nil {
		t.Fatalf("persist queue deletion: %v", err)
	}
	rows, err = st.LoadInputQueue(ctx, conversation)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted queue rows=%+v err=%v", rows, err)
	}
	storedReceipt, found, err = st.LookupInputReceipt(ctx, conversation, receipt.ClientRequestID)
	if err != nil || !found || storedReceipt.Status != "cancelled" {
		t.Fatalf("cancelled receipt=%+v found=%v err=%v", storedReceipt, found, err)
	}
}

func TestLoadInputQueueByKindFiltersRowsAndQueuedCountIncludesAllKinds(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-kind.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	const conversation = "queue-kind"
	rows := []InputQueueRow{
		queueInputRowOfKind(t, 1, 1, "user", "queued", "visible one"),
		queueInputRowOfKind(t, 2, 1, "trigger", "queued", "internal trigger"),
		queueInputRowOfKind(t, 3, 1, "user", "claimed", "visible two"),
		queueInputRowOfKind(t, 4, 1, "child_agent", "queued", "internal child"),
	}
	state := RuntimeState{InputBoxState: json.RawMessage(`{"seq":4,"conversation_epoch":1,"queue_revision":4,"queue_storage_version":1}`)}
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: conversation, NodeID: "node", RuntimeState: state, InputQueueRows: rows}, nil); err != nil {
		t.Fatal(err)
	}

	userRows, err := st.LoadInputQueueByKind(ctx, conversation, "user")
	if err != nil {
		t.Fatal(err)
	}
	if len(userRows) != 2 || userRows[0].InputSeq != 1 || userRows[1].InputSeq != 3 || userRows[1].Status != "claimed" {
		t.Fatalf("user queue rows=%+v, want ordered queued and claimed user rows", userRows)
	}
	queued, err := st.CountQueuedInputs(ctx, conversation)
	if err != nil || queued != 3 {
		t.Fatalf("queued count=%d err=%v, want all three queued kinds", queued, err)
	}
	if _, err := st.LoadInputQueueByKind(ctx, conversation, " "); err == nil {
		t.Fatal("empty queue kind unexpectedly accepted")
	}
}

func TestInputQueueRejectsStaleAndSameRevisionContentWrites(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-cas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	base := Record{AgentID: "queue-cas", NodeID: "node", RuntimeState: RuntimeState{InputBoxState: json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":3,"queue_storage_version":1}`)}, InputQueueRows: []InputQueueRow{queueInputRow(t, 1, 1, "claimed", "original")}}
	if _, err := st.PersistRuntimeDelta(ctx, base, nil); err != nil {
		t.Fatal(err)
	}
	stale := base
	stale.RuntimeState.InputBoxState = json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":2,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, stale, nil); err == nil {
		t.Fatal("stale queue snapshot unexpectedly committed")
	}
	sameRevision := base
	sameRevision.InputQueueRows = []InputQueueRow{queueInputRow(t, 1, 1, "claimed", "different")}
	if _, err := st.PersistRuntimeDelta(ctx, sameRevision, nil); err == nil {
		t.Fatal("same queue revision accepted changed payload")
	}
	rows, err := st.LoadInputQueue(ctx, "queue-cas")
	if err != nil || len(rows) != 1 {
		t.Fatalf("failed CAS corrupted queue rows=%+v err=%v", rows, err)
	}
}

func TestInputQueueControlRequiresRevisionAdvance(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	base := Record{AgentID: "queue-control", NodeID: "node", RuntimeState: RuntimeState{InputBoxState: json.RawMessage(`{"seq":0,"conversation_epoch":1,"queue_revision":1,"queue_control_revision":1,"queue_paused":false,"queue_storage_version":1}`)}, InputQueueRows: []InputQueueRow{}}
	if _, err := st.PersistRuntimeDelta(ctx, base, nil); err != nil {
		t.Fatal(err)
	}
	stale := base
	stale.RuntimeState.InputBoxState = json.RawMessage(`{"seq":0,"conversation_epoch":1,"queue_revision":2,"queue_control_revision":1,"queue_paused":true,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, stale, nil); err == nil {
		t.Fatal("pause change without control revision advance unexpectedly committed")
	}
	updated := base
	updated.RuntimeState.InputBoxState = json.RawMessage(`{"seq":0,"conversation_epoch":1,"queue_revision":2,"queue_control_revision":2,"queue_paused":true,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, updated, nil); err != nil {
		t.Fatalf("persist revisioned queue pause: %v", err)
	}
	loaded, err := st.Load(ctx, "queue-control")
	if err != nil || loaded == nil {
		t.Fatalf("load queue control: record=%+v err=%v", loaded, err)
	}
	paused, revision, err := func() (bool, uint64, error) {
		header, err := parseInputQueueSnapshotHeader(loaded.RuntimeState.InputBoxState)
		return header.QueuePaused, header.QueueControlRevision, err
	}()
	if err != nil || !paused || revision != 2 {
		t.Fatalf("persisted queue control paused=%v revision=%d err=%v", paused, revision, err)
	}
}

func TestInputQueueControlUpdateCanSkipUnchangedRows(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-control-rows.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	conversation := "queue-control-rows"
	base := Record{
		AgentID:        conversation,
		RuntimeState:   RuntimeState{InputBoxState: json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":1,"queue_control_revision":1,"queue_paused":false,"queue_storage_version":1}`)},
		InputQueueRows: []InputQueueRow{queueInputRow(t, 1, 1, "queued", "keep me")},
	}
	if _, err := st.PersistRuntimeDelta(ctx, base, nil); err != nil {
		t.Fatal(err)
	}

	paused := Record{
		AgentID:        conversation,
		RuntimeState:   RuntimeState{InputBoxState: json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":1,"queue_control_revision":2,"queue_paused":true,"queue_storage_version":1}`)},
		InputQueueRows: nil,
	}
	if _, err := st.PersistRuntimeDelta(ctx, paused, nil); err != nil {
		t.Fatalf("persist control-only update without row projection: %v", err)
	}
	rows, err := st.LoadInputQueue(ctx, conversation)
	if err != nil || len(rows) != 1 || rows[0].PayloadJSON == nil {
		t.Fatalf("control-only update changed queue rows=%+v err=%v", rows, err)
	}

	changedQueue := paused
	changedQueue.RuntimeState.InputBoxState = json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":2,"queue_control_revision":2,"queue_paused":true,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, changedQueue, nil); err == nil {
		t.Fatal("queue revision change without row projection unexpectedly committed")
	}
	rows, err = st.LoadInputQueue(ctx, conversation)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rejected projection changed queue rows=%+v err=%v", rows, err)
	}
}

func TestEmptyLegacyMailboxCanInitializeRowBackedHeaderWithoutRows(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-empty-legacy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	conversation := "empty-legacy-mailbox"
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: conversation, NodeID: "node"}, nil); err != nil {
		t.Fatal(err)
	}
	empty := Record{
		AgentID:      conversation,
		NodeID:       "node",
		RuntimeState: RuntimeState{InputBoxState: json.RawMessage(`{"seq":0,"conversation_epoch":1,"queue_revision":1,"queue_control_revision":1,"queue_storage_version":1}`)},
	}
	if _, err := st.PersistRuntimeDelta(ctx, empty, nil); err != nil {
		t.Fatalf("initialize empty row-backed mailbox: %v", err)
	}
	nonempty := empty
	nonempty.RuntimeState.InputBoxState = json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":2,"queue_control_revision":1,"queue_storage_version":1}`)
	if _, err := st.PersistRuntimeDelta(ctx, nonempty, nil); err == nil {
		t.Fatal("nonempty mailbox initialized without its row projection")
	}
}

func TestInputQueueRejectsInvalidClaimAndPayloadIdentity(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	baseState := RuntimeState{InputBoxState: json.RawMessage(`{"seq":2,"conversation_epoch":1,"queue_revision":1,"queue_storage_version":1}`)}
	rows := []InputQueueRow{queueInputRow(t, 1, 1, "claimed", "one"), queueInputRow(t, 2, 1, "claimed", "two")}
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "multiple-claims", RuntimeState: baseState, InputQueueRows: rows}, nil); err == nil {
		t.Fatal("multiple claimed rows unexpectedly committed")
	}
	bad := queueInputRow(t, 1, 1, "queued", "payload")
	bad.InputSeq = 2
	if _, err := st.PersistRuntimeDelta(ctx, Record{AgentID: "identity-mismatch", RuntimeState: baseState, InputQueueRows: []InputQueueRow{bad}}, nil); err == nil {
		t.Fatal("row/payload identity mismatch unexpectedly committed")
	}
	for _, id := range []string{"multiple-claims", "identity-mismatch"} {
		var count int
		if err := st.db.QueryRow(`SELECT count(*) FROM agent_runtimes WHERE agent_id=?`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("invalid queue transaction left runtime row for %s", id)
		}
	}
}

func TestLegacyInputQueueMigrationMovesPayloadAndReceiptsAtomically(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-migration.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	conversation := "legacy-queue"
	legacyReceipt := InputReceipt{ClientRequestID: "legacy-request", PayloadDigest: "sha256", ConversationEpoch: 1, InputSeq: 2, InputRevision: 1, Status: "claimed"}
	legacyState := json.RawMessage(`{"seq":2,"conversation_epoch":1,"items":[{"seq":2,"revision":1,"kind":"user","env":{"content":"legacy payload"}}],"receipts":{"legacy-request":{"client_request_id":"legacy-request","payload_digest":"sha256","conversation_epoch":1,"input_seq":2,"input_revision":1,"status":"claimed"}}}`)
	if err := st.Save(ctx, Record{AgentID: conversation, NodeID: "node", RuntimeState: RuntimeState{InputBoxState: legacyState}}); err != nil {
		t.Fatal(err)
	}
	queuePayload, _ := json.Marshal(map[string]any{"seq": 2, "revision": 1, "kind": "user", "env": map[string]any{"content": "legacy payload"}})
	newState := json.RawMessage(`{"seq":2,"conversation_epoch":1,"queue_revision":1,"queue_storage_version":1}`)
	if err := st.MigrateLegacyInputState(ctx, conversation, newState, []InputQueueRow{{ConversationEpoch: 1, InputSeq: 2, InputRevision: 1, Kind: "user", Status: "claimed", PayloadJSON: queuePayload}}, []InputReceipt{legacyReceipt}, 0, 0); err != nil {
		t.Fatal(err)
	}
	loaded, err := st.Load(ctx, conversation)
	if err != nil || loaded == nil {
		t.Fatalf("load migrated runtime=%+v err=%v", loaded, err)
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(loaded.RuntimeState.InputBoxState, &header); err != nil {
		t.Fatal(err)
	}
	if _, exists := header["items"]; exists {
		t.Fatalf("legacy queue payload remains in runtime state: %s", loaded.RuntimeState.InputBoxState)
	}
	rows, err := st.LoadInputQueue(ctx, conversation)
	if err != nil || len(rows) != 1 || rows[0].InputSeq != 2 || rows[0].Status != "claimed" {
		t.Fatalf("migrated queue rows=%+v err=%v", rows, err)
	}
	stored, found, err := st.LookupInputReceipt(ctx, conversation, legacyReceipt.ClientRequestID)
	if err != nil || !found || stored.Status != "claimed" {
		t.Fatalf("migrated receipt=%+v found=%v err=%v", stored, found, err)
	}
}

func TestLegacyInputQueueMigrationRollsBackSnapshotAndReceiptsTogether(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "input-queue-migration-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	conversation := "legacy-queue-failure"
	legacyState := json.RawMessage(`{"seq":1,"conversation_epoch":1,"items":[{"seq":1,"revision":1,"kind":"user","env":{"content":"legacy payload"}}],"receipts":{"legacy-request":{"client_request_id":"legacy-request","payload_digest":"sha256","conversation_epoch":1,"input_seq":1,"input_revision":1,"status":"queued"}}}`)
	if err := st.Save(ctx, Record{AgentID: conversation, NodeID: "node", RuntimeState: RuntimeState{InputBoxState: legacyState}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER fail_queue_migration BEFORE INSERT ON conversation_input_queue BEGIN SELECT RAISE(ABORT,'queue migration failure'); END`); err != nil {
		t.Fatal(err)
	}
	queuePayload, _ := json.Marshal(map[string]any{"seq": 1, "revision": 1, "kind": "user", "env": map[string]any{"content": "legacy payload"}})
	newState := json.RawMessage(`{"seq":1,"conversation_epoch":1,"queue_revision":1,"queue_storage_version":1}`)
	receipt := InputReceipt{ClientRequestID: "legacy-request", PayloadDigest: "sha256", ConversationEpoch: 1, InputSeq: 1, InputRevision: 1, Status: "queued"}
	err = st.MigrateLegacyInputState(ctx, conversation, newState, []InputQueueRow{{ConversationEpoch: 1, InputSeq: 1, InputRevision: 1, Kind: "user", Status: "queued", PayloadJSON: queuePayload}}, []InputReceipt{receipt}, 0, 0)
	if err == nil {
		t.Fatal("expected injected queue migration failure")
	}
	loaded, loadErr := st.Load(ctx, conversation)
	if loadErr != nil || loaded == nil || string(loaded.RuntimeState.InputBoxState) != string(legacyState) {
		t.Fatalf("failed migration changed legacy runtime snapshot: record=%+v err=%v", loaded, loadErr)
	}
	rows, loadErr := st.LoadInputQueue(ctx, conversation)
	if loadErr != nil || len(rows) != 0 {
		t.Fatalf("failed migration left queue rows=%+v err=%v", rows, loadErr)
	}
	if _, found, lookupErr := st.LookupInputReceipt(ctx, conversation, receipt.ClientRequestID); lookupErr != nil || found {
		t.Fatalf("failed migration left receipt found=%v err=%v", found, lookupErr)
	}
}

func TestAcceptedInputReceiptAndRuntimeSnapshotRollbackTogether(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "inputs-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.Exec(`CREATE TRIGGER fail_input_receipt BEFORE INSERT ON conversation_inputs BEGIN SELECT RAISE(ABORT,'receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	receipt := InputReceipt{ClientRequestID: "request-fail", PayloadDigest: "sha256", ConversationEpoch: 1, InputSeq: 1, InputRevision: 1, Status: "queued"}
	_, err = st.PersistRuntimeDelta(context.Background(), Record{AgentID: "conversation-fail", RuntimeState: RuntimeState{InputBoxState: json.RawMessage(`{"seq":1,"conversation_epoch":1}`)}, AcceptedInputReceipt: &receipt}, nil)
	if err == nil {
		t.Fatal("expected injected receipt write failure")
	}
	var runtimes, receipts int
	if err := st.db.QueryRow(`SELECT count(*) FROM agent_runtimes WHERE agent_id='conversation-fail'`).Scan(&runtimes); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT count(*) FROM conversation_inputs WHERE conversation_id='conversation-fail'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	var queueRows int
	if err := st.db.QueryRow(`SELECT count(*) FROM conversation_input_queue WHERE conversation_id='conversation-fail'`).Scan(&queueRows); err != nil {
		t.Fatal(err)
	}
	if runtimes != 0 || receipts != 0 || queueRows != 0 {
		t.Fatalf("partial receipt transaction: runtimes=%d receipts=%d queue_rows=%d", runtimes, receipts, queueRows)
	}
}

func TestTerminalBoundaryMarksAcceptedInputConsumed(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "inputs-terminal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	receipt := InputReceipt{ClientRequestID: "request-terminal", PayloadDigest: "sha256", ConversationEpoch: 1, InputSeq: 1, InputRevision: 1, Status: "queued"}
	if _, err := st.PersistRuntimeDelta(context.Background(), Record{AgentID: "terminal-session", NodeID: "terminal-agent", AcceptedInputReceipt: &receipt}, nil); err != nil {
		t.Fatal(err)
	}
	boundary := terminalBoundaryFixture()
	boundary.CompletedInputSeq = 1
	boundary.CompletedInputRevision = 1
	if _, _, err := st.CommitTerminalBoundary(context.Background(), boundary); err != nil {
		t.Fatal(err)
	}
	stored, found, err := st.LookupInputReceipt(context.Background(), "terminal-session", receipt.ClientRequestID)
	if err != nil || !found || stored.Status != "consumed" || stored.InputRevision != 1 {
		t.Fatalf("terminal receipt=%+v found=%v err=%v", stored, found, err)
	}
}
