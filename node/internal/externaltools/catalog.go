// Package externaltools 索引 `.runtime/externaltools/` 外置 CLI、编译二进制与 shell 脚本。
package externaltools

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	dirName      = "externaltools"
	menuFileName = "externaltools_menu.md"
)

// reservedSubdirs 不参与自动列举（非 Agent 可调用的服务钩子目录等）。
var reservedSubdirs = map[string]struct{}{
	"serve": {},
}

// Catalog 读取 externaltools_menu.md 并扫描 externaltools/ 下的可执行文件。
type Catalog struct {
	runtimeDir string
}

// NewCatalog 绑定 `.runtime` 根目录。
func NewCatalog(runtimeDir string) *Catalog {
	return &Catalog{runtimeDir: strings.TrimSpace(runtimeDir)}
}

// DirPath 返回外置工具目录 `<runtime>/externaltools`。
func DirPath(runtimeDir string) string {
	root := strings.TrimRight(strings.TrimSpace(runtimeDir), "/")
	if root == "" {
		root = "./.runtime"
	}
	return filepath.Join(root, dirName)
}

// MenuPath 返回索引文件 `<runtime>/externaltools_menu.md`。
func (c *Catalog) MenuPath() string {
	if c == nil || c.runtimeDir == "" {
		return ""
	}
	return filepath.Join(c.runtimeDir, menuFileName)
}

// Dir 返回外置工具目录并确保存在。
func (c *Catalog) Dir() (string, error) {
	if c == nil || c.runtimeDir == "" {
		return "", nil
	}
	dir := DirPath(c.runtimeDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// RenderPromptSection 生成 system prompt 段落；无索引且无可执行文件时返回空串。
func (c *Catalog) RenderPromptSection() string {
	if c == nil || c.runtimeDir == "" {
		return ""
	}
	menu := c.readMenu()
	bins := c.listExecutables()
	if strings.TrimSpace(menu) == "" && len(bins) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n## 外置 CLI 与工具\n\n")
	b.WriteString(
		"Node 管理的 `externaltools/` 用于放置 **shell 脚本、编译好的二进制与第三方 CLI**，与工作区中的文件和 Markdown 技能区分。" +
			"安装后通常已加入 `PATH`；详细说明见 Node 运行时的 `externaltools_menu.md`。\n",
	)
	if menu != "" {
		b.WriteString("\n")
		b.WriteString(menu)
		b.WriteByte('\n')
	}
	if len(bins) > 0 {
		b.WriteString("\n当前 `externaltools/` 目录中的可执行文件（自动扫描，不含子目录）：\n\n")
		for _, name := range bins {
			b.WriteString("- `")
			b.WriteString(name)
			b.WriteString("`\n")
		}
	}
	return b.String()
}

func (c *Catalog) readMenu() string {
	path := c.MenuPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(string(raw))
	if text == "" || isPlaceholderMenu(text) {
		return ""
	}
	return text
}

func isPlaceholderMenu(text string) bool {
	lines := strings.Split(text, "\n")
	nonEmpty := 0
	for _, line := range lines {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if strings.HasPrefix(s, "|") {
			if strings.Contains(s, "用户自行安装") || strings.Contains(s, "------") || strings.Contains(s, "名称") {
				continue
			}
		}
		nonEmpty++
	}
	return nonEmpty == 0
}

func (c *Catalog) listExecutables() []string {
	dir, err := c.Dir()
	if err != nil || dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		lower := strings.ToLower(name)
		if lower == "readme.md" || strings.HasSuffix(lower, ".md") {
			continue
		}
		info, err := ent.Info()
		if err != nil {
			continue
		}
		if !looksExecutable(name, info) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}

func looksExecutable(name string, info os.FileInfo) bool {
	if info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
		return true
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".exe"),
		strings.HasSuffix(lower, ".cmd"),
		strings.HasSuffix(lower, ".bat"),
		strings.HasSuffix(lower, ".com"):
		return true
	case strings.HasSuffix(lower, ".sh"):
		return true
	default:
		return false
	}
}

// ReservedSubdir 返回 externaltools 下保留子目录名（测试用）。
func ReservedSubdir(name string) bool {
	_, ok := reservedSubdirs[name]
	return ok
}
