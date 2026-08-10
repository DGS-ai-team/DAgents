package agenttemplate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoader_listAndOverride(t *testing.T) {
	builtin := t.TempDir()
	user := t.TempDir()
	write := func(dir, name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(builtin, "general.yaml", `
id: general
display_name: 通用助手
`)
	write(builtin, "ops.yaml", `
id: ops-runner
display_name: 运维
`)
	write(user, "ops-runner.yaml", `
id: ops-runner
display_name: 运维（覆盖）
`)

	l := NewLoader(builtin, user)
	list, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	// 嵌入内置至少含 general / code-reviewer / ops-runner；磁盘可覆盖同 id。
	if len(list) < 3 {
		t.Fatalf("len=%d", len(list))
	}
	ops, err := l.Get("ops-runner")
	if err != nil {
		t.Fatal(err)
	}
	if ops.DisplayName != "运维（覆盖）" {
		t.Fatalf("ops = %+v", ops)
	}
	general, err := l.Get("general")
	if err != nil {
		t.Fatal(err)
	}
	if general.DisplayName != "通用助手" {
		t.Fatalf("general from disk builtin = %+v", general)
	}
	if _, err := l.Get("missing"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestLoader_builtinPackagingTemplates(t *testing.T) {
	// 相对仓库根：本测试在 node/internal/agenttemplate 下运行。
	root := filepath.Join("..", "..", "..", "packaging", "agent-templates")
	if _, err := os.Stat(root); err != nil {
		t.Skip("packaging templates not available:", err)
	}
	l := NewLoader(root, "")
	list, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 3 {
		t.Fatalf("expected >=3 templates, got %d", len(list))
	}
	g, err := l.Get("general")
	if err != nil {
		t.Fatal(err)
	}
	if g.DisplayName == "" {
		t.Fatal("general display_name empty")
	}
	soul, custom := PromptBodiesFromDefaults(g.Defaults)
	if soul == "" {
		t.Fatal("general soul_md preset empty")
	}
	if custom == "" {
		t.Fatal("general custom_md preset empty")
	}
}

func TestLoader_embeddedBuiltinsAlwaysAvailable(t *testing.T) {
	// 无磁盘目录时仍应能列出嵌入模板（安装包场景）。
	l := NewLoader("", "")
	list, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 3 {
		t.Fatalf("embedded builtins: got %d", len(list))
	}
	for _, id := range []string{"general", "code-reviewer", "ops-runner"} {
		tpl, err := l.Get(id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		soul, custom := PromptBodiesFromDefaults(tpl.Defaults)
		if soul == "" || custom == "" {
			t.Fatalf("%s missing soul/custom presets", id)
		}
	}
}

func TestPromptBodiesStrip(t *testing.T) {
	defaults := map[string]any{
		"prompt_context": map[string]any{
			"soul_enabled": true,
			"soul_md":      "角色",
			"custom_md":    "补充",
		},
	}
	soul, custom := PromptBodiesFromDefaults(defaults)
	if soul != "角色" || custom != "补充" {
		t.Fatalf("bodies=%q %q", soul, custom)
	}
	StripPromptBodiesFromDefaults(defaults)
	pc := defaults["prompt_context"].(map[string]any)
	if _, ok := pc["soul_md"]; ok {
		t.Fatal("soul_md should be stripped")
	}
	if pc["soul_enabled"] != true {
		t.Fatal("enabled flag must remain")
	}
}
