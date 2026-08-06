package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrateAgentPoliciesMergeSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.db")
	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	if err := st.SaveAgentPolicy(ctx, AgentPolicyRecord{
		AgentID: "agt-old",
		Tools: map[string]string{
			"write_file":       "deny",
			"browser_navigate": "always",
		},
		Shell:     map[string]map[string]string{},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAgentPolicy(ctx, AgentPolicyRecord{
		AgentID: "agt-fresh",
		Tools: map[string]string{
			"browser_run_task":    "always",
			"browser_task_status": "never",
			"browser_task_cancel": "always",
			"write_file":          "rule",
		},
		Shell:     map[string]map[string]string{},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := st.MigrateAgentPoliciesMergeSeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.AgentsTouched < 1 {
		t.Fatalf("AgentsTouched = %d, want >= 1", result.AgentsTouched)
	}
	if result.ToolsAdded < 1 {
		t.Fatalf("ToolsAdded = %d, want >= 1", result.ToolsAdded)
	}

	old, err := st.GetAgentPolicy(ctx, "agt-old")
	if err != nil || old == nil {
		t.Fatalf("get old: %+v %v", old, err)
	}
	if old.Tools["write_file"] != "deny" {
		t.Fatalf("must keep write_file=deny, got %q", old.Tools["write_file"])
	}
	if old.Tools["browser_run_task"] != "always" {
		t.Fatalf("browser_run_task = %q", old.Tools["browser_run_task"])
	}
	if old.Tools["browser_task_status"] != "never" {
		t.Fatalf("browser_task_status = %q", old.Tools["browser_task_status"])
	}

	fresh, err := st.GetAgentPolicy(ctx, "agt-fresh")
	if err != nil || fresh == nil {
		t.Fatalf("get fresh: %+v %v", fresh, err)
	}
	if fresh.Tools["write_file"] != "rule" {
		t.Fatalf("fresh write_file changed: %q", fresh.Tools["write_file"])
	}
	if fresh.Tools["browser_run_task"] != "always" {
		t.Fatalf("fresh browser_run_task = %q", fresh.Tools["browser_run_task"])
	}

	result2, err := st.MigrateAgentPoliciesMergeSeed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result2.AgentsTouched != 0 || result2.ToolsAdded != 0 {
		t.Fatalf("second migrate not idempotent: %+v", result2)
	}
}

func TestEnsureAgentPolicyMergesSeedOnExisting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.db")
	st, err := OpenAgents(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()

	if err := st.SaveAgentPolicy(ctx, AgentPolicyRecord{
		AgentID: "agt-1",
		Tools: map[string]string{
			"bash_run": "rule",
		},
		Shell:     map[string]map[string]string{},
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	pol, err := st.EnsureAgentPolicy(ctx, "agt-1", "")
	if err != nil || pol == nil {
		t.Fatalf("ensure = %+v err=%v", pol, err)
	}
	if pol.Tools["browser_run_task"] != "always" {
		t.Fatalf("expected seed merge on ensure, tools=%v", pol.Tools)
	}
	if pol.Tools["bash_run"] != "rule" {
		t.Fatalf("bash_run overwritten: %q", pol.Tools["bash_run"])
	}
}
