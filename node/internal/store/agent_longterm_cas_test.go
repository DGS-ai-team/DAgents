package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestUpdateAgentLongTermCAS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.db")
	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	if err := st.Save(ctx, AgentRecord{AgentID: "agt-1", DisplayName: "A", TemplateID: "general"}); err != nil {
		t.Fatal(err)
	}
	pc, err := st.EnsureAgentPromptContext(ctx, "agt-1", "")
	if err != nil || pc == nil {
		t.Fatalf("ensure prompt = %+v err=%v", pc, err)
	}
	v1 := pc.UpdatedAt

	ok, err := st.UpdateAgentLongTermCAS(ctx, "agt-1", "memory v1", v1)
	if err != nil || !ok {
		t.Fatalf("first CAS = %v err=%v", ok, err)
	}
	got, err := st.GetAgentPromptContext(ctx, "agt-1")
	if err != nil || got.LongTermMD != "memory v1" {
		t.Fatalf("got = %+v err=%v", got, err)
	}

	ok, err = st.UpdateAgentLongTermCAS(ctx, "agt-1", "memory v2", v1)
	if err != nil || ok {
		t.Fatalf("stale CAS should fail: ok=%v err=%v", ok, err)
	}

	ok, err = st.UpdateAgentLongTermCAS(ctx, "agt-1", "memory v2", got.UpdatedAt)
	if err != nil || !ok {
		t.Fatalf("second CAS = %v err=%v", ok, err)
	}
	got, err = st.GetAgentPromptContext(ctx, "agt-1")
	if err != nil || got.LongTermMD != "memory v2" {
		t.Fatalf("got = %+v err=%v", got, err)
	}

	// zero time should never match a real row
	ok, err = st.UpdateAgentLongTermCAS(ctx, "agt-1", "memory v3", time.Time{})
	if err != nil || ok {
		t.Fatalf("zero version CAS should fail: ok=%v err=%v", ok, err)
	}
}
