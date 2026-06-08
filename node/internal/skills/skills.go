// Package skills 扫描 .runtime/skills 并渲染 prompt 段（对齐 Python harness/skills）。
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LoadedSkill 为会话已加载 skill 元信息（持久化在 session）。
type LoadedSkill struct {
	SkillName   string `json:"skill_name"`
	Description string `json:"description"`
}

// Definition 为磁盘 skill 定义。
type Definition struct {
	SkillName   string
	Description string
	Content     string
}

// Catalog 提供 skill 目录扫描与 prompt 渲染。
type Catalog struct {
	root        string
	maxInPrompt int
	enabled     bool
}

// NewCatalog 构造 skill 目录访问器。
func NewCatalog(root string, enabled bool, maxInPrompt int) *Catalog {
	if maxInPrompt <= 0 {
		maxInPrompt = 3
	}
	return &Catalog{
		root:        strings.TrimSpace(root),
		enabled:     enabled,
		maxInPrompt: maxInPrompt,
	}
}

// Enabled 表示 skills 功能是否开启。
func (c *Catalog) Enabled() bool {
	return c != nil && c.enabled
}

// List 扫描 `{root}/*/SKILL.md` 并返回全部 skill 元数据与正文。
func (c *Catalog) List() []Definition {
	if !c.enabled || c.root == "" {
		return nil
	}
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return nil
	}
	out := make([]Definition, 0)
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		dirName := strings.TrimSpace(ent.Name())
		if dirName == "" {
			continue
		}
		def, ok := c.readSkill(filepath.Join(c.root, dirName, "SKILL.md"), dirName)
		if ok {
			out = append(out, def)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SkillName < out[j].SkillName })
	return out
}

// ListMetadata 返回 skill_name/description 列表。
func (c *Catalog) ListMetadata() []LoadedSkill {
	defs := c.List()
	out := make([]LoadedSkill, 0, len(defs))
	for _, d := range defs {
		out = append(out, LoadedSkill{SkillName: d.SkillName, Description: d.Description})
	}
	return out
}

// SelectByName 按目录名查找 skill。
func (c *Catalog) SelectByName(skillName string) (Definition, bool) {
	name := strings.TrimSpace(skillName)
	if name == "" {
		return Definition{}, false
	}
	for _, d := range c.List() {
		if d.SkillName == name {
			return d, true
		}
	}
	return Definition{}, false
}

// RenderMetadataSection 渲染可用 skills 元数据段。
func (c *Catalog) RenderMetadataSection() string {
	meta := c.ListMetadata()
	if len(meta) == 0 {
		return ""
	}
	var b strings.Builder
	for _, item := range meta {
		desc := strings.TrimSpace(item.Description)
		if desc == "" {
			b.WriteString(fmt.Sprintf("- %s\n", item.SkillName))
		} else {
			b.WriteString(fmt.Sprintf("- %s: %s\n", item.SkillName, desc))
		}
	}
	return strings.TrimSpace(b.String())
}

// RenderLoadedSection 渲染已加载 skill 正文段（不含外层标题，由 prompt 层拼标题）。
func (c *Catalog) RenderLoadedSection(loaded []LoadedSkill) string {
	if len(loaded) == 0 {
		return ""
	}
	var b strings.Builder
	for _, item := range loaded {
		def, ok := c.SelectByName(item.SkillName)
		if !ok {
			continue
		}
		body := strings.TrimSpace(def.Content)
		if body == "" {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n%s\n\n", def.SkillName, body))
	}
	return strings.TrimSpace(b.String())
}

// SetLoadedSkills 按名称整组替换 loaded skills（load_skills 语义）。
func (c *Catalog) SetLoadedSkills(names []string) []LoadedSkill {
	if !c.enabled {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]LoadedSkill, 0, len(names))
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		def, ok := c.SelectByName(name)
		if !ok {
			continue
		}
		out = append(out, LoadedSkill{SkillName: def.SkillName, Description: def.Description})
		if len(out) >= c.maxInPrompt {
			break
		}
	}
	return out
}

// UnloadSkills 从 loaded 中移除指定名称。
func (c *Catalog) UnloadSkills(loaded []LoadedSkill, names []string) []LoadedSkill {
	remove := make(map[string]struct{})
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name != "" {
			remove[name] = struct{}{}
		}
	}
	out := make([]LoadedSkill, 0, len(loaded))
	for _, item := range loaded {
		if _, drop := remove[item.SkillName]; drop {
			continue
		}
		out = append(out, item)
	}
	return out
}

func (c *Catalog) readSkill(path, dirName string) (Definition, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, false
	}
	meta, body := parseFrontmatter(string(raw))
	desc := strings.TrimSpace(fmt.Sprint(meta["description"]))
	skillName := dirName
	if v, ok := meta["name"]; ok {
		if name := strings.TrimSpace(fmt.Sprint(v)); name != "" {
			skillName = name
		}
	}
	return Definition{
		SkillName:   skillName,
		Description: desc,
		Content:     body,
	}, true
}

func parseFrontmatter(text string) (map[string]any, string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "---\n") {
		return map[string]any{}, text
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return map[string]any{}, text
	}
	metaBlock := text[4 : 4+end]
	body := strings.TrimSpace(text[4+end+5:])
	return parseSimpleYAML(metaBlock), body
}

func parseSimpleYAML(block string) map[string]any {
	out := make(map[string]any)
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, ":") {
			continue
		}
		key, val, _ := strings.Cut(line, ":")
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" {
			continue
		}
		lower := strings.ToLower(val)
		if lower == "true" {
			out[key] = true
		} else if lower == "false" {
			out[key] = false
		} else {
			out[key] = val
		}
	}
	return out
}
