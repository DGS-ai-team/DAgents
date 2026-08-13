package turn

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
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
	if !containsAll(prompt, "ops-01", "memory/", "sessions.db", "data/", "临时工作区", "skills/", "数据库", "最高优先级规则", "sess-abc", "运行环境", "工作区目录", "相对路径均基于工作区根目录", "操作工作区内资源时请使用相对路径") {
		t.Fatalf("prompt = %q", prompt)
	}
	if contains(prompt, "FS_ROOT") || contains(prompt, "/data/ws") {
		t.Fatalf("system prompt should not expose fs_root path, got %q", prompt)
	}
	if contains(prompt, "bash_run") || contains(prompt, "background_job") || contains(prompt, "## 可用 skills") {
		t.Fatalf("system prompt should not embed tool-specific guidance, got %q", prompt)
	}
}

func TestBuildSystemPrompt_includesHistoryJournalWhenEnabled(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:               "ops-01",
		SessionID:             "sess-a",
		IncludeHistoryJournal: true,
	})
	if !containsAll(prompt, "history/", "YYYYMMDD", "read_file") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildSystemPrompt_omitsHistoryJournalWhenDisabled(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:               "ops-01",
		SessionID:             "sess-a",
		IncludeHistoryJournal: false,
	})
	if contains(prompt, "history/") {
		t.Fatalf("prompt should omit history journal section, got %q", prompt)
	}
}

func TestBuildSystemPrompt_includesExternalTools(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	cliDir := filepath.Join(root, "externaltools")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "externaltools_menu.md"), []byte("# tools\n\n| x | y |\n| a | b |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// On Windows executable discovery is extension-based, so use a portable
	// command-file name instead of relying on POSIX executable permission bits.
	if err := os.WriteFile(filepath.Join(cliDir, "mycli.cmd"), []byte("@echo off\r\nexit /b 0\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	hostsnapshot.CaptureAtStartup()
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID: "ops-01",
		FSRoot:  root,
	})
	if !containsAll(prompt, "外置 CLI 与工具", "mycli.cmd", "externaltools_menu.md", "编译好的二进制") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildSystemPrompt_includesPreferredName(t *testing.T) {
	hostsnapshot.CaptureAtStartup()
	r := promptcontext.NewContentReader(promptcontext.Content{User: "legacy user.md ignored"})
	r.SetPreferredName("小明")
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:   "ops-01",
		FSRoot:    "/data/ws",
		SessionID: "sess-x",
		PromptCtx: r,
	})
	if !containsAll(prompt, "以下是用户信息", "请称呼用户为：小明") {
		t.Fatalf("prompt = %q", prompt)
	}
	if contains(prompt, "legacy user.md") || contains(prompt, "用户信息与偏好") {
		t.Fatalf("should not inject user.md sidecar, got %q", prompt)
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
	if !containsAll(prompt, "临时子 Agent", "review patch", "child-abc", "memory/", "运行环境", "工作区目录", "相对路径均基于工作区根目录") {
		t.Fatalf("prompt = %q", prompt)
	}
	if contains(prompt, "FS_ROOT") || contains(prompt, "/data/ws") {
		t.Fatalf("child prompt should not expose fs_root path, got %q", prompt)
	}
	if contains(prompt, "打招呼") || contains(prompt, "可用技能的目录") || contains(prompt, "以下是用户信息") {
		t.Fatalf("child prompt should omit parent sections, got %q", prompt)
	}
}

func TestChildSystemPromptBuilder_usedByOrchestrator(t *testing.T) {
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig("/data/ws")}, nil)
	orch.SetSystemPromptBuilder(ChildSystemPromptBuilder("scan logs"))
	prompt := orch.buildSystemPrompt("child-xyz")
	if !containsAll(prompt, "scan logs", "child-xyz", "临时子 Agent") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestChildSystemPromptBuilder_includesLoadedSkills(t *testing.T) {
	root := t.TempDir()
	writeSkillForPromptTest(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2)
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig("/data/ws")}, nil)
	orch.SetSystemPromptBuilder(ChildSystemPromptBuilder("review"))
	prompt := orch.buildSystemPrompt("child-xyz")
	if !containsAll(prompt, "Write clearly.", "已加载 skills") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildSystemPrompt_runPromptBuildPhase(t *testing.T) {
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{}, DefaultMaxToolLoops(), nil, nil, hooks.RuntimeConfig{
		Duplicate:  hooks.DefaultDuplicateConfig(),
		ToolResult: hooks.DefaultToolResultConfig("/data/ws"),
	}, nil)
	orch.toolHooks.RegisterPhaseHook(promptInjectHook{t: t}, hooks.RegisterOpts{Priority: 0})

	prompt := orch.buildSystemPrompt("sess-hook")
	if !contains(prompt, "## Injected By Hook") {
		t.Fatalf("prompt = %q", prompt)
	}
	if !containsAll(prompt, "ops-01", "sess-hook") {
		t.Fatalf("builtin system prompt missing base content: %q", prompt)
	}
}

type promptInjectHook struct {
	t *testing.T
}

func (h promptInjectHook) Name() string          { return "test.prompt.inject" }
func (h promptInjectHook) Phases() []hooks.Phase { return []hooks.Phase{hooks.PhasePromptBuild} }
func (h promptInjectHook) Run(_ context.Context, hc *hooks.Context, _ hooks.Host) (hooks.Result, error) {
	base := ""
	if hc.PromptBuild != nil {
		base = hc.PromptBuild.SystemPrompt
	}
	return hooks.Result{
		Mutations: map[string]any{
			hooks.MutationSystemPrompt: base + "\n## Injected By Hook",
		},
	}, nil
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
