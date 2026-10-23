package turn

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/logx"
	"github.com/DGS-ai-team/DAgents/node/internal/policy"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
	"github.com/DGS-ai-team/DAgents/node/internal/stream"
	"github.com/DGS-ai-team/DAgents/node/internal/tools"
)

func TestLoadedSkillFileCanBeEditedAndNextTurnReadsUpdatedBody(t *testing.T) {
	root := t.TempDir()
	skillsRoot := filepath.Join(root, "skills")
	writeLifecycleSkill(t, skillsRoot, "writer", "---\nname: writer\ndescription: Write docs\n---\nOriginal body.\n")
	activeCatalog := skills.NewCatalog(skillsRoot, true, 2).NewTurnView()
	loaded := activeCatalog.SetLoadedSkills([]string{"writer"})
	if len(loaded) != 1 {
		t.Fatalf("loaded skills = %+v", loaded)
	}

	executor, err := tools.NewRegistry(root, 30)
	if err != nil {
		t.Fatal(err)
	}
	policyEngine := policy.NewEngineFromMaps(policy.Maps{Tools: map[string]policy.ApprovalMode{
		"write_file": policy.ModeNever,
	}})
	orch := NewOrchestrator("agent-1", root, stream.NewHub(8, logx.Discard()), &llm.MockClient{}, executor, policyEngine, SkillAccess{
		Catalog: activeCatalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, nil, nil, hooks.RuntimeConfig{}, logx.Discard())

	updatedBody := "---\nname: writer\ndescription: Write docs\n---\nUpdated body.\n"
	arguments, err := json.Marshal(map[string]string{
		"path":    "skills/writer/SKILL.md",
		"content": updatedBody,
	})
	if err != nil {
		t.Fatal(err)
	}
	call := llm.ToolCall{
		ID:   "edit-loaded-skill",
		Type: "function",
		Function: llm.ToolCallFunction{
			Name:      "write_file",
			Arguments: string(arguments),
		},
	}
	history := []llm.Message{}
	if _, _, err := orch.processToolCalls(context.Background(), "session-1", &history, []llm.ToolCall{call}); err != nil {
		t.Fatal(err)
	}
	if len(history) == 0 || !strings.Contains(history[len(history)-1].Content, "wrote ") {
		t.Fatalf("write result = %+v", history)
	}
	written, err := os.ReadFile(filepath.Join(skillsRoot, "writer", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(written) != updatedBody {
		t.Fatalf("written skill body = %q, want %q", written, updatedBody)
	}

	// The active Turn keeps its frozen body snapshot stable after the write.
	activeBody := activeCatalog.ReadLoadedSkillContents(loaded)
	if len(activeBody) != 1 || !strings.Contains(activeBody[0].Content, "Original body.") {
		t.Fatalf("active Turn body changed unexpectedly: %+v", activeBody)
	}

	// A later Turn sees the edited body through its new Catalog view.
	nextTurnCatalog := skills.NewCatalog(skillsRoot, true, 2).NewTurnView()
	nextTurnLoaded := nextTurnCatalog.SetLoadedSkills([]string{"writer"})
	nextTurnBody := nextTurnCatalog.ReadLoadedSkillContents(nextTurnLoaded)
	if len(nextTurnBody) != 1 || !strings.Contains(nextTurnBody[0].Content, "Updated body.") {
		t.Fatalf("next Turn body did not observe update: %+v", nextTurnBody)
	}
}
