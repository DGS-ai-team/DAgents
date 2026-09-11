package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestChildRunStoreRoundTripAndUpsert(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "child-runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	progress := json.RawMessage(`{"status":"active","phase":"tool_executing","revision":2}`)
	record := ChildRunRecord{
		ChildAgentID:  "child-1",
		ParentAgentID: "parent-1",
		NodeID:        "node-1",
		ToolCallID:    "call-1",
		Purpose:       "inspect files",
		Status:        "active",
		Phase:         "tool_executing",
		AllowedTools:  []string{"read_file", "bash_run"},
		LoadedSkills:  []string{"writer"},
		Progress:      progress,
		TurnCount:     2,
		MaxTurns:      8,
		CreatedAt:     now,
		ExpiresAt:     now.Add(time.Hour),
		UpdatedAt:     now,
		Revision:      2,
	}
	if err := st.SaveChildRun(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	record.Status = "completed"
	record.Phase = "completed"
	record.Summary = "done"
	record.FinishedAt = now.Add(time.Minute)
	record.Revision = 3
	if err := st.SaveChildRun(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	loaded, err := st.LoadChildRun(context.Background(), "child-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil || loaded.Status != "completed" || loaded.Revision != 3 || loaded.Summary != "done" {
		t.Fatalf("loaded child run = %#v", loaded)
	}
	if string(loaded.Progress) != string(progress) || len(loaded.AllowedTools) != 2 || loaded.LoadedSkills[0] != "writer" {
		t.Fatalf("loaded child run payload = %#v", loaded)
	}

	items, err := st.ListChildRuns(context.Background(), "parent-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ChildAgentID != "child-1" {
		t.Fatalf("listed child runs = %#v", items)
	}
}
