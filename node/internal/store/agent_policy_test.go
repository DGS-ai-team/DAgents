package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestAgentPolicyIgnoresLegacyGrantPayload(t *testing.T) {
	st, err := OpenAgents(filepath.Join(t.TempDir(), "agents.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	if err := st.Save(ctx, AgentRecord{AgentID: "agt-legacy", DisplayName: "Legacy", TemplateID: "general"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `INSERT INTO agent_policy (agent_id, policy_json, updated_at) VALUES (?, ?, ?)`, "agt-legacy", `{"tools":{"write_file":"deny"},"shell":{},"grants":[{"id":"legacy","tools":["write_file"]}]}`, "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetAgentPolicy(ctx, "agt-legacy")
	if err != nil || got.Tools["write_file"] != "deny" {
		t.Fatalf("legacy policy = %+v err=%v", got, err)
	}
	if err := st.SaveAgentPolicy(ctx, *got); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := st.db.QueryRowContext(ctx, `SELECT policy_json FROM agent_policy WHERE agent_id = ?`, "agt-legacy").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw == "" {
		t.Fatalf("policy payload not rewritten: %s", raw)
	}
	var saved agentPolicyJSON
	if err := json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Tools["write_file"] != "deny" || strings.Contains(raw, "grants") {
		t.Fatalf("legacy grants were not discarded or policy was lost: %s", raw)
	}
}
