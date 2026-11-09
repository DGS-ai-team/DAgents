package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestChildRunStoreWritesIntoLegacySchemaWithDormantMaxTurnsColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-child-runs.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE child_runs (
	child_agent_id TEXT PRIMARY KEY, parent_agent_id TEXT NOT NULL, node_id TEXT NOT NULL DEFAULT '',
	tool_call_id TEXT NOT NULL DEFAULT '', purpose TEXT NOT NULL DEFAULT '', status TEXT NOT NULL,
	phase TEXT NOT NULL DEFAULT '', allowed_tools_json TEXT NOT NULL DEFAULT '[]',
	loaded_skills_json TEXT NOT NULL DEFAULT '[]', progress_json TEXT NOT NULL DEFAULT '{}',
	turn_count INTEGER NOT NULL DEFAULT 0, max_turns INTEGER NOT NULL DEFAULT 0,
	summary TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL, updated_at TEXT NOT NULL, finished_at TEXT NOT NULL DEFAULT '',
	revision INTEGER NOT NULL DEFAULT 0)`)
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
	now := time.Now().UTC()
	if err := st.SaveChildRun(context.Background(), ChildRunRecord{
		ChildAgentID: "child-legacy", ParentAgentID: "parent", Status: "active",
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), UpdatedAt: now,
	}); err != nil {
		t.Fatalf("save against legacy schema: %v", err)
	}
	loaded, err := st.LoadChildRun(context.Background(), "child-legacy")
	if err != nil || loaded == nil || loaded.Status != "active" {
		t.Fatalf("load legacy row = %#v, err=%v", loaded, err)
	}
}

func TestChildRunStoreRoundTripAndUpsert(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "child-runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	rows, err := st.db.Query(`PRAGMA table_info(child_runs)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, kind string
		var def any
		if err := rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "max_turns" {
			t.Fatal("new child_runs schema must not declare retired max_turns")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

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
