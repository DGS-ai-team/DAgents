package hooks

import (
	"context"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/policy"
)

func TestLoadedSkillFileGuard_blocksWriteFile(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	hc := BuildToolBeforeEachContext(ToolBeforeEachInput{
		SessionID: "s1",
		ToolName:  "write_file",
		ToolArgs:  map[string]any{"path": "skills/writer/SKILL.md", "content": "x"},
	})
	hc.LoadedSkills = []LoadedSkillInfo{{Name: "writer", Description: "d"}}
	out, err := reg.RunPhase(context.Background(), PhaseToolBeforeEach, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	decision := ToolBeforeEachDecisionFrom(out)
	if decision.Action != policy.ActionDeny {
		t.Fatalf("action = %q, want deny", decision.Action)
	}
	if decision.ApprovalReason != LoadedSkillFileDenyMessage {
		t.Fatalf("reason = %q", decision.ApprovalReason)
	}
}

func TestLoadedSkillFileGuard_allowsUnloadedSkillPath(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	hc := BuildToolBeforeEachContext(ToolBeforeEachInput{
		SessionID: "s1",
		ToolName:  "write_file",
		ToolArgs:  map[string]any{"path": "skills/draft/SKILL.md", "content": "x"},
	})
	hc.LoadedSkills = []LoadedSkillInfo{{Name: "writer", Description: "d"}}
	out, err := reg.RunPhase(context.Background(), PhaseToolBeforeEach, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	decision := ToolBeforeEachDecisionFrom(out)
	if decision.Action == policy.ActionDeny {
		t.Fatalf("unexpected deny: %q", decision.ApprovalReason)
	}
}

func TestLoadedSkillFileGuard_blocksBashMutatingCommand(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	hc := BuildToolBeforeEachContext(ToolBeforeEachInput{
		SessionID: "s1",
		ToolName:  "bash_run",
		ToolArgs:  map[string]any{"command": "Set-Content skills/writer/out.txt 'x'"},
	})
	hc.LoadedSkills = []LoadedSkillInfo{{Name: "writer"}}
	out, err := reg.RunPhase(context.Background(), PhaseToolBeforeEach, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	decision := ToolBeforeEachDecisionFrom(out)
	if decision.Action != policy.ActionDeny {
		t.Fatalf("action = %q, want deny", decision.Action)
	}
}

func TestLoadedSkillFileGuard_allowsBashReadOnly(t *testing.T) {
	reg := NewRegistry(nil, RuntimeConfig{})
	hc := BuildToolBeforeEachContext(ToolBeforeEachInput{
		SessionID: "s1",
		ToolName:  "bash_run",
		ToolArgs:  map[string]any{"command": "Get-Content skills/writer/SKILL.md"},
	})
	hc.LoadedSkills = []LoadedSkillInfo{{Name: "writer"}}
	out, err := reg.RunPhase(context.Background(), PhaseToolBeforeEach, hc, NoopHost())
	if err != nil {
		t.Fatal(err)
	}
	decision := ToolBeforeEachDecisionFrom(out)
	if decision.Action == policy.ActionDeny {
		t.Fatalf("unexpected deny: %q", decision.ApprovalReason)
	}
}

func TestToolDenyMessage(t *testing.T) {
	if got := ToolDenyMessage(ToolBeforeEachResult{ApprovalReason: LoadedSkillFileDenyMessage}); got != LoadedSkillFileDenyMessage {
		t.Fatalf("got %q", got)
	}
	if got := ToolDenyMessage(ToolBeforeEachResult{}); got != "rejected: policy_denied" {
		t.Fatalf("got %q", got)
	}
}

func TestRelPathTouchesLoadedSkill(t *testing.T) {
	cases := map[string]bool{
		"skills/writer/SKILL.md":      true,
		".runtime/skills/writer/x.md": true,
		"skills/writer":               true,
		"skills/other/x.md":           false,
		"notes/writer.txt":            false,
	}
	for path, want := range cases {
		got := relPathTouchesLoadedSkill(path, []string{"writer"})
		if got != want {
			t.Fatalf("%q => %v, want %v", path, got, want)
		}
	}
}
