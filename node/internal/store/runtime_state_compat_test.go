package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeStateIgnoresLegacyLifecycleCoverageField(t *testing.T) {
	var state RuntimeState
	if err := json.Unmarshal([]byte(`{"history_revision":4,"lifecycle_covered_seq":99}`), &state); err != nil {
		t.Fatalf("decode legacy runtime state: %v", err)
	}
	if state.HistoryRevision != 4 {
		t.Fatalf("history revision=%d, want 4", state.HistoryRevision)
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encode runtime state: %v", err)
	}
	if strings.Contains(string(raw), "lifecycle_covered_seq") {
		t.Fatalf("runtime state still emits unused lifecycle coverage field: %s", raw)
	}
}

func TestCheckpointReadIgnoresReservedLegacyLifecycleCoverageColumn(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "legacy-coverage.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.db.ExecContext(context.Background(), `INSERT INTO context_checkpoints(conversation_id,revision,messages_json,lineage,lifecycle_covered_seq,created_at) VALUES('legacy-coverage','7','[]','test',99,'2026-11-17T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	cp, err := st.LoadConversationCheckpointRevision(context.Background(), "legacy-coverage", 7)
	if err != nil || cp == nil || cp.Revision != 7 || cp.Lineage != "test" || len(cp.Messages) != 0 {
		t.Fatalf("load checkpoint with legacy coverage metadata=%+v err=%v", cp, err)
	}
}
