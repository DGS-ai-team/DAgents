package turn

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

func TestBuildSystemPrompt_includesAgentAndWorkspace(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:   "ops-01",
		FSRoot:    "/data/ws",
		SessionID: "sess-abc",
	})
	if prompt == "" {
		t.Fatal("empty prompt")
	}
	if !containsAll(prompt, "ops-01", "/data/ws", "read_file", "最高优先级规则", "sess-abc", "当前运行环境") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildSystemPrompt_includesPromptContext(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	dir := filepath.Join(root, "prompt_context")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "user.md"), []byte("prefer concise"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostsnapshot.CaptureAtStartup()
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:   "ops-01",
		FSRoot:    "/data/ws",
		SessionID: "sess-x",
		PromptCtx: promptcontext.NewReader(root),
	})
	if !containsAll(prompt, "用户信息与偏好", "prefer concise", "prompt_context") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildChildSystemPrompt_includesPurposeAndSkipsParentSections(t *testing.T) {
	hostsnapshot.CaptureAtStartup()
	prompt := BuildChildSystemPrompt(ChildSystemPromptInput{
		AgentID:   "ops-01",
		FSRoot:    "/data/ws",
		SessionID: "child-abc",
		Purpose:   "review patch",
	})
	if !containsAll(prompt, "临时子 Agent", "review patch", "child-abc", "/data/ws", "当前运行环境") {
		t.Fatalf("prompt = %q", prompt)
	}
	if contains(prompt, "打招呼") || contains(prompt, "可用技能的目录") || contains(prompt, "prompt_context") {
		t.Fatalf("child prompt should omit parent sections, got %q", prompt)
	}
}

func TestChildSystemPromptBuilder_usedByOrchestrator(t *testing.T) {
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, nil)
	orch.SetSystemPromptBuilder(ChildSystemPromptBuilder("scan logs"))
	prompt := orch.buildSystemPrompt("child-xyz")
	if !containsAll(prompt, "scan logs", "child-xyz", "临时子 Agent") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestChildSystemPromptBuilder_includesLoadedSkills(t *testing.T) {
	root := t.TempDir()
	writeSkillForPromptTest(t, root, "writer", "---\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2)
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, DefaultMaxToolLoops(), nil, nil, nil)
	orch.SetSystemPromptBuilder(ChildSystemPromptBuilder("review"))
	prompt := orch.buildSystemPrompt("child-xyz")
	if !containsAll(prompt, "Write clearly.", "已加载技能") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func writeSkillForPromptTest(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunTurnPhase_mapsAwaitingTool(t *testing.T) {
	if got := RunTurnPhase(StateAwaitingTool); got != "awaiting_tool_execution" {
		t.Fatalf("RunTurnPhase = %q", got)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !contains(text, part) {
			return false
		}
	}
	return true
}

func contains(text, sub string) bool {
	return len(sub) == 0 || (len(text) >= len(sub) && indexOf(text, sub) >= 0)
}

func indexOf(text, sub string) int {
	for i := 0; i+len(sub) <= len(text); i++ {
		if text[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
