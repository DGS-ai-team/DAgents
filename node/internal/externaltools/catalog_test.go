package externaltools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderPromptSection_menuAndBinaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, menuFileName), []byte(`# 工具索引

| 名称 | 命令 | 说明 |
|------|------|------|
| officecli | officecli | Office 文档 |
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "officecli"), []byte("#!/bin/sh\necho ok"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.sh"), []byte("#!/bin/sh\ntrue"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("doc"), 0o644); err != nil {
		t.Fatal(err)
	}

	section := NewCatalog(root).RenderPromptSection()
	for _, want := range []string{
		"## 外置 CLI 与工具",
		"shell 脚本、编译好的二进制",
		"officecli",
		"helper.sh",
		"externaltools_menu.md",
		"`externaltools/`",
	} {
		if !strings.Contains(section, want) {
			t.Fatalf("section missing %q:\n%s", want, section)
		}
	}
	if strings.Contains(section, "notes.md") {
		t.Fatalf("should not list markdown: %s", section)
	}
}

func TestRenderPromptSection_emptyWhenNoContent(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".runtime")
	if err := os.MkdirAll(filepath.Join(root, dirName), 0o755); err != nil {
		t.Fatal(err)
	}
	placeholder := `# .runtime/externaltools/ 索引

| 名称 | 命令 | 说明 |
|------|------|------|
| （用户自行安装） | — | 将二进制放入本目录 |
`
	if err := os.WriteFile(filepath.Join(root, menuFileName), []byte(placeholder), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := NewCatalog(root).RenderPromptSection(); got != "" {
		t.Fatalf("placeholder menu should not render section, got %q", got)
	}
}

func TestDirPath_default(t *testing.T) {
	if got := DirPath(""); got != filepath.Join("./.runtime", "externaltools") {
		t.Fatalf("DirPath(\"\") = %q", got)
	}
}
