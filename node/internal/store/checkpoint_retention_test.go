package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestCheckpointPruningRetainsCurrentAndLatestRoundBase(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "checkpoint-retention.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := st.db.Exec(`INSERT INTO conversations(conversation_id,node_id,revision,current_checkpoint_revision,lifecycle_covered_seq,history_completeness,created_at,updated_at) VALUES('checkpoint-retention','node',5,4,0,'complete',?,?)`, now, now); err != nil {
		t.Fatal(err)
	}
	for revision := uint64(0); revision <= 5; revision++ {
		if _, err := st.db.Exec(`INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES('checkpoint-retention',?,'[]','test',0,?)`, revision, now); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`INSERT INTO conversation_rounds(conversation_id,round_id,ordinal,selected_revision_id,pre_checkpoint_revision) VALUES('checkpoint-retention','round-old',1,'revision-old',0),('checkpoint-retention','round-latest',2,'revision-latest',3)`); err != nil {
		t.Fatal(err)
	}
	tx, err := st.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pruneConversationCheckpointsTx(context.Background(), tx, "checkpoint-retention", 4); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var retained []uint64
	rows, err := st.db.Query(`SELECT revision FROM context_checkpoints WHERE conversation_id=? ORDER BY revision`, "checkpoint-retention")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var revision uint64
		if err := rows.Scan(&revision); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		retained = append(retained, revision)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retained, []uint64{3, 4}) {
		t.Fatalf("retained checkpoints=%v, want latest round base and current [3 4]", retained)
	}
	base, err := st.LoadConversationCheckpointRevision(context.Background(), "checkpoint-retention", 3)
	if err != nil || base == nil || base.Revision != 3 || base.Lineage != "test" {
		t.Fatalf("load retained round base=%+v err=%v", base, err)
	}
	missing, err := st.LoadConversationCheckpointRevision(context.Background(), "checkpoint-retention", 2)
	if err != nil || missing != nil {
		t.Fatalf("intermediate checkpoint should be pruned: checkpoint=%+v err=%v", missing, err)
	}

	// Once a newer logical round is recorded, its pre-round checkpoint becomes
	// the only retained base and the previous round's base can be collected.
	if _, err := st.db.Exec(`UPDATE conversation_rounds SET pre_checkpoint_revision=4 WHERE conversation_id=? AND round_id=?`, "checkpoint-retention", "round-latest"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES('checkpoint-retention',5,'[]','test',0,?)`, now); err != nil {
		t.Fatal(err)
	}
	tx, err = st.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := pruneConversationCheckpointsTx(context.Background(), tx, "checkpoint-retention", 5); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	retained = nil
	rows, err = st.db.Query(`SELECT revision FROM context_checkpoints WHERE conversation_id=? ORDER BY revision`, "checkpoint-retention")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var revision uint64
		if err := rows.Scan(&revision); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		retained = append(retained, revision)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retained, []uint64{4, 5}) {
		t.Fatalf("retained checkpoints after new round=%v, want [4 5]", retained)
	}
}
