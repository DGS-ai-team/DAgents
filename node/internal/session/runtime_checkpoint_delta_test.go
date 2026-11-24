package session

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/store"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func TestCommitStepHistoryAppliesRecordedMutationsIncrementally(t *testing.T) {
	r := &runtime{
		messages:           []llm.Message{{Role: "assistant", Content: "archived prefix"}, {Role: "user", Content: "original"}},
		activeContextStart: 1,
	}
	history := []llm.Message{{Role: "user", Content: "original"}}

	appendMessage := r.trackHistoryMutation(turn.HistoryMutationAppend, 1, llm.Message{Role: "assistant", Content: "answer"})
	history = append(history, appendMessage)
	insertMessage := r.trackHistoryMutation(turn.HistoryMutationInsert, 0, llm.Message{Role: "system", Content: "injected"})
	history = append(history, llm.Message{})
	copy(history[1:], history[:len(history)-1])
	history[0] = insertMessage
	replaceMessage := r.trackHistoryMutation(turn.HistoryMutationReplace, 1, llm.Message{Role: "user", Content: "edited"})
	history[1] = replaceMessage

	if !r.commitStepHistory(&history) {
		t.Fatal("commitStepHistory did not apply captured mutations")
	}
	want := []llm.Message{
		{Role: "assistant", Content: "archived prefix"},
		insertMessage,
		replaceMessage,
		appendMessage,
	}
	if !reflect.DeepEqual(r.messages, want) {
		t.Fatalf("runtime messages=%+v want %+v", r.messages, want)
	}
	if r.historyRevision != 1 {
		t.Fatalf("history revision=%d, want one atomic step commit", r.historyRevision)
	}
	if r.checkpointFullReplaceSequence != 0 || len(r.checkpointMutations) != 3 {
		t.Fatalf("ordinary writes forced a full checkpoint: full=%d mutations=%d", r.checkpointFullReplaceSequence, len(r.checkpointMutations))
	}
	if r.commitStepHistory(&history) {
		t.Fatal("replaying the same history snapshot committed twice")
	}
	if r.historyRevision != 1 {
		t.Fatalf("duplicate history commit advanced revision to %d", r.historyRevision)
	}
}

func TestRuntimePersistsOrdinaryHistoryAsCheckpointDelta(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "runtime-checkpoint-delta.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	baseMessages := []llm.Message{{Role: "user", Content: "first"}}
	base, err := st.PersistRuntimeDelta(ctx, store.Record{AgentID: "runtime-checkpoint-delta", NodeID: "node"}, &store.ConversationDelta{
		ConversationID: "runtime-checkpoint-delta", NodeID: "node",
		Checkpoint: &store.CheckpointCommit{Messages: baseMessages, Lineage: "base"},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &runtime{
		session:                   Session{ID: "runtime-checkpoint-delta", AgentID: "node"},
		store:                     st,
		messages:                  append([]llm.Message(nil), baseMessages...),
		conversationRevision:      base.ConversationRevision,
		contextCheckpointRevision: base.CheckpointRevision,
	}
	history := append([]llm.Message(nil), baseMessages...)
	message := r.trackHistoryMutation(turn.HistoryMutationAppend, len(history), llm.Message{Role: "assistant", Content: "reply"})
	history = append(history, message)
	if !r.commitStepHistory(&history) {
		t.Fatal("history mutation was not committed in memory")
	}
	if err := r.persist(ctx); err != nil {
		t.Fatalf("persist delta checkpoint: %v", err)
	}
	checkpoint, err := st.LoadConversationCheckpoint(ctx, r.session.ID)
	if err != nil || checkpoint == nil || len(checkpoint.Messages) != 2 || checkpoint.Messages[1].Content != "reply" {
		t.Fatalf("restored checkpoint=%+v err=%v", checkpoint, err)
	}
	var kind, messagesJSON, deltaJSON string
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`SELECT checkpoint_kind,messages_json,delta_json FROM context_checkpoints WHERE conversation_id=? AND revision=?`, r.session.ID, checkpoint.Revision).Scan(&kind, &messagesJSON, &deltaJSON); err != nil {
		t.Fatal(err)
	}
	if kind != "delta" || messagesJSON != "[]" || len(deltaJSON) == 0 {
		t.Fatalf("ordinary update stored kind=%q messages=%q delta=%q", kind, messagesJSON, deltaJSON)
	}
}
