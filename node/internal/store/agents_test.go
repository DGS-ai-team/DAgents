package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentStore_CRUD(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.db")
	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	snap, _ := json.Marshal(map[string]any{"llm": map[string]any{"model": "x"}})
	rec := AgentRecord{
		AgentID:        "agt-1",
		DisplayName:    "测试助手",
		TemplateID:     "general",
		ConfigSnapshot: snap,
	}
	if err := st.Save(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(ctx, "agt-1")
	if err != nil || got == nil {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	if got.DisplayName != "测试助手" || got.TemplateID != "general" {
		t.Fatalf("got = %+v", got)
	}
	got.DisplayName = "改名"
	if err := st.Save(ctx, *got); err != nil {
		t.Fatal(err)
	}
	list, err := st.List(ctx)
	if err != nil || len(list) != 1 || list[0].DisplayName != "改名" {
		t.Fatalf("list = %+v err=%v", list, err)
	}
	if err := st.SoftDelete(ctx, "agt-1"); err != nil {
		t.Fatal(err)
	}
	list, err = st.List(ctx)
	if err != nil || len(list) != 0 {
		t.Fatalf("after delete list = %+v", list)
	}
}

func TestAgentStore_HostJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents-host.db")
	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	ctx := context.Background()
	rec := AgentRecord{
		AgentID:        "agt-host",
		DisplayName:    "主机信息",
		TemplateID:     "",
		ConfigSnapshot: json.RawMessage(`{}`),
		HostJSON:       json.RawMessage(`{"os_kind":"linux","display_label":"Linux","display_available":false}`),
	}
	if err := st.Save(ctx, rec); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get(ctx, "agt-host")
	if err != nil || got == nil {
		t.Fatalf("get = %+v err=%v", got, err)
	}
	if string(got.HostJSON) == "" || !strings.Contains(string(got.HostJSON), "Linux") {
		t.Fatalf("host = %s", got.HostJSON)
	}
}

func TestAgentRuntimeRevisionIsIndependentAndMonotonic(t *testing.T) {
	st, err := OpenAgents(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	rec := AgentRecord{AgentID: "agent-revision", DisplayName: "Revision"}
	if err := st.Save(ctx, rec); err != nil {
		t.Fatal(err)
	}
	first, err := st.Get(ctx, rec.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if first.RuntimeRevision != 1 {
		t.Fatalf("first runtime revision = %d, want 1", first.RuntimeRevision)
	}
	// Keep UpdatedAt fixed to prove the runtime revision does not depend on
	// timestamp precision or clock changes.
	rec.UpdatedAt = first.UpdatedAt
	if err := st.Save(ctx, rec); err != nil {
		t.Fatal(err)
	}
	second, err := st.Get(ctx, rec.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	if second.RuntimeRevision != 2 {
		t.Fatalf("second runtime revision = %d, want 2", second.RuntimeRevision)
	}
}

func TestAgentStore_MigratesLegacyAgentColumnsWithoutDroppingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-agents.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE agents (
		agent_id TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		template_id TEXT NOT NULL,
		origin TEXT NOT NULL DEFAULT 'local',
		sandbox_enabled INTEGER NOT NULL DEFAULT 0,
		sandbox_backend TEXT NOT NULL DEFAULT 'process',
		config_snapshot_json TEXT NOT NULL DEFAULT '{}',
		archived INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO agents (
		agent_id, display_name, template_id, origin, sandbox_enabled,
		sandbox_backend, config_snapshot_json, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"agt-legacy", "Legacy Agent", "general", "remote", 1, "process",
		`{"llm":{"model":"legacy-model"}}`, "2026-07-01T00:00:00Z", "2026-07-02T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	got, err := st.Get(ctx, "agt-legacy")
	if err != nil || got == nil {
		t.Fatalf("get legacy agent = %+v err=%v", got, err)
	}
	if got.DisplayName != "Legacy Agent" || got.HostJSON == nil || string(got.HostJSON) != "{}" || got.RuntimeRevision != 1 {
		t.Fatalf("migrated legacy agent = %+v", got)
	}
	var origin, backend string
	var sandboxEnabled int
	if err := st.db.QueryRow(`SELECT origin, sandbox_backend, sandbox_enabled FROM agents WHERE agent_id = ?`, "agt-legacy").Scan(&origin, &backend, &sandboxEnabled); err != nil {
		t.Fatal(err)
	}
	if origin != "remote" || backend != "process" || sandboxEnabled != 1 {
		t.Fatalf("legacy columns changed: origin=%q backend=%q sandbox_enabled=%d", origin, backend, sandboxEnabled)
	}
	if err := st.Save(ctx, *got); err != nil {
		t.Fatal(err)
	}
	got, err = st.Get(ctx, "agt-legacy")
	if err != nil || got == nil || got.RuntimeRevision != 2 {
		t.Fatalf("saved legacy agent = %+v err=%v", got, err)
	}
}
