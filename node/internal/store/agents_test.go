package store

import (
	"context"
	"encoding/json"
	"path/filepath"
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
		SandboxEnabled: false,
		SandboxBackend: "process",
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
