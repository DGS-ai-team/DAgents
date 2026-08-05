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
	if len(list) != 2 {
		t.Fatalf("len=%d", len(list))
	}
	ops, err := l.Get("ops-runner")
	if err != nil {
		t.Fatal(err)
	}
	if ops.DisplayName != "运维（覆盖）" {
		t.Fatalf("ops = %+v", ops)
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
}
