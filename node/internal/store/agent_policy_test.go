package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAgentPolicyAndPromptContextSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.db")
	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	if err := st.Save(ctx, AgentRecord{
		AgentID:     "agt-1",
		DisplayName: "A",
		TemplateID:  "general",
	}); err != nil {
		t.Fatal(err)
	}

	pol, err := st.EnsureAgentPolicy(ctx, "agt-1")
	if err != nil || pol == nil {
		t.Fatalf("ensure policy = %+v err=%v", pol, err)
	}
	if pol.Tools == nil {
		t.Fatal("tools map nil")
	}
	pol.Tools["write_file"] = "deny"
	if err := st.SaveAgentPolicy(ctx, *pol); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetAgentPolicy(ctx, "agt-1")
	if err != nil || got.Tools["write_file"] != "deny" {
		t.Fatalf("got policy = %+v err=%v", got, err)
	}

	pc, err := st.EnsureAgentPromptContext(ctx, "agt-1")
	if err != nil || pc == nil {
		t.Fatalf("ensure prompt = %+v err=%v", pc, err)
	}
	pc.SoulMD = "hello"
	if err := st.SaveAgentPromptContext(ctx, *pc); err != nil {
		t.Fatal(err)
	}
	gotPC, err := st.GetAgentPromptContext(ctx, "agt-1")
	if err != nil || gotPC.SoulMD != "hello" {
		t.Fatalf("got prompt = %+v err=%v", gotPC, err)
	}

	if err := st.SoftDelete(ctx, "agt-1"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetAgentPolicy(ctx, "agt-1"); got != nil {
		t.Fatal("policy should be deleted on soft delete")
	}
	if got, _ := st.GetAgentPromptContext(ctx, "agt-1"); got != nil {
		t.Fatal("prompt context should be deleted on soft delete")
	}
}
