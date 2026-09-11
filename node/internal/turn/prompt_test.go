package turn

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/DGS-ai-team/DAgents/node/internal/hooks"
	"github.com/DGS-ai-team/DAgents/node/internal/hostsnapshot"
	"github.com/DGS-ai-team/DAgents/node/internal/llm"
	"github.com/DGS-ai-team/DAgents/node/internal/promptcontext"
	"github.com/DGS-ai-team/DAgents/node/internal/skills"
)

func TestBuildSystemPrompt_keepsStablePrefixOnly(t *testing.T) {
	in := SystemPromptInput{
		AgentID:       "ops-01",
		WorkspaceRoot: "/data/ws",
		SessionID:     "sess-abc",
	}
	prompt := BuildSystemPrompt(in)
	if prompt == "" {
		t.Fatal("empty prompt")
	}
	if !containsAll(prompt, "tool_outputs/", "最高优先级规则", "任务执行契约", "完成条件", "明确证据后才能声称完成", "工具结果处理", "Node tool_result 事件以及模型可见的 [TOOL_RESULT_METADATA] 元数据", "工作区目录", "workspace_root", "runtime_root", "所有工具的 path、directory、cwd 等路径参数的相对路径均以它为基准", "操作工作区内资源时请使用相对路径") {
		t.Fatalf("prompt = %q", prompt)
	}
	if contains(prompt, "`data/`") {
		t.Fatalf("system prompt should not describe the retired data directory, got %q", prompt)
	}
	if contains(prompt, "ops-01") || contains(prompt, "sess-abc") || contains(prompt, "运行环境") {
		t.Fatalf("system prompt should not contain request context, got %q", prompt)
	}
	if contains(prompt, "bash_run") || contains(prompt, "background_job") || contains(prompt, "## 可用 skills") {
		t.Fatalf("system prompt should not embed tool-specific guidance, got %q", prompt)
	}
	injections := BuildContextInjections(in)
	if len(injections) != 1 || !containsAll(injections[0].Content, "ops-01", "sess-abc", "运行环境") {
		t.Fatalf("context injection = %+v", injections)
	}
}

