package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	_ "modernc.org/sqlite"
)

func TestColdHydrateDoesNotDecodeInternalInputPayloads(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "hydrate-pending-inputs.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	row := func(seq uint64, kind InputKind, status, content string) store.InputQueueRow {
		t.Helper()
		record := InputRecord{
			Seq: seq, Revision: 1, Kind: kind,
			Env: queue.Envelope{RequestType: queue.RequestTypeMessage, Content: content},
		}
		payload, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		return store.InputQueueRow{
			ConversationEpoch: 1, InputSeq: seq, InputRevision: 1,
			Kind: string(kind), Status: status, PayloadJSON: payload,
		}
	}
	state := store.RuntimeState{InputBoxState: json.RawMessage(`{"seq":3,"conversation_epoch":1,"queue_revision":3,"queue_storage_version":1}`)}
	rows := []store.InputQueueRow{
		row(1, InputKindUser, "queued", "visible queued message"),
		row(2, InputKindTrigger, "queued", "internal trigger payload"),
		row(3, InputKindUser, "claimed", "visible claimed message"),
	}
	if _, err := st.PersistRuntimeDelta(ctx, store.Record{AgentID: "hydrate-cold-inputs", NodeID: "node", RuntimeState: state, InputQueueRows: rows}, nil); err != nil {
		st.Close()
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	// A damaged internal payload must not break the user-facing pending-input
	// projection. The SQL kind filter should keep it out of hydrate's decoder.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE conversation_input_queue SET payload_json='not-json' WHERE conversation_id=? AND kind=?`, "hydrate-cold-inputs", string(InputKindTrigger)); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err = store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	manager := NewManager("agent-1", nil, nil, nil, nil, st, TurnOptions{}, logx.Discard())
	defer manager.Stop()
	view, err := manager.GetHydrateView("hydrate-cold-inputs")
	if err != nil {
		t.Fatalf("cold hydrate failed on an internal trigger payload: %v", err)
	}
	if view.QueuePending != 2 {
		t.Fatalf("queued count=%d, want both user and internal queued rows", view.QueuePending)
	}
	if len(view.PendingInputs) != 2 {
		t.Fatalf("pending inputs=%+v, want only the two user rows", view.PendingInputs)
	}
	if view.PendingInputs[0].InputSeq != 1 || view.PendingInputs[0].Content != "visible queued message" || view.PendingInputs[0].Status != "queued" {
		t.Fatalf("first pending input=%+v", view.PendingInputs[0])
	}
	if view.PendingInputs[1].InputSeq != 3 || view.PendingInputs[1].Content != "visible claimed message" || view.PendingInputs[1].Status != "claimed" {
		t.Fatalf("second pending input=%+v", view.PendingInputs[1])
	}
}
