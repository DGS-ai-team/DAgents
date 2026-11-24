package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/turn"
)

func conversationResubmitStartFixture(t *testing.T, st *SQLiteStore) ([]turn.TurnEventEnvelope, RoundRevision, RuntimeState) {
	t.Helper()
	const conversationID = "resubmit-agent"
	seedConversationHistory(t, st, conversationID, 1)
	if _, err := st.db.Exec(`INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES(?,1,'[]','live_append',0,?)`, conversationID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	start := turn.NewTurnEventEnvelope(conversationID, turn.EventTurnStarted, now)
	start.AgentID, start.TurnID, start.Source, start.CommandID = conversationID, "turn-resubmit", string(turn.TurnSourceHuman), "resubmit-start-turn"
	start.Payload = json.RawMessage(`{"command_type":"start_turn","generation":2}`)
	step := turn.NewTurnEventEnvelope(conversationID, turn.EventStepStarted, now.Add(time.Millisecond))
	step.AgentID, step.TurnID, step.StepID, step.Source, step.CommandID = conversationID, "turn-resubmit", "turn-resubmit-step-1", string(turn.TurnSourceHuman), "resubmit-start-step"
	step.Payload = json.RawMessage(`{"command_type":"start_step","generation":2}`)
	revision := RoundRevision{
		ConversationID: conversationID, RoundID: "round-1", RevisionID: "revision-new",
		TurnID: "turn-resubmit", Source: string(turn.TurnSourceHuman), Generation: 2, Status: "active",
		ResubmitFromRevisionID: "revision-1", ExpectedConversationRevision: 10,
		ExpectedPreCheckpointRevision: 1,
		ResubmitCheckpoint:            &CheckpointCommit{Messages: []llm.Message{{Role: "user", Content: "pre-round base"}}, Lineage: "resubmit_base"},
	}
	return []turn.TurnEventEnvelope{start, step}, revision, RuntimeState{}
}

func TestConversationResubmitStartBoundaryKeepsOldRevisionAndCommitsNewBase(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "resubmit-start.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	events, revision, runtimeState := conversationResubmitStartFixture(t, st)
	if _, err := st.AppendTurnStartBoundary(context.Background(), events, revision, runtimeState); err != nil {
		t.Fatalf("append resubmit start boundary: %v", err)
	}
	var conversationRevision, checkpointRevision uint64
	var selectedRevision string
	if err := st.db.QueryRow(`SELECT revision,current_checkpoint_revision FROM conversations WHERE conversation_id=?`, revision.ConversationID).Scan(&conversationRevision, &checkpointRevision); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT selected_revision_id FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, revision.ConversationID, revision.RoundID).Scan(&selectedRevision); err != nil {
		t.Fatal(err)
	}
	if conversationRevision != 11 || checkpointRevision != 2 || selectedRevision != revision.RevisionID {
		t.Fatalf("conversation revision=%d checkpoint=%d selected=%q", conversationRevision, checkpointRevision, selectedRevision)
	}
	var oldStatus, newStatus string
	if err := st.db.QueryRow(`SELECT status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, revision.ConversationID, revision.RoundID, "revision-1").Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT status FROM conversation_round_revisions WHERE conversation_id=? AND round_id=? AND revision_id=?`, revision.ConversationID, revision.RoundID, revision.RevisionID).Scan(&newStatus); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "completed" || newStatus != "active" {
		t.Fatalf("old/new revision status=%q/%q", oldStatus, newStatus)
	}
	checkpoint, err := st.LoadConversationCheckpointRevision(context.Background(), revision.ConversationID, 2)
	if err != nil || checkpoint == nil || checkpoint.Lineage != "resubmit_base" || len(checkpoint.Messages) != 1 || checkpoint.Messages[0].Content != "pre-round base" {
		t.Fatalf("restored base checkpoint=%+v err=%v", checkpoint, err)
	}
	oldItems, err := st.LoadConversationRoundItems(context.Background(), revision.ConversationID, revision.RoundID, "revision-1", "", 20)
	if err != nil || oldItems.Selected || len(oldItems.Items) < 2 || oldItems.Items[0].Payload.Content != "request 1" {
		t.Fatalf("old immutable revision items=%+v err=%v", oldItems, err)
	}
}

func TestConversationResubmitStartBoundaryRollsBackOnLifecycleWriteFailure(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "resubmit-rollback.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	events, revision, runtimeState := conversationResubmitStartFixture(t, st)
	if _, err := st.db.Exec(`CREATE TRIGGER fail_resubmit_start_event BEFORE INSERT ON turn_events BEGIN SELECT RAISE(ABORT,'injected resubmit event failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTurnStartBoundary(context.Background(), events, revision, runtimeState); err == nil {
		t.Fatal("expected injected lifecycle write failure")
	}
	var conversationRevision, checkpointRevision uint64
	var selectedRevision string
	if err := st.db.QueryRow(`SELECT revision,current_checkpoint_revision FROM conversations WHERE conversation_id=?`, revision.ConversationID).Scan(&conversationRevision, &checkpointRevision); err != nil {
		t.Fatal(err)
	}
	if err := st.db.QueryRow(`SELECT selected_revision_id FROM conversation_rounds WHERE conversation_id=? AND round_id=?`, revision.ConversationID, revision.RoundID).Scan(&selectedRevision); err != nil {
		t.Fatal(err)
	}
	var revisions, checkpoints, lifecycleEvents, runtimes int
	for query, dest := range map[string]*int{
		`SELECT COUNT(*) FROM conversation_round_revisions WHERE conversation_id=?`: &revisions,
		`SELECT COUNT(*) FROM context_checkpoints WHERE conversation_id=?`:          &checkpoints,
		`SELECT COUNT(*) FROM turn_events WHERE session_id=?`:                       &lifecycleEvents,
		`SELECT COUNT(*) FROM agent_runtimes WHERE agent_id=?`:                      &runtimes,
	} {
		if err := st.db.QueryRow(query, revision.ConversationID).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	if conversationRevision != 10 || checkpointRevision != 1 || selectedRevision != "revision-1" || revisions != 1 || checkpoints != 1 || lifecycleEvents != 0 || runtimes != 0 {
		t.Fatalf("partial resubmit escaped rollback: conversation=%d checkpoint=%d selected=%q revisions=%d checkpoints=%d events=%d runtimes=%d", conversationRevision, checkpointRevision, selectedRevision, revisions, checkpoints, lifecycleEvents, runtimes)
	}
}
