package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

func TestEnsureLoadedSkillInstructionsInsertsBeforeCurrentRootUser(t *testing.T) {
	root := t.TempDir()
	writeSkillInjectionTestSkill(t, root, "writer", "Write clearly.")
	catalog := skills.NewCatalog(root, true, 2).NewTurnView()
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	o := NewOrchestrator("agent-1", t.TempDir(), nil, &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{}, nil)
	history := []llm.Message{llm.UserMessage("请写一份文档", llm.UserNameHuman)}

	o.ensureLoadedSkillInstructions("session-1", &history)
	if len(history) != 2 || history[0].Name != llm.UserNameSkill || history[1].Name != llm.UserNameHuman {
		t.Fatalf("history = %+v", history)
	}
	if !strings.Contains(history[0].Content, "Write clearly.") || !strings.Contains(history[0].Content, "<content_digest>") {
		t.Fatalf("skill message = %q", history[0].Content)
	}

	o.ensureLoadedSkillInstructions("session-1", &history)
	if len(history) != 2 {
		t.Fatalf("skill instruction duplicated: %+v", history)
	}
}

func TestEnsureLoadedSkillInstructionsAppendsAfterSkillToolResult(t *testing.T) {
	root := t.TempDir()
	writeSkillInjectionTestSkill(t, root, "writer", "Write clearly.")
	catalog := skills.NewCatalog(root, true, 2).NewTurnView()
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	o := NewOrchestrator("agent-1", t.TempDir(), nil, &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{}, nil)
	history := []llm.Message{
		llm.UserMessage("加载写作能力", llm.UserNameHuman),
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "load-1", Function: llm.ToolCallFunction{Name: "load_skills"}}}},
		llm.ToolResultMessage("load-1", "load_skills", `{"loaded_skills":[{"skill_name":"writer"}]}`),
	}

	o.ensureLoadedSkillInstructions("session-1", &history)
	if len(history) != 4 || history[3].Name != llm.UserNameSkill {
		t.Fatalf("skill activation position = %+v", history)
	}
}

func TestEnsureLoadedSkillInstructionsRestoresAfterCompression(t *testing.T) {
	root := t.TempDir()
	writeSkillInjectionTestSkill(t, root, "writer", "Write clearly.")
	catalog := skills.NewCatalog(root, true, 2).NewTurnView()
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	o := NewOrchestrator("agent-1", t.TempDir(), nil, &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{}, nil)

	// Compression replaces the old skill message with a summary. The durable
	// loaded set is the source of truth, so the next model step must restore the
	// current body before the next human message is sent.
	history := []llm.Message{
		llm.UserMessage("此前已完成写作任务的摘要", llm.UserNameCompression),
		llm.UserMessage("继续写作", llm.UserNameHuman),
	}
	o.ensureLoadedSkillInstructions("session-1", &history)
	if len(history) != 3 || history[1].Name != llm.UserNameSkill || !strings.Contains(history[1].Content, "Write clearly.") {
		t.Fatalf("restored skill context = %+v", history)
	}

	o.ensureLoadedSkillInstructions("session-1", &history)
	if len(history) != 3 {
		t.Fatalf("restored skill context duplicated after compression: %+v", history)
	}
}

func TestFilterSkillInstructionMessagesRemovesStaleAndUnloadedBodies(t *testing.T) {
	root := t.TempDir()
	writeSkillInjectionTestSkill(t, root, "writer", "new body")
	catalog := skills.NewCatalog(root, true, 2).NewTurnView()
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	o := NewOrchestrator("agent-1", t.TempDir(), nil, &llm.MockClient{}, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{}, nil)
	old := llm.UserMessage("<skill_instructions><name>writer</name><instructions>old body</instructions></skill_instructions>", llm.UserNameSkill)
	current := buildSkillInstructionMessage(catalog.ReadLoadedSkillContents(loaded)[0])
	history := []llm.Message{old, current, llm.UserMessage("继续", llm.UserNameHuman)}
	request := o.filterSkillInstructionMessages(history)
	if len(request) != 2 || request[0].Content != current.Content {
		t.Fatalf("filtered request = %+v", request)
	}

	loaded = nil
	request = o.filterSkillInstructionMessages(history)
	for _, message := range request {
		if message.Name == llm.UserNameSkill {
			t.Fatalf("unloaded skill body remained in request: %+v", request)
		}
	}
}

func writeSkillInjectionTestSkill(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: test skill\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