func TestBuildSystemPrompt_includesHistoryJournalWhenEnabled(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptInput{
		AgentID:               "ops-01",
		SessionID:             "sess-a",
		IncludeHistoryJournal: true,
	})
	if !containsAll(prompt, ".dagents/<agent_id>/history/", "Node 写入", "不是 LLM 上下文的一部分") {
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

func TestBuildSystemPrompt_includesFrozenSkillsMetadataWithoutBody(t *testing.T) {
	root := t.TempDir()
	writeSkillForPromptTest(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2).NewTurnView()
	prompt := BuildSystemPrompt(SystemPromptInput{Catalog: catalog})
	if !contains(prompt, "## 可用 skills") || !contains(prompt, "writer: Write docs") || !contains(prompt, "list_available_skills") {
		t.Fatalf("frozen skills metadata is missing from system prompt: %q", prompt)
	}
	if contains(prompt, "Write clearly.") {
		t.Fatalf("skill body must not be part of system prompt: %q", prompt)
	}
}

func TestBuildSystemPrompt_keepsSkillsMetadataAtContextBoundary(t *testing.T) {
	root := t.TempDir()
	writeSkillForPromptTest(t, root, "writer", "---\nname: writer\ndescription: v1\n---\nBody\n")
	skillPath := filepath.Join(root, "writer", "SKILL.md")
	catalog := skills.NewCatalog(root, true, 2)
	frozen := catalog.NewTurnView()
	first := BuildSystemPrompt(SystemPromptInput{Catalog: frozen})
	if err := os.WriteFile(skillPath, []byte("---\nname: writer\ndescription: v2\n---\nBody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unchanged := BuildSystemPrompt(SystemPromptInput{Catalog: frozen})
	if unchanged != first || contains(unchanged, "v2") {
		t.Fatalf("active context prompt changed after live catalog edit: first=%q unchanged=%q", first, unchanged)
	}
	latest := BuildSystemPrompt(SystemPromptInput{Catalog: catalog.NewTurnView()})
	if !contains(latest, "v2") {
		t.Fatalf("new context boundary did not refresh skills metadata: %q", latest)
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
		AgentID:     "ops-01",
		RuntimeRoot: root,
	})
	if !containsAll(prompt, "外置 CLI 与工具", "mycli.cmd", "externaltools_menu.md", "编译好的二进制") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestBuildSystemPrompt_doesNotTreatWorkspaceAsRuntimeRoot(t *testing.T) {
	workspace := t.TempDir()
	cliDir := filepath.Join(workspace, "externaltools")
	if err := os.MkdirAll(cliDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cliDir, "workspace-only.cmd"), []byte("@echo off\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := BuildSystemPrompt(SystemPromptInput{WorkspaceRoot: workspace})
	if contains(prompt, "workspace-only.cmd") {
		t.Fatalf("workspace was incorrectly scanned as runtime root: %q", prompt)
	}
	if !containsAll(prompt, "workspace_root", "runtime_root", "与 `workspace_root` 不同") {
		t.Fatalf("path scopes are missing: %q", prompt)
	}
}

func TestBuildSystemPrompt_includesPreferredName(t *testing.T) {
	hostsnapshot.CaptureAtStartup()
	r := promptcontext.NewContentReader(promptcontext.Content{})
	r.SetPreferredName("小明")
	in := SystemPromptInput{
		AgentID:       "ops-01",
		WorkspaceRoot: "/data/ws",
		SessionID:     "sess-x",
		PromptCtx:     r,
	}
	prompt := BuildSystemPrompt(in)
	if contains(prompt, "以下是用户信息") || contains(prompt, "请称呼用户为：小明") {
		t.Fatalf("stable system prompt should omit prompt context: %q", prompt)
	}
	injections := BuildContextInjections(in)
	if len(injections) != 1 || !containsAll(injections[0].Content, "以下是用户信息", "请称呼用户为：小明") {
		t.Fatalf("context injection = %+v", injections)
	}
	if contains(prompt, "user.md") || contains(prompt, "用户信息与偏好") {
		t.Fatalf("should not inject user.md sidecar, got %q", prompt)
	}
}

func TestBuildChildSystemPrompt_includesPurposeAndSkipsParentSections(t *testing.T) {
	hostsnapshot.CaptureAtStartup()
	prompt := BuildChildSystemPrompt(ChildSystemPromptInput{
		AgentID:       "ops-01",
		WorkspaceRoot: "/data/ws",
		SessionID:     "child-abc",
		Purpose:       "review patch",
	})
	if !containsAll(prompt, "临时子 Agent", "review patch", "tool_outputs/", "工作区目录", "所有工具的 path、directory、cwd 等路径参数的相对路径均以它为基准") {
		t.Fatalf("prompt = %q", prompt)
	}
	if contains(prompt, "`data/`") {
		t.Fatalf("child system prompt should not describe the retired data directory, got %q", prompt)
	}
	if contains(prompt, "child-abc") || contains(prompt, "运行环境") {
		t.Fatalf("child system prompt should omit request context, got %q", prompt)
	}
	injections := BuildChildContextInjections(ChildSystemPromptInput{AgentID: "ops-01", SessionID: "child-abc"})
	if len(injections) != 1 || !containsAll(injections[0].Content, "child-abc", "运行环境") {
		t.Fatalf("child context injection = %+v", injections)
	}
	if !containsAll(prompt, "任务执行契约", "工具调用成功不等于任务成功", "只有在缺少关键信息") {
		t.Fatalf("child prompt missing execution contract: %q", prompt)
	}
	if contains(prompt, "多向用户确认") || contains(prompt, "积极向用户澄清") {
		t.Fatalf("child prompt should not encourage over-confirmation: %q", prompt)
	}
	if contains(prompt, "打招呼") || contains(prompt, "可用技能的目录") || contains(prompt, "以下是用户信息") {
		t.Fatalf("child prompt should omit parent sections, got %q", prompt)
	}
}

func TestChildSystemPromptBuilder_usedByOrchestrator(t *testing.T) {
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig("/data/ws")}, nil)
	orch.SetSystemPromptBuilder(ChildSystemPromptBuilder("scan logs"))
	prompt := orch.buildSystemPrompt("child-xyz")
	if !containsAll(prompt, "scan logs", "临时子 Agent") || contains(prompt, "child-xyz") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestChildSystemPromptBuilder_keepsLoadedSkillsOutOfSystemPrompt(t *testing.T) {
	root := t.TempDir()
	writeSkillForPromptTest(t, root, "writer", "---\nname: writer\ndescription: Write docs\n---\nWrite clearly.\n")
	catalog := skills.NewCatalog(root, true, 2)
	loaded := catalog.SetLoadedSkills([]string{"writer"})
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{
		Catalog: catalog,
		Get:     func() []skills.LoadedSkill { return loaded },
	}, nil, nil, hooks.RuntimeConfig{Duplicate: hooks.DefaultDuplicateConfig(), ToolResult: hooks.DefaultToolResultConfig("/data/ws")}, nil)
	orch.SetSystemPromptBuilder(ChildSystemPromptBuilder("review"))
	prompt := orch.buildSystemPrompt("child-xyz")
	if contains(prompt, "Write clearly.") || contains(prompt, "已加载 skills") {
		t.Fatalf("child system prompt must not contain skill body: %q", prompt)
	}
	messages := orch.activeSkillInstructionMessages()
	if len(messages) != 1 || messages[0].Name != llm.UserNameSkill || !contains(messages[0].Content, "Write clearly.") {
		t.Fatalf("skill context messages = %+v", messages)
	}
}

func TestBuildSystemPrompt_runPromptBuildPhase(t *testing.T) {
	orch := NewOrchestrator("ops-01", "/data/ws", nil, nil, nil, nil, SkillAccess{}, nil, nil, hooks.RuntimeConfig{
		Duplicate:  hooks.DefaultDuplicateConfig(),
		ToolResult: hooks.DefaultToolResultConfig("/data/ws"),
	}, nil)
	orch.toolHooks.RegisterPhaseHook(promptInjectHook{t: t}, hooks.RegisterOpts{Priority: 0})

	prompt := orch.buildSystemPrompt("sess-hook")
	if !contains(prompt, "## Injected By Hook") {
		t.Fatalf("prompt = %q", prompt)
	}
	if containsAll(prompt, "ops-01", "sess-hook") {
		t.Fatalf("stable system prompt unexpectedly contains request identity: %q", prompt)
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
