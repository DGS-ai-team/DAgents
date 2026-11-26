package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/queue"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
)

func TestInputQueueControlSurvivesColdRestore(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "input-queue-control.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	rt := newRuntime("queue-control-session", "agent-1", nil, &llm.MockClient{}, nil, nil, st, nil,
		nil, nil, nil, 0, 0, TurnOptions{}, nil)
	if _, err := rt.inputBox.Append(InputKindUser, queue.Envelope{RequestType: queue.RequestTypeMessage, Content: "keep queued"}); err != nil {
		t.Fatal(err)
	}
	if err := rt.persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	var before inputBoxState
	if err := json.Unmarshal(rt.inputBox.SnapshotForPersistence(), &before); err != nil {
		t.Fatal(err)
	}
	if revision, changed, err := rt.setInputQueuePaused(true, 1); err != nil || !changed || revision != 2 {
		t.Fatalf("pause revision=%d changed=%v err=%v", revision, changed, err)
	}

	record, err := st.Load(context.Background(), rt.session.ID)
	if err != nil || record == nil {
		t.Fatalf("load paused queue record=%v err=%v", record, err)
	}
	var persisted inputBoxState
	if err := json.Unmarshal(record.RuntimeState.InputBoxState, &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.QueuePaused || persisted.QueueControlRevision != 2 || persisted.QueueRevision != before.QueueRevision {
		t.Fatalf("persisted queue header=%+v before=%+v", persisted, before)
	}

	rows, err := st.LoadInputQueue(context.Background(), rt.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	restored := NewInputBox()
	if err := restored.RestoreQueueRows(record.RuntimeState.InputBoxState, rows); err != nil {
		t.Fatal(err)
	}
	paused, revision := restored.QueueControlState()
	if !paused || revision != 2 {
		t.Fatalf("restored queue control paused=%v revision=%d", paused, revision)
	}
	if claimed, ok := restored.PopForIdle(); ok {
		t.Fatalf("cold-restored paused queue claimed %+v", claimed)
	}
}
