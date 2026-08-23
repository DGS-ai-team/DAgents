package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestSkillControlPlaneReportsDiagnosticsAndIdleBoundary(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "writer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := tools.NewRegistry(t.TempDir(), 30)
	if err != nil {
		t.Fatal(err)
	}
	pol, _ := policy.LoadFile("")
	mgr := NewManager("agent-1", stream.NewHub(16, logx.Discard()), &llm.MockClient{}, reg, pol, nil, TurnOptions{
		SkillsRoot:        root,
		SkillsEnabled:     true,
		SkillsMaxInPrompt: 2,
	}, logx.Discard())
	defer mgr.Stop()
	sess, _, err := mgr.Create("")
	if err != nil {
		t.Fatal(err)
	}

	loaded, err := mgr.LoadSessionSkillDetailed(sess.ID, "writer")
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Changed || len(loaded.Loaded) != 1 || loaded.Loaded[0].SkillName != "writer" {
		t.Fatalf("loaded outcome = %+v", loaded)
	}
	if loaded.SessionStateAppliedBoundary != "immediate" || loaded.ModelContextAppliedBoundary != "next_human_turn" {
		t.Fatalf("loaded boundaries = %+v", loaded)
	}
	if loaded.Rejected == nil || len(loaded.Rejected) != 0 || loaded.HooksLoaded == nil || loaded.HooksFailed == nil {
		t.Fatalf("loaded empty arrays = %+v", loaded)
	}

	missing, err := mgr.LoadSessionSkillDetailed(sess.ID, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if missing.Changed || missing.ModelContextAppliedBoundary != "unchanged" || len(missing.Rejected) != 1 || missing.Rejected[0].Reason != "not_found" {
		t.Fatalf("missing outcome = %+v", missing)
	}

	unloaded, err := mgr.UnloadSessionSkillDetailed(sess.ID, "missing")
	if err != nil {
		t.Fatal(err)
	}
	if unloaded.Changed || len(unloaded.Rejected) != 1 || unloaded.Rejected[0].Reason != "not_loaded" {
		t.Fatalf("unload missing outcome = %+v", unloaded)
	}
}
